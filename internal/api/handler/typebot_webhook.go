package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/outbound"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
)

// TypebotWebhookPayload represents the incoming JSON payload from a Typebot Webhook block.
type TypebotWebhookPayload struct {
	Message      string `json:"message"`
	ConnectionID string `json:"connection_id"`
	ContactID    string `json:"contact_id"`
}

type TypebotWebhookHandler struct {
	pool     *pgxpool.Pool
	ingestor outbound.OutboundProcessor
}

func NewTypebotWebhookHandler(pool *pgxpool.Pool, publisher Publisher) *TypebotWebhookHandler {
	var ingestor outbound.OutboundProcessor
	if pool != nil && publisher != nil {
		connRepo := repository.NewConnectionRepository(pool, nil)
		contactRepo := repository.NewContactRepository(pool)
		ingestor = outbound.NewProcessor(nil, nil, connRepo, publisher).SetContactRepository(contactRepo)
	}
	return &TypebotWebhookHandler{
		pool:     pool,
		ingestor: ingestor,
	}
}

func (h *TypebotWebhookHandler) SetIngestor(ingestor outbound.OutboundProcessor) *TypebotWebhookHandler {
	if h == nil {
		return nil
	}
	h.ingestor = ingestor
	return h
}

func (h *TypebotWebhookHandler) Handle(c *echo.Context) error {
	workspaceID, ok := tenant.WorkspaceIDFrom(c.Request().Context())
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{
			"code":    "unauthorized",
			"message": "invalid or missing API key",
		})
	}

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "failed to read body",
		})
	}

	var payload TypebotWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "failed to parse JSON",
		})
	}

	if payload.Message == "" || payload.ConnectionID == "" || payload.ContactID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "missing required fields (message, connection_id, contact_id)",
		})
	}

	// Resolve the connection's sender identity and channel
	var senderIdentity, channel string
	err = h.pool.QueryRow(c.Request().Context(), `
		SELECT sender_identity, channel
		FROM connections
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, payload.ConnectionID).Scan(&senderIdentity, &channel)
	if err != nil {
		slog.Error("failed to resolve connection identity", "error", err, "connection_id", payload.ConnectionID)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "failed to resolve connection",
		})
	}

	// Resolve the customer's channel identity (sender_identity column in contact_identities represents customer destination)
	var customerIdentity string
	err = h.pool.QueryRow(c.Request().Context(), `
		SELECT sender_identity 
		FROM contact_identities 
		WHERE workspace_id = $1 AND contact_id = $2 AND channel = $3
		LIMIT 1
	`, workspaceID, payload.ContactID, channel).Scan(&customerIdentity)
	if err != nil {
		slog.Error("failed to resolve customer identity from contact_identities", "error", err, "contact_id", payload.ContactID)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "failed to resolve contact identity",
		})
	}

	connectionID, err := uuid.Parse(payload.ConnectionID)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "invalid connection_id format",
		})
	}

	contactID, err := uuid.Parse(payload.ContactID)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "invalid contact_id format",
		})
	}

	traceID := uuid.New().String()
	req := &domain.CreateMessageRequest{
		To:      customerIdentity,
		Channel: channel,
		Body:    payload.Message,
		ConversationalIngestFields: domain.ConversationalIngestFields{
			SenderType:   string(domain.SenderTypeAIAgent),
			SenderName:   "Typebot",
			ConnectionID: &connectionID,
			ContactID:    &contactID,
		},
	}

	if _, err := h.ingestor.Ingest(c.Request().Context(), workspaceID, traceID, req); err != nil {
		slog.Error("failed to ingest outbound message from typebot webhook", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "failed to publish message",
		})
	}

	return c.NoContent(http.StatusOK)
}
