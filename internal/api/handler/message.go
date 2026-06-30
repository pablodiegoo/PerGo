package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/platform/storage"
	"github.com/pablojhp.pergo/internal/repository"
)

// Publisher defines the interface for publishing messages to a queue.
// JetStream implementation provides dedup via Nats-Msg-Id = traceID.
type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte, traceID string) error
}

// ConnectionFinder abstracts querying connection details for routing.
type ConnectionFinder interface {
	GetBySenderIdentity(ctx context.Context, workspaceID uuid.UUID, senderIdentity string) (*repository.Connection, error)
	GetDefaultChannelConnection(ctx context.Context, workspaceID uuid.UUID, channel string) (*repository.Connection, error)
}

// MessageHandler holds dependencies for the POST /messages endpoint.
type MessageHandler struct {
	Publisher      Publisher
	QueueDepth     *middleware.QueueDepthTracker
	S3Client       *storage.S3Client
	ConnectionRepo ConnectionFinder
}

// RegisterRoutes wires the message endpoints onto the Echo router.
// Optional middlewares are applied before the handler.
func (h *MessageHandler) RegisterRoutes(e *echo.Echo, middlewares ...echo.MiddlewareFunc) {
	e.POST("/api/v1/messages", h.Create, middlewares...)
}

// Create handles POST /messages — validates the payload, checks backpressure,
// generates a message ID, publishes to JetStream, and returns 202 Accepted
// with trace correlation.
func (h *MessageHandler) Create(c *echo.Context) error {
	// Extract trace_id from context (set by trace middleware)
	traceID, _ := middleware.TraceIDFrom(c.Request().Context())

	// Extract workspace_id from context (set by auth middleware)
	workspaceID, _ := tenant.WorkspaceIDFrom(c.Request().Context())

	// --- Backpressure: check queue depth BEFORE publish ---
	if h.QueueDepth != nil && workspaceID != (uuid.UUID{}) {
		if h.QueueDepth.Exceeds(workspaceID, 1000) {
			c.Response().Header().Set("Retry-After", "5")
			return c.JSON(http.StatusTooManyRequests, domain.ErrorResponse{
				Code:    "queue_full",
				Message: "per-session message queue limit exceeded",
				MoreInfo: "https://docs.pergo.dev/errors/queue_full",
			})
		}
	}

	// Bind JSON body to request struct
	var req domain.CreateMessageRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, domain.ErrorResponse{
			Code:    "invalid_payload",
			Message: "request body validation failed",
			Details: []domain.FieldError{
				{Field: "body", Message: "invalid JSON or missing required fields"},
			},
		})
	}

	// Validate the request
	if validationErr := domain.ValidateMessage(&req); validationErr != nil {
		return c.JSON(http.StatusBadRequest, *validationErr)
	}

	// Handle media downloading and validation if present
	if req.Media != nil {
		if h.S3Client == nil {
			slog.Error("S3 storage client is not configured for media downloads", "trace_id", traceID)
			return c.JSON(http.StatusInternalServerError, domain.ErrorResponse{
				Code:    "internal_error",
				Message: "media storage configuration error",
			})
		}

		res, err := storage.DownloadAndValidate(c.Request().Context(), req.Media.MediaURL, 25000000)
		if err != nil {
			if errors.Is(err, storage.ErrMediaSizeExceeded) {
				return c.JSON(http.StatusUnprocessableEntity, domain.ErrorResponse{
					Code:    "media_size_exceeded",
					Message: "the downloaded file exceeds the maximum size boundary of 25MB",
					Details: []domain.FieldError{
						{Field: "media.media_url", Message: "file exceeds 25MB limit"},
					},
				})
			}
			return c.JSON(http.StatusUnprocessableEntity, domain.ErrorResponse{
				Code:    "media_download_failed",
				Message: "failed to download media from the specified URL",
				Details: []domain.FieldError{
					{Field: "media.media_url", Message: err.Error()},
				},
			})
		}

		// Store downloaded media in S3.
		// Key format: {workspace_id}/{content_hash}.{ext}
		key := workspaceID.String() + "/" + res.Hash + "." + res.Extension
		if err := h.S3Client.Upload(c.Request().Context(), key, res.Bytes, res.ContentType); err != nil {
			slog.Error("failed to upload media to S3", "error", err, "trace_id", traceID, "key", key)
			return c.JSON(http.StatusInternalServerError, domain.ErrorResponse{
				Code:    "internal_error",
				Message: "failed to store media file",
			})
		}

		// Rewire the message payload's MediaURL to the internal proxy URL.
		// Format: /media/{workspace_id}/{hash}.{ext}
		req.Media.MediaURL = "/media/" + workspaceID.String() + "/" + res.Hash + "." + res.Extension
	}

	// Generate message ID
	msgID := uuid.New()

	// Queue status
	queuedAt := time.Now().UTC()

	if h.ConnectionRepo == nil {
		return c.JSON(http.StatusInternalServerError, domain.ErrorResponse{
			Code:    "internal_error",
			Message: "route resolver is not configured",
		})
	}

	var connID uuid.UUID
	var senderIdentity string
	var conn *repository.Connection
	var err error

	if req.From != "" {
		conn, err = h.ConnectionRepo.GetBySenderIdentity(c.Request().Context(), workspaceID, req.From)
		if err != nil {
			return c.JSON(http.StatusUnprocessableEntity, domain.ErrorResponse{
				Code:    "route_not_found",
				Message: "no matching connection route resolved for the specified sender identity",
			})
		}
		if conn.Channel != req.Channel {
			return c.JSON(http.StatusBadRequest, domain.ErrorResponse{
				Code:    "route_not_found",
				Message: "connection channel does not match request channel",
			})
		}
	} else {
		conn, err = h.ConnectionRepo.GetDefaultChannelConnection(c.Request().Context(), workspaceID, req.Channel)
		if err != nil {
			return c.JSON(http.StatusUnprocessableEntity, domain.ErrorResponse{
				Code:    "route_not_found",
				Message: "no default connection found for channel",
			})
		}
	}
	connID = conn.ID
	senderIdentity = conn.SenderIdentity

	// Publish to JetStream (if publisher is wired)
	if h.Publisher != nil {
		qMsg := domain.QueueMessage{
			WorkspaceID:      workspaceID,
			ConnectionID:     connID,
			SenderIdentity:   senderIdentity,
			TraceID:          traceID,
			To:               req.To,
			Channel:          req.Channel,
			Body:             req.Body,
			Media:            req.Media,
			Metadata:         req.Metadata,
			TTLSeconds:       req.TTLSeconds,
			QueuedAt:         queuedAt,
			FallbackChannels: req.FallbackChannels,
			TemplateName:     req.TemplateName,
			Language:         req.Language,
			Components:       req.Components,
		}
		payload, err := json.Marshal(qMsg)
		if err != nil {
			slog.Error("failed to marshal message", "error", err, "trace_id", traceID)
			return c.JSON(http.StatusInternalServerError, domain.ErrorResponse{
				Code:    "internal_error",
				Message: "failed to process message",
			})
		}
		if err := h.Publisher.Publish(c.Request().Context(), "messages.outbound", payload, traceID); err != nil {
			slog.Error("failed to publish message", "error", err, "trace_id", traceID)
			return c.JSON(http.StatusInternalServerError, domain.ErrorResponse{
				Code:    "publish_failed",
				Message: "failed to enqueue message",
			})
		}
	}

	// --- Backpressure: increment queue depth after successful publish ---
	if h.QueueDepth != nil && workspaceID != (uuid.UUID{}) {
		h.QueueDepth.Increment(workspaceID)
	}

	// Log the ingestion event
	slog.Info("message ingested",
		"trace_id", traceID,
		"workspace_id", workspaceID.String(),
		"message_id", msgID.String(),
		"channel", req.Channel,
		"to", req.To,
	)

	// Set X-Trace-Id response header
	c.Response().Header().Set("X-Trace-Id", traceID)

	// Return 202 Accepted
	return c.JSON(http.StatusAccepted, domain.CreateMessageResponse{
		MessageID: msgID,
		Status:    domain.StatusQueued,
		QueuedAt:  queuedAt,
	})
}
