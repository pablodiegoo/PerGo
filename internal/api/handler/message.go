package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/media"
	"github.com/pablojhp.pergo/internal/outbound"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/platform/storage"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/session"
)

// Publisher defines the interface for publishing messages to a queue.
// JetStream implementation provides dedup via Nats-Msg-Id = traceID.
type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte, traceID string) error
}

// ConnectionFinder abstracts querying connection details for routing.
type ConnectionFinder interface {
	GetBySlug(ctx context.Context, workspaceID uuid.UUID, slug string) (*repository.Connection, error)
	GetBySenderIdentity(ctx context.Context, workspaceID uuid.UUID, senderIdentity string) (*repository.Connection, error)
	GetDefaultChannelConnection(ctx context.Context, workspaceID uuid.UUID, channel string) (*repository.Connection, error)
}

// MessageHandler holds dependencies for the POST /messages endpoint.
type MessageHandler struct {
	Ingestor        outbound.OutboundProcessor
	Publisher       Publisher
	QueueDepth      *middleware.QueueDepthTracker
	S3Client        *storage.S3Client
	ConnectionRepo  ConnectionFinder
	WindowChecker   *session.WindowChecker
	IdempotencyRepo *repository.IdempotencyRepository
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

	// Read body bytes for deterministic hashing
	bodyBytes, _ := io.ReadAll(c.Request().Body)
	c.Request().Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// Bind JSON body to request struct
	var keyHash string
	var req domain.CreateMessageRequest
	if err := c.Bind(&req); err != nil {
		c.Request().Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		return c.JSON(http.StatusBadRequest, domain.ErrorResponse{
			Code:    "invalid_payload",
			Message: "request body validation failed",
			Details: []domain.FieldError{
				{Field: "body", Message: "invalid JSON or missing required fields"},
			},
		})
	}
	c.Request().Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// Idempotency check and ledger recording
	idempotencyKey, keyHash := h.hashIdempotencyKey(c.Request().Header.Get("Idempotency-Key"), bodyBytes)
	if cached, err := h.checkAndRecordIdempotency(c, workspaceID, traceID, keyHash, idempotencyKey, &req); err != nil || cached {
		return err
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
		proc := outbound.NewProcessor(tracker, mediaEngine, h.ConnectionRepo, h.Publisher)
		if h.WindowChecker != nil {
			proc.SetWindowChecker(h.WindowChecker)
		}
		ingestor = proc
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

		var sessionErr *session.SessionWindowError
		if errors.As(err, &sessionErr) {
			expiredAtStr := ""
			windowDurStr := "24h"
			sourceStr := "ingestion"
			if sessionErr.Status != nil {
				if !sessionErr.Status.ExpiresAt.IsZero() {
					expiredAtStr = sessionErr.Status.ExpiresAt.Format(time.RFC3339)
				}
				if sessionErr.Status.WindowDuration > 0 {
					windowDurStr = sessionErr.Status.WindowDuration.String()
				}
			}
			if sessionErr.Source != "" {
				sourceStr = sessionErr.Source
			}

			return c.JSON(http.StatusUnprocessableEntity, map[string]any{
				"code":    "SESSION_WINDOW_EXPIRED",
				"message": "Customer service window expired for recipient",
				"details": map[string]string{
					"window_expired_at": expiredAtStr,
					"window_duration":   windowDurStr,
					"hint":              "Use type: template to reach this contact",
					"source":            sourceStr,
				},
			})
		}

		if errors.Is(err, outbound.ErrMissingCatalogID) {
			return c.JSON(http.StatusUnprocessableEntity, domain.ErrorResponse{
				Code:    "missing_catalog_id",
				Message: outbound.ErrMissingCatalogID.Error(),
			})
		}

		var valErr *outbound.ValidationError
		if errors.As(err, &valErr) {
			if valErr.Response != nil && valErr.Response.Code == "invalid_product_payload" {
				return c.JSON(http.StatusUnprocessableEntity, *valErr.Response)
			}
			return c.JSON(http.StatusBadRequest, *valErr.Response)
		}

		if errors.Is(err, outbound.ErrInvalidProductPayload) {
			return c.JSON(http.StatusUnprocessableEntity, domain.ErrorResponse{
				Code:    "invalid_product_payload",
				Message: outbound.ErrInvalidProductPayload.Error(),
			})
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

		if errors.Is(err, outbound.ErrTemplateNotFound) {
			return c.JSON(http.StatusUnprocessableEntity, domain.ErrorResponse{
				Code:    "template_not_found",
				Message: "The requested template was not found in the connection cache",
			})
		}

		var tmplNotAppErr *outbound.ErrTemplateNotApproved
		if errors.As(err, &tmplNotAppErr) {
			reason := "Template is not approved"
			if tmplNotAppErr.RejectionReason != nil {
				reason = *tmplNotAppErr.RejectionReason
			}
			return c.JSON(http.StatusUnprocessableEntity, domain.ErrorResponse{
				Code:    "template_not_approved",
				Message: reason,
			})
		}

		var invalidParamErr *outbound.ErrInvalidTemplateParameters
		if errors.As(err, &invalidParamErr) {
			return c.JSON(http.StatusUnprocessableEntity, domain.ErrorResponse{
				Code:    "invalid_template_parameters",
				Message: invalidParamErr.Message,
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
	resp := domain.CreateMessageResponse{
		MessageID: msgID,
		Status:    domain.StatusQueued,
		QueuedAt:  qMsg.QueuedAt,
	}

	respBytes, _ := json.Marshal(resp)
	h.recordIdempotencyCompletion(c.Request().Context(), workspaceID, traceID, keyHash, respBytes)

	return c.JSON(http.StatusAccepted, resp)
}

// hashIdempotencyKey computes the SHA-256 hash for the given header key or body payload.
func (h *MessageHandler) hashIdempotencyKey(headerKey string, bodyBytes []byte) (idempotencyKey string, keyHash string) {
	if headerKey != "" {
		hasher := sha256.New()
		hasher.Write([]byte(headerKey))
		return headerKey, hex.EncodeToString(hasher.Sum(nil))
	}
	hasher := sha256.New()
	hasher.Write(bodyBytes)
	keyHash = hex.EncodeToString(hasher.Sum(nil))
	return keyHash, keyHash
}

// checkAndRecordIdempotency performs idempotency lookup, key registration, and initial ledger recording.
// Returns true if a cached response was served directly to c.
func (h *MessageHandler) checkAndRecordIdempotency(c *echo.Context, workspaceID uuid.UUID, traceID string, keyHash string, idempotencyKey string, req *domain.CreateMessageRequest) (bool, error) {
	if h.IdempotencyRepo == nil || workspaceID == uuid.Nil {
		return false, nil
	}

	ctx := c.Request().Context()
	if entry, err := h.IdempotencyRepo.GetByIdempotencyKey(ctx, workspaceID, keyHash); err == nil && entry != nil && entry.StatusCode != nil {
		c.Response().Header().Set("Content-Type", "application/json")
		c.Response().WriteHeader(*entry.StatusCode)
		_, _ = c.Response().Write(entry.ResponseBody)
		return true, nil
	}

	if _, err := h.IdempotencyRepo.CheckAndStore(ctx, workspaceID, keyHash, traceID, 24*time.Hour); err != nil {
		slog.Error("failed to store idempotency key", "trace_id", traceID, "workspace_id", workspaceID.String(), "error", err)
	}
	if err := h.IdempotencyRepo.RecordLedger(ctx, &repository.IngressLedgerEntry{
		WorkspaceID:    workspaceID,
		TraceID:        traceID,
		IdempotencyKey: idempotencyKey,
		Channel:        req.Channel,
		Recipient:      req.To,
		Status:         "accepted",
	}); err != nil {
		slog.Error("failed to record idempotency ledger", "trace_id", traceID, "workspace_id", workspaceID.String(), "error", err)
	}
	return false, nil
}

// recordIdempotencyCompletion updates the ledger status to enqueued and records the HTTP 202 response body.
func (h *MessageHandler) recordIdempotencyCompletion(ctx context.Context, workspaceID uuid.UUID, traceID string, keyHash string, respBytes []byte) {
	if h.IdempotencyRepo != nil && workspaceID != uuid.Nil {
		if err := h.IdempotencyRepo.UpdateLedgerStatus(ctx, workspaceID, traceID, "enqueued", nil); err != nil {
			slog.Error("failed to update idempotency ledger status", "trace_id", traceID, "workspace_id", workspaceID.String(), "error", err)
		}
		if err := h.IdempotencyRepo.UpdateResponse(ctx, workspaceID, keyHash, http.StatusAccepted, respBytes, nil); err != nil {
			slog.Error("failed to update idempotency response", "trace_id", traceID, "workspace_id", workspaceID.String(), "error", err)
		}
	}
}
