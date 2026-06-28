package admin

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/pages"
)

// APIKeyHandler holds dependencies for API key admin operations.
type APIKeyHandler struct {
	Repo       *repository.APIKeyRepository
	Workspaces *repository.WorkspaceRepository
}

// List returns the API key list fragment for a workspace.
func (h *APIKeyHandler) List(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	keys, err := h.Repo.ListByWorkspace(c.Request().Context(), workspaceID)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to load API keys")
	}

	return mw.Render(c, http.StatusOK, pages.APIKeyListFragment(workspaceID, keys))
}

// Generate creates a new API key and returns a fragment showing the plaintext once.
func (h *APIKeyHandler) Generate(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	name := c.FormValue("name")
	if name == "" {
		return c.String(http.StatusBadRequest, "name is required")
	}

	apiKey, plaintext, err := h.Repo.Create(c.Request().Context(), workspaceID, name)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to generate API key")
	}

	return mw.Render(c, http.StatusOK, pages.APIKeyRevealed(plaintext, apiKey.KeyPrefix, apiKey.Name))
}

// ConfirmRevoke returns an HTMX modal fragment for key revocation confirmation.
func (h *APIKeyHandler) ConfirmRevoke(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	keyIdStr, err := echo.PathParam[string](c, "key_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid key ID")
	}
	keyID, err := uuid.Parse(keyIdStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid key ID")
	}

	key, err := h.Repo.GetByID(c.Request().Context(), keyID)
	if err != nil {
		return c.String(http.StatusNotFound, "API key not found")
	}

	return mw.Render(c, http.StatusOK, pages.APIKeyRevokeConfirm(workspaceID, *key))
}

// Revoke revokes an API key and returns a fragment with the revoked badge.
func (h *APIKeyHandler) Revoke(c *echo.Context) error {
	keyIdStr, err := echo.PathParam[string](c, "key_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid key ID")
	}
	keyID, err := uuid.Parse(keyIdStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid key ID")
	}

	if err := h.Repo.Revoke(c.Request().Context(), keyID); err != nil {
		return c.String(http.StatusInternalServerError, "failed to revoke API key")
	}

	// Return a simple revoked row — HTMX will swap the original row
	idStr, _ := echo.PathParam[string](c, "id")
	workspaceID, _ := uuid.Parse(idStr)
	key, err := h.Repo.GetByID(c.Request().Context(), keyID)
	if err != nil {
		// Key was revoked but we can't fetch it — return minimal response
		return c.HTML(http.StatusOK, `<tr><td colspan="5"><span class="badge badge-danger">Revoked</span></td></tr>`)
	}

	return mw.Render(c, http.StatusOK, pages.APIKeyRow(workspaceID, *key))
}
