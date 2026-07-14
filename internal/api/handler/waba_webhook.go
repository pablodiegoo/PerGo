package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/channel"
	"github.com/pablojhp.pergo/internal/channel/whatsapp"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/repository"
)

// WABAWebhookHandler handles verification and inbound payloads for Meta's WhatsApp Cloud API (WABA).
type WABAWebhookHandler struct {
	connectionsRepo  *repository.ConnectionRepository
	inboundProcessor *inbound.InboundProcessor
	adapter          channel.InboundAdapter
}

func NewWABAWebhookHandler(
	connectionsRepo *repository.ConnectionRepository,
	inboundProcessor *inbound.InboundProcessor,
) *WABAWebhookHandler {
	return &WABAWebhookHandler{
		connectionsRepo:  connectionsRepo,
		inboundProcessor: inboundProcessor,
		adapter:          whatsapp.NewWABAInboundAdapter(),
	}
}

// SetBaseURL overrides the base Meta Graph API URL (useful for testing).
func (h *WABAWebhookHandler) SetBaseURL(url string) {
	if wa, ok := h.adapter.(*whatsapp.WABAInboundAdapter); ok {
		wa.SetBaseURL(url)
	}
}

type wabaVerifyCreds struct {
	VerifyToken string `json:"verify_token"`
}

// HandleGet verification from Meta
func (h *WABAWebhookHandler) HandleGet(c *echo.Context) error {
	workspaceIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil || workspaceIDStr == "" {
		return c.NoContent(http.StatusBadRequest)
	}
	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
	}

	verifyToken := c.Request().URL.Query().Get("hub.verify_token")
	challenge := c.Request().URL.Query().Get("hub.challenge")

	expectedVerifyToken := "pergo_verify_token_" + workspaceIDStr

	// Load registered connections for the workspace
	conns, err := h.connectionsRepo.ListByWorkspace(c.Request().Context(), workspaceID)
	if err != nil {
		return c.NoContent(http.StatusForbidden)
	}

	var matchFound bool
	for _, conn := range conns {
		if conn.Channel != "whatsapp_cloud" {
			continue
		}

		var creds wabaVerifyCreds
		if err := json.Unmarshal(conn.Credentials, &creds); err == nil {
			if verifyToken != "" && (verifyToken == creds.VerifyToken || verifyToken == expectedVerifyToken) {
				matchFound = true
				break
			}
		}
	}

	if !matchFound {
		return c.NoContent(http.StatusForbidden)
	}

	return c.String(http.StatusOK, challenge)
}

// HandlePost ingests inbound messages from Meta
func (h *WABAWebhookHandler) HandlePost(c *echo.Context) error {
	workspaceIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil || workspaceIDStr == "" {
		return c.NoContent(http.StatusBadRequest)
	}
	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
	}

	// Load registered connections for the workspace
	conns, err := h.connectionsRepo.ListByWorkspace(c.Request().Context(), workspaceID)
	if err != nil {
		return c.NoContent(http.StatusForbidden)
	}

	var matchingConn *repository.Connection
	for _, conn := range conns {
		if conn.Channel == "whatsapp_cloud" {
			matchingConn = conn
			break
		}
	}

	if matchingConn == nil {
		slog.Warn("waba webhook: no connection found", "workspace_id", workspaceID)
		return c.NoContent(http.StatusForbidden)
	}

	// Read raw request body
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
	}

	events, err := h.adapter.Parse(c.Request().Context(), body, nil, matchingConn)
	if err != nil {
		slog.Warn("waba webhook: adapter failed to parse", "error", err)
		return c.NoContent(http.StatusForbidden)
	}

	ctx := c.Request().Context()
	for _, event := range events {
		if h.inboundProcessor != nil {
			err := h.inboundProcessor.Process(ctx, event)
			if err != nil {
				slog.Error("waba webhook: inbound processor failed", "error", err, "message_id", event.MessageID)
				return c.NoContent(http.StatusInternalServerError)
			}
		}
	}

	return c.NoContent(http.StatusOK)
}
