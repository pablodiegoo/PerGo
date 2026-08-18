package admin

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/pages"
)

// WorkspaceHandler holds dependencies for workspace admin operations.
type WorkspaceHandler struct {
	Repo        *repository.WorkspaceRepository
	APIKeys     *repository.APIKeyRepository
	ExternalURL string
}


// ActiveWorkspace redirects to the detail page of the active workspace.
func (h *WorkspaceHandler) ActiveWorkspace(c *echo.Context) error {
	ctx := c.Request().Context()
	var wsID uuid.UUID
	if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		wsID = id
	} else if cookie, err := c.Cookie(mw.ActiveWorkspaceCookieName); err == nil && cookie != nil && cookie.Value != "" {
		if parsed, parseErr := uuid.Parse(cookie.Value); parseErr == nil && parsed != uuid.Nil {
			if _, checkErr := h.Repo.GetByID(ctx, parsed); checkErr == nil {
				wsID = parsed
			}
		}
	}

	if wsID == uuid.Nil {
		if ws, err := h.Repo.GetEarliest(ctx); err == nil && ws != nil {
			wsID = ws.ID
			mw.SetActiveWorkspaceCookie(c, wsID)
		} else {
			return c.Redirect(http.StatusFound, "/admin/workspaces/new")
		}
	}

	return c.Redirect(http.StatusFound, fmt.Sprintf("/admin/workspaces/%s", wsID.String()))
}

// List renders the workspace list page or HTMX fragment.
func (h *WorkspaceHandler) List(c *echo.Context) error {
	workspaces, err := h.Repo.List(c.Request().Context(), 50)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to load workspaces")
	}

	if mw.IsHTMX(c) {
		return mw.Render(c, http.StatusOK, pages.WorkspaceListContent(workspaces))
	}
	return mw.Render(c, http.StatusOK, pages.WorkspaceListPage(workspaces))
}

// Create handles workspace creation via POST form.
func (h *WorkspaceHandler) Create(c *echo.Context) error {
	name := c.FormValue("name")
	if name == "" {
		return c.String(http.StatusBadRequest, "name is required")
	}

	ws, err := h.Repo.Create(c.Request().Context(), name)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to create workspace")
	}

	return mw.Render(c, http.StatusOK, pages.WorkspaceRow(*ws))
}

// Detail renders the workspace detail page with API keys.
func (h *WorkspaceHandler) Detail(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	ws, err := h.Repo.GetByID(c.Request().Context(), id)
	if err != nil {
		return c.String(http.StatusNotFound, "workspace not found")
	}

	var keys []repository.APIKey
	if h.APIKeys != nil {
		keys, err = h.APIKeys.ListByWorkspace(c.Request().Context(), id)
		if err != nil {
			keys = nil // degrade gracefully
		}
	}

	return mw.Render(c, http.StatusOK, pages.WorkspaceDetailPage(*ws, keys))
}

// ConfirmDelete returns an HTMX modal fragment for delete confirmation.
func (h *WorkspaceHandler) ConfirmDelete(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	ws, err := h.Repo.GetByID(c.Request().Context(), id)
	if err != nil {
		return c.String(http.StatusNotFound, "workspace not found")
	}

	return mw.Render(c, http.StatusOK, pages.WorkspaceDeleteConfirm(*ws))
}

// Delete removes a workspace and returns empty 200 for HTMX to remove the row.
func (h *WorkspaceHandler) Delete(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	if err := h.Repo.Delete(c.Request().Context(), id); err != nil {
		return c.String(http.StatusInternalServerError, "failed to delete workspace")
	}

	return c.NoContent(http.StatusOK)
}

// GetWebhookSecret returns the workspace's webhook secret key.
func (h *WorkspaceHandler) GetWebhookSecret(c *echo.Context) error {
	id, err := resolveWorkspaceID(c)
	if err != nil || id == uuid.Nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid workspace ID"})
	}

	ws, err := h.Repo.GetByID(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "workspace not found"})
	}

	secret := ""
	if ws.WebhookSecret != nil {
		secret = *ws.WebhookSecret
	}

	return c.JSON(http.StatusOK, map[string]string{
		"workspace_id":   id.String(),
		"webhook_secret": secret,
	})
}

// GenerateWebhookSecret generates or regenerates a workspace's webhook secret key.
func (h *WorkspaceHandler) GenerateWebhookSecret(c *echo.Context) error {
	id, err := resolveWorkspaceID(c)
	if err != nil || id == uuid.Nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid workspace ID"})
	}

	var req struct {
		WebhookSecret string `json:"webhook_secret"`
	}
	if c.Request().Body != nil && c.Request().ContentLength > 0 {
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		}
	}

	var secret string
	if req.WebhookSecret != "" {
		secret = req.WebhookSecret
		if err := h.Repo.SetWebhookSecret(c.Request().Context(), id, secret); err != nil {
			if errors.Is(err, repository.ErrWorkspaceNotFound) {
				return c.JSON(http.StatusNotFound, map[string]string{"error": "workspace not found"})
			}
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to set webhook secret"})
		}
	} else {
		var genErr error
		secret, genErr = h.Repo.GenerateWebhookSecret(c.Request().Context(), id)
		if genErr != nil {
			if errors.Is(genErr, repository.ErrWorkspaceNotFound) {
				return c.JSON(http.StatusNotFound, map[string]string{"error": "workspace not found"})
			}
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to generate webhook secret"})
		}
	}

	return c.JSON(http.StatusOK, map[string]string{
		"workspace_id":   id.String(),
		"webhook_secret": secret,
	})
}


