package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.omnigo/internal/api/middleware"
	"github.com/pablojhp.omnigo/internal/domain"
	"github.com/pablojhp.omnigo/internal/platform/postgres/tenant"
)

// Publisher defines the interface for publishing messages to a queue.
// JetStream implementation provides dedup via Nats-Msg-Id = traceID.
type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte, traceID string) error
}

// MessageHandler holds dependencies for the POST /messages endpoint.
type MessageHandler struct {
	Publisher  Publisher
	QueueDepth *middleware.QueueDepthTracker
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
				MoreInfo: "https://docs.omnigo.dev/errors/queue_full",
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

	// Generate message ID
	msgID := uuid.New()

	// Queue status
	queuedAt := time.Now().UTC()

	// Publish to JetStream (if publisher is wired)
	if h.Publisher != nil {
		qMsg := domain.QueueMessage{
			WorkspaceID:      workspaceID,
			TraceID:          traceID,
			To:               req.To,
			Channel:          req.Channel,
			Body:             req.Body,
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
