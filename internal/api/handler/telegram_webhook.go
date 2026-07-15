package handler

import (
	"io"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/channel"
	"github.com/pablojhp.pergo/internal/channel/telegram"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/media"
	"github.com/pablojhp.pergo/internal/repository"
)

// TelegramWebhookHandler handles inbound webhooks from Telegram.
type TelegramWebhookHandler struct {
	connectionsRepo     *repository.ConnectionRepository
	telegramContactRepo *repository.TelegramContactRepository
	inboundProcessor    *inbound.InboundProcessor
	adapter             channel.InboundAdapter
	telegramBaseURL     string
}

// NewTelegramWebhookHandler creates a new TelegramWebhookHandler.
func NewTelegramWebhookHandler(
	connectionsRepo *repository.ConnectionRepository,
	telegramContactRepo *repository.TelegramContactRepository,
	inboundProcessor *inbound.InboundProcessor,
	mediaEngine media.Engine,
) *TelegramWebhookHandler {
	return &TelegramWebhookHandler{
		connectionsRepo:     connectionsRepo,
		telegramContactRepo: telegramContactRepo,
		inboundProcessor:    inboundProcessor,
		adapter:             telegram.NewTelegramInboundAdapter(telegramContactRepo, mediaEngine),
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
	workspaceIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil || workspaceIDStr == "" {
		return c.NoContent(http.StatusBadRequest)
	}

	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
	}

	// Retrieve secret token from headers
	receivedToken := c.Request().Header.Get("X-Telegram-Bot-Api-Secret-Token")
	if receivedToken == "" {
		return c.NoContent(http.StatusForbidden)
	}

	// Load registered connections for the workspace
	conns, err := h.connectionsRepo.ListByWorkspace(c.Request().Context(), workspaceID)
	if err != nil {
		return c.NoContent(http.StatusForbidden)
	}

	var matchingConn *repository.Connection
	for _, conn := range conns {
		if conn.Channel != "telegram" {
			continue
		}
		matchingConn = conn
		break
	}

	if matchingConn == nil {
		slog.Warn("tg webhook: no matching connection found for workspace", "workspace_id", workspaceID)
		return c.NoContent(http.StatusForbidden)
	}

	// Read raw request body
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
	}

	headers := map[string]string{
		"X-Telegram-Bot-Api-Secret-Token": receivedToken,
	}

	if h.telegramBaseURL != "" {
		if ta, ok := h.adapter.(*telegram.TelegramInboundAdapter); ok {
			ta.SetBaseURL(h.telegramBaseURL)
		}
	}

	events, err := h.adapter.Parse(c.Request().Context(), body, headers, matchingConn)
	if err != nil {
		slog.Warn("tg webhook: adapter failed to parse", "error", err)
		return c.NoContent(http.StatusForbidden)
	}

	ctx := c.Request().Context()
	for _, event := range events {
		if h.inboundProcessor != nil {
			err = h.inboundProcessor.Process(ctx, event)
			if err != nil {
				slog.Error("tg webhook: inbound processor failed", "error", err)
				return c.NoContent(http.StatusInternalServerError)
			}
		}
	}

	return c.NoContent(http.StatusOK)
}
