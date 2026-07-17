package handler

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
)

// ChatwootWebhookHandler handles incoming webhooks from Chatwoot.
type ChatwootWebhookHandler struct{}

// NewChatwootWebhookHandler creates a new ChatwootWebhookHandler.
func NewChatwootWebhookHandler() *ChatwootWebhookHandler {
	return &ChatwootWebhookHandler{}
}

// Handle processes the incoming Chatwoot webhook requests.
func (h *ChatwootWebhookHandler) Handle(c *echo.Context) error {
	_, ok := tenant.WorkspaceIDFrom(c.Request().Context())
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{
			"code":    "unauthorized",
			"message": "invalid or missing API key",
		})
	}

	return c.NoContent(http.StatusOK)
}
