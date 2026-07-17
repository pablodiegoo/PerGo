package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
)


// ChatwootWebhookPayload represents the Chatwoot webhook payload for message events.
type ChatwootWebhookPayload struct {
	Event       string `json:"event"`
	MessageType string `json:"message_type"`
	Private     bool   `json:"private"`
	Content     string `json:"content"`
	Sender      struct {
		Type string `json:"type"`
	} `json:"sender"`
	Conversation struct {
		ID      int64 `json:"id"`
		InboxID int64 `json:"inbox_id"`
	} `json:"conversation"`
}

// ChatwootWebhookHandler handles incoming webhook events from Chatwoot.
type ChatwootWebhookHandler struct {
	pool        *pgxpool.Pool
	mappingRepo *repository.ChatwootMappingRepository
	publisher   Publisher
}

// NewChatwootWebhookHandler creates a new ChatwootWebhookHandler.
func NewChatwootWebhookHandler(
	pool *pgxpool.Pool,
	mappingRepo *repository.ChatwootMappingRepository,
	publisher Publisher,
) *ChatwootWebhookHandler {
	return &ChatwootWebhookHandler{
		pool:        pool,
		mappingRepo: mappingRepo,
		publisher:   publisher,
	}
}

// Handle processes the incoming Chatwoot webhook request.
func (h *ChatwootWebhookHandler) Handle(c *echo.Context) error {
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

	var payload ChatwootWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "failed to parse JSON",
		})
	}

	// Filter criteria (D-06): message_type must be "outgoing", private must be false, and sender.type must be "user"
	if payload.MessageType != "outgoing" || payload.Private || payload.Sender.Type != "user" {
		return c.NoContent(http.StatusOK)
	}

	// Lookup mapping (composite key tenant isolation)
	mapping, err := h.mappingRepo.GetByConversationID(c.Request().Context(), workspaceID, payload.Conversation.ID)
	if err != nil {
		if errors.Is(err, repository.ErrChatwootMappingNotFound) {
			slog.Warn("Chatwoot conversation mapping not found", "workspace_id", workspaceID, "conversation_id", payload.Conversation.ID)
			return c.NoContent(http.StatusOK)
		}
		slog.Error("failed to query chatwoot mapping", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "failed to query mapping",
		})
	}

	// Resolve customer's channel identity (sender_identity column in contact_identities represents customer destination)
	var customerIdentity string
	err = h.pool.QueryRow(c.Request().Context(), `
		SELECT sender_identity 
		FROM contact_identities 
		WHERE workspace_id = $1 AND contact_id = $2 AND channel = $3
		LIMIT 1
	`, workspaceID, mapping.ContactID, mapping.Channel).Scan(&customerIdentity)
	if err != nil {
		slog.Error("failed to resolve customer identity from contact_identities", "error", err, "contact_id", mapping.ContactID)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "failed to resolve contact identity",
		})
	}

	// Construct outbound queue message
	traceID := uuid.New().String()
	msg := domain.QueueMessage{
		WorkspaceID:    workspaceID,
		ConnectionID:   mapping.ConnectionID,
		SenderIdentity: mapping.SenderIdentity,
		TraceID:        traceID,
		To:             customerIdentity,
		Channel:        mapping.Channel,
		Body:           payload.Content,
		QueuedAt:       time.Now().UTC(),
	}

	payloadBytes, err := json.Marshal(msg)
	if err != nil {
		slog.Error("failed to marshal outbound message", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "failed to serialize message",
		})
	}

	// Enqueue/Publish payload
	err = h.publisher.Publish(c.Request().Context(), "messages.outbound", payloadBytes, traceID)
	if err != nil {
		slog.Error("failed to publish outbound message to NATS", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "failed to publish message",
		})
	}

	return c.NoContent(http.StatusOK)
}
