package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/platform/queue"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/pages"
)

type WebhookDLQHandler struct {
	Repo       *repository.WebhookDLQRepository
	Workspaces *repository.WorkspaceRepository
	Publisher  *queue.JetStreamPublisher
}

func NewWebhookDLQHandler(repo *repository.WebhookDLQRepository, workspaces *repository.WorkspaceRepository, publisher *queue.JetStreamPublisher) *WebhookDLQHandler {
	return &WebhookDLQHandler{
		Repo:       repo,
		Workspaces: workspaces,
		Publisher:  publisher,
	}
}

// Page renders the webhooks config page for a workspace.
func (h *WebhookDLQHandler) Page(c *echo.Context) error {
	wsIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	ws, err := h.Workspaces.GetByID(c.Request().Context(), workspaceID)
	if err != nil {
		return c.String(http.StatusNotFound, "workspace not found")
	}

	config, err := h.Repo.GetConfig(c.Request().Context(), workspaceID)
	if err != nil && !errors.Is(err, repository.ErrWebhookConfigNotFound) {
		return c.String(http.StatusInternalServerError, "failed to fetch webhook config")
	}

	dlqItems, err := h.Repo.ListDLQ(c.Request().Context(), workspaceID, 100, 0)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to list DLQ items")
	}

	page := pages.WorkspaceWebhooksPage(*ws, config, dlqItems)
	return mw.Render(c, http.StatusOK, page)
}

// SaveConfig saves or updates the webhook config.
func (h *WebhookDLQHandler) SaveConfig(c *echo.Context) error {
	wsIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	url := c.FormValue("url")
	secret := c.FormValue("secret")

	if url == "" || secret == "" {
		return c.String(http.StatusBadRequest, "url and secret are required")
	}

	// If secret is the placeholder, fetch the existing config to preserve the original secret
	var secretBytes []byte
	if secret == "********" {
		cfg, err := h.Repo.GetConfig(c.Request().Context(), workspaceID)
		if err != nil {
			return c.String(http.StatusBadRequest, "cannot update configuration without a new secret")
		}
		secretBytes = cfg.Secret
	} else {
		secretBytes = []byte(secret)
	}

	err = h.Repo.SaveConfig(c.Request().Context(), workspaceID, url, secretBytes)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to save webhook configuration")
	}

	// Trigger HTMX redirect back to the page
	c.Response().Header().Set("HX-Refresh", "true")
	return c.NoContent(http.StatusOK)
}

// DeleteConfig deletes the webhook configuration.
func (h *WebhookDLQHandler) DeleteConfig(c *echo.Context) error {
	wsIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	err = h.Repo.DeleteConfig(c.Request().Context(), workspaceID)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to delete configuration")
	}

	c.Response().Header().Set("HX-Refresh", "true")
	return c.NoContent(http.StatusOK)
}

// GlobalPage renders the global webhooks and DLQ page for the sidebar.
func (h *WebhookDLQHandler) GlobalPage(c *echo.Context) error {
	ctx := c.Request().Context()
	workspaces, err := h.Workspaces.List(ctx, 100)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to list workspaces")
	}

	// Collect DLQ items across all workspaces
	// Since we don't have a global ListAllDLQ method in the repo, we can list from the DB directly or fetch for each workspace.
	// Let's implement a global ListAllDLQ in repo? Or we can query directly using the pool.
	// Actually, let's list DLQ items for each workspace and combine them, or run a query directly on the handler for simplicity.
	// Let's run a query directly in the handler, or we can add ListAllDLQ to repository.
	// Wait, to keep repository clean, let's define listAllDLQ here:
	dlqItems, err := h.listAllDLQ(ctx, 100, 0)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to fetch dead-lettered logs")
	}

	page := pages.WebhooksPage(workspaces, dlqItems)
	return mw.Render(c, http.StatusOK, page)
}

func (h *WebhookDLQHandler) listAllDLQ(ctx context.Context, limit, offset int) ([]*repository.WebhookDLQ, error) {
	return h.Repo.ListAllDLQ(ctx, limit, offset)
}

// GetBadgeCount returns the badge count fragment for the sidebar.
func (h *WebhookDLQHandler) GetBadgeCount(c *echo.Context) error {
	ctx := c.Request().Context()
	// Sum counts across all workspaces
	workspaces, err := h.Workspaces.List(ctx, 100)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to list workspaces")
	}

	total := 0
	for _, ws := range workspaces {
		count, err := h.Repo.GetDLQBadgeCount(ctx, ws.ID)
		if err == nil {
			total += count
		}
	}

	return mw.Render(c, http.StatusOK, pages.DLQBadgeFragment(total))
}

// GetDetails renders the details modal for a DLQ item.
func (h *WebhookDLQHandler) GetDetails(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "dlq_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid DLQ ID")
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid DLQ ID")
	}

	item, err := h.Repo.GetDLQByID(c.Request().Context(), id)
	if err != nil {
		return c.String(http.StatusNotFound, "DLQ log not found")
	}

	return mw.Render(c, http.StatusOK, pages.DLQDetailModal(item))
}

// DeleteDLQ deletes a specific DLQ item.
func (h *WebhookDLQHandler) DeleteDLQ(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "dlq_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid DLQ ID")
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid DLQ ID")
	}

	err = h.Repo.DeleteDLQ(c.Request().Context(), id)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to delete DLQ log")
	}

	return c.NoContent(http.StatusOK)
}

// RetryDLQ triggers manual retry by re-enqueueing the event to NATS.
func (h *WebhookDLQHandler) RetryDLQ(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "dlq_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid DLQ ID")
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid DLQ ID")
	}

	ctx := c.Request().Context()
	item, err := h.Repo.GetDLQByID(ctx, id)
	if err != nil {
		return c.String(http.StatusNotFound, "DLQ log not found")
	}

	// 1. Publish fresh event to NATS
	var evt queue.WebhookEvent
	err = json.Unmarshal(item.Payload, &evt)
	if err != nil {
		slog.Error("admin: failed to unmarshal DLQ payload for retry", "error", err, "dlq_id", id)
		return c.String(http.StatusInternalServerError, "failed to retry webhook delivery: please check your connection and try again.")
	}

	// Update timestamp of the retry
	evt.Timestamp = time.Now().UTC().Format(time.RFC3339)
	payload, err := json.Marshal(evt)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to marshal retry event")
	}

	err = h.Publisher.Publish(ctx, "webhooks.events", payload, evt.TraceID)
	if err != nil {
		slog.Error("admin: failed to publish retry event to NATS", "error", err, "dlq_id", id)
		return c.String(http.StatusInternalServerError, "failed to retry webhook delivery: please check your connection and try again.")
	}

	// 2. Delete from DLQ table
	err = h.Repo.DeleteDLQ(ctx, id)
	if err != nil {
		slog.Error("admin: failed to delete retried DLQ log from DB", "error", err, "dlq_id", id)
	}

	// Return empty string / status 200 so HTMX removes the row or we can return a success indicator
	return c.HTML(http.StatusOK, fmt.Sprintf("<td colspan=\"7\" class=\"success-icon\" style=\"color: var(--color-success); text-align: center;\">✓ Re-enqueued for delivery</td>"))
}
