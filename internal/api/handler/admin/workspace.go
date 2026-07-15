package admin

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.pergo/internal/api/middleware"
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
	cookie, err := c.Cookie("pergo-active-workspace")
	if err == nil && cookie != nil && cookie.Value != "" {
		if parsed, parseErr := uuid.Parse(cookie.Value); parseErr == nil {
			// Verify workspace exists
			if _, checkErr := h.Repo.GetByID(ctx, parsed); checkErr == nil {
				wsID = parsed
			}
		}
	}

	if wsID == uuid.Nil {
		// Fetch first workspace
		list, err := h.Repo.List(ctx, 1)
		if err == nil && len(list) > 0 {
			wsID = list[0].ID
		} else {
			return c.String(http.StatusInternalServerError, "no workspaces configured")
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
