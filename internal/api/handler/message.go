package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/media"
	"github.com/pablojhp.pergo/internal/outbound"
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
	Ingestor       outbound.OutboundProcessor
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

	// Dynamically wrap legacy fields if Ingestor is not injected
	ingestor := h.Ingestor
	if ingestor == nil {
		var mediaEngine media.Engine
		if h.S3Client != nil {
			mediaEngine = media.NewDefaultEngine(h.S3Client)
		}
		var tracker outbound.QueueDepthChecker
		if h.QueueDepth != nil {
			tracker = h.QueueDepth
		}
		ingestor = outbound.NewProcessor(tracker, mediaEngine, h.ConnectionRepo, h.Publisher)
	}

	// Ingest using OutboundProcessor
	qMsg, err := ingestor.Ingest(c.Request().Context(), workspaceID, traceID, &req)
	if err != nil {
		if errors.Is(err, outbound.ErrQueueFull) {
			c.Response().Header().Set("Retry-After", "5")
			return c.JSON(http.StatusTooManyRequests, domain.ErrorResponse{
				Code:     "queue_full",
				Message:  "per-session message queue limit exceeded",
				MoreInfo: "https://docs.pergo.dev/errors/queue_full",
			})
		}

		var valErr *outbound.ValidationError
		if errors.As(err, &valErr) {
			return c.JSON(http.StatusBadRequest, *valErr.Response)
		}

		var mediaErr *outbound.MediaError
		if errors.As(err, &mediaErr) {
			if mediaErr.Code == "media_size_exceeded" {
				return c.JSON(http.StatusUnprocessableEntity, domain.ErrorResponse{
					Code:    "media_size_exceeded",
					Message: mediaErr.Message,
					Details: []domain.FieldError{
						{Field: mediaErr.Field, Message: "file exceeds 25MB limit"},
					},
				})
			}
			if mediaErr.Code == "internal_error" {
				return c.JSON(http.StatusInternalServerError, domain.ErrorResponse{
					Code:    "internal_error",
					Message: mediaErr.Message,
				})
			}
			return c.JSON(http.StatusUnprocessableEntity, domain.ErrorResponse{
				Code:    "media_download_failed",
				Message: mediaErr.Message,
				Details: []domain.FieldError{
					{Field: mediaErr.Field, Message: mediaErr.Err.Error()},
				},
			})
		}

		var routeErr *outbound.RouteError
		if errors.As(err, &routeErr) {
			if routeErr.Message == "route resolver is not configured" {
				return c.JSON(http.StatusInternalServerError, domain.ErrorResponse{
					Code:    "internal_error",
					Message: routeErr.Message,
				})
			}
			return c.JSON(http.StatusUnprocessableEntity, domain.ErrorResponse{
				Code:    "route_not_found",
				Message: routeErr.Message,
			})
		}

		// Generic internal server error
		return c.JSON(http.StatusInternalServerError, domain.ErrorResponse{
			Code:    "internal_error",
			Message: "failed to process message",
		})
	}

	msgID := uuid.New()

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
		QueuedAt:  qMsg.QueuedAt,
	})
}
