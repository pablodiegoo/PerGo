package admin

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.omnigo/internal/api/middleware"
	"github.com/pablojhp.omnigo/internal/repository"
	"github.com/pablojhp.omnigo/templates/pages"
)

// WorkspaceHandler holds dependencies for workspace admin operations.
type WorkspaceHandler struct {
	Repo        *repository.WorkspaceRepository
	APIKeys     *repository.APIKeyRepository
	Credentials *repository.CredentialsRepository
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

// Detail renders the workspace detail page with API keys and channel configuration.
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

	var waba pages.WABAConfig
	var tg pages.TelegramConfig

	if h.Credentials != nil {
		wabaBytes, err := h.Credentials.Get(c.Request().Context(), id, "whatsapp_cloud")
		if err == nil {
			_ = json.Unmarshal(wabaBytes, &waba)
		}
		tgBytes, err := h.Credentials.Get(c.Request().Context(), id, "telegram")
		if err == nil {
			_ = json.Unmarshal(tgBytes, &tg)
		}
	}

	return mw.Render(c, http.StatusOK, pages.WorkspaceDetailPage(*ws, keys, waba, tg))
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

// SaveCredentials handles form submission via POST for channel credentials.
func (h *WorkspaceHandler) SaveCredentials(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	channel, err := echo.PathParam[string](c, "channel")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid channel param")
	}
	if channel != "whatsapp_cloud" && channel != "telegram" {
		return c.String(http.StatusBadRequest, "invalid channel")
	}

	var payload []byte
	if channel == "whatsapp_cloud" {
		payload, err = json.Marshal(pages.WABAConfig{
			PhoneNumberID: c.FormValue("phone_number_id"),
			Token:         c.FormValue("token"),
			WABAAccountID: c.FormValue("waba_account_id"),
		})
	} else {
		payload, err = json.Marshal(pages.TelegramConfig{
			Token: c.FormValue("token"),
		})
	}
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to marshal credentials")
	}

	if err := h.Credentials.Save(c.Request().Context(), workspaceID, channel, payload); err != nil {
		return c.String(http.StatusInternalServerError, "failed to save credentials")
	}

	if channel == "whatsapp_cloud" {
		var waba pages.WABAConfig
		_ = json.Unmarshal(payload, &waba)
		return mw.Render(c, http.StatusOK, pages.WABACredentialsCard(idStr, waba))
	} else {
		var tg pages.TelegramConfig
		_ = json.Unmarshal(payload, &tg)
		return mw.Render(c, http.StatusOK, pages.TelegramCredentialsCard(idStr, tg))
	}
}

// DeleteCredentials revokes channel credentials.
func (h *WorkspaceHandler) DeleteCredentials(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(idStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	channel, err := echo.PathParam[string](c, "channel")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid channel param")
	}
	if channel != "whatsapp_cloud" && channel != "telegram" {
		return c.String(http.StatusBadRequest, "invalid channel")
	}

	if err := h.Credentials.Delete(c.Request().Context(), workspaceID, channel); err != nil {
		return c.String(http.StatusInternalServerError, "failed to delete credentials")
	}

	if channel == "whatsapp_cloud" {
		return mw.Render(c, http.StatusOK, pages.WABACredentialsCard(idStr, pages.WABAConfig{}))
	} else {
		return mw.Render(c, http.StatusOK, pages.TelegramCredentialsCard(idStr, pages.TelegramConfig{}))
	}
}

