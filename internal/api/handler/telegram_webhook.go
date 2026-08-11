package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/channel"
	"github.com/pablojhp.pergo/internal/channel/telegram"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/media"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/repository"
)

const (
	// HeaderTelegramSecretToken is the HTTP header for Telegram bot secret token verification.
	HeaderTelegramSecretToken = "X-Telegram-Bot-Api-Secret-Token"
	// MaxTelegramWebhookPayloadSize limits Telegram webhook payloads to 5MiB.
	MaxTelegramWebhookPayloadSize = 5 * 1024 * 1024
)

// TelegramWebhookHandler handles inbound webhooks from Telegram.
type TelegramWebhookHandler struct {
	connectionsRepo     *repository.ConnectionRepository
	inboundProcessor    *inbound.InboundProcessor
	adapter             channel.InboundAdapter
	telegramBaseURL     string
}

// NewTelegramWebhookHandler creates a new TelegramWebhookHandler.
func NewTelegramWebhookHandler(
	connectionsRepo *repository.ConnectionRepository,
	inboundProcessor *inbound.InboundProcessor,
	mediaEngine media.Engine,
) *TelegramWebhookHandler {
	return &TelegramWebhookHandler{
		connectionsRepo:     connectionsRepo,
		inboundProcessor:    inboundProcessor,
		adapter:             telegram.NewTelegramInboundAdapter(mediaEngine),
	}
}

// SetBaseURL overrides the base Telegram API URL (useful for testing).
func (h *TelegramWebhookHandler) SetBaseURL(url string) {
	h.telegramBaseURL = url
	if ta, ok := h.adapter.(*telegram.TelegramInboundAdapter); ok {
		ta.SetBaseURL(url)
	}
}

// Handle processes the incoming Telegram webhook POST request.
func (h *TelegramWebhookHandler) Handle(c *echo.Context) error {
	ctx := c.Request().Context()
	traceID, _ := middleware.TraceIDFrom(ctx)

	workspaceIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil || workspaceIDStr == "" {
		return c.NoContent(http.StatusBadRequest)
	}

	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
	}

	// Retrieve secret token from headers
	receivedToken := c.Request().Header.Get(HeaderTelegramSecretToken)
	if receivedToken == "" {
		return c.NoContent(http.StatusUnauthorized)
	}

	// Load registered connections for the workspace
	conns, err := h.connectionsRepo.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return c.NoContent(http.StatusUnauthorized)
	}

	var matchingConn *repository.Connection
	for _, conn := range conns {
		if conn.Channel != "telegram" {
			continue
		}
		var config struct {
			SecretToken string `json:"secret_token"`
		}
		if err := json.Unmarshal(conn.Credentials, &config); err == nil && config.SecretToken != "" {
			if crypto.CompareHashConstantTime(receivedToken, config.SecretToken) {
				matchingConn = conn
				break
			}
		}
	}

	if matchingConn == nil {
		slog.Warn("tg webhook: no matching connection found for secret token", "workspace_id", workspaceID, "trace_id", traceID)
		return c.NoContent(http.StatusUnauthorized)
	}

	// Read raw request body with 5MiB limit
	body, err := ReadLimitedBody(c.Request().Body, MaxTelegramWebhookPayloadSize)
	if err != nil {
		if errors.Is(err, ErrPayloadTooLarge) {
			slog.Warn("tg webhook: body exceeds 5MiB limit", "workspace_id", workspaceID, "trace_id", traceID)
			return c.NoContent(http.StatusRequestEntityTooLarge)
		}
		return c.NoContent(http.StatusBadRequest)
	}

	headers := map[string]string{
		HeaderTelegramSecretToken: receivedToken,
	}

	if h.telegramBaseURL != "" {
		if ta, ok := h.adapter.(*telegram.TelegramInboundAdapter); ok {
			ta.SetBaseURL(h.telegramBaseURL)
		}
	}

	events, err := h.adapter.Parse(ctx, body, headers, matchingConn)
	if err != nil {
		slog.Warn("tg webhook: adapter failed to parse", "error", err, "trace_id", traceID)
		return c.NoContent(http.StatusForbidden)
	}

	for _, event := range events {
		if h.inboundProcessor != nil {
			err = h.inboundProcessor.Process(ctx, event)
			if err != nil {
				slog.Error("tg webhook: inbound processor failed", "error", err, "trace_id", traceID)
				return c.NoContent(http.StatusInternalServerError)
			}
		}
	}

	return c.NoContent(http.StatusOK)
}
