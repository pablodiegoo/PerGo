package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/pages"
)

// ChatwootAdminHandler handles Chatwoot integration settings from the admin panel.
type ChatwootAdminHandler struct {
	integrationRepo *repository.IntegrationRepository
}

// NewChatwootAdminHandler creates a new ChatwootAdminHandler.
func NewChatwootAdminHandler(integrationRepo *repository.IntegrationRepository) *ChatwootAdminHandler {
	return &ChatwootAdminHandler{
		integrationRepo: integrationRepo,
	}
}

// GetSettings renders the Chatwoot settings page with current configuration.
func (h *ChatwootAdminHandler) GetSettings(c *echo.Context) error {
	workspaceIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	var cfg pages.ChatwootConfig
	var active bool

	integration, err := h.integrationRepo.GetByProvider(c.Request().Context(), workspaceID, "chatwoot")
	if err != nil {
		if !errors.Is(err, repository.ErrIntegrationNotFound) {
			return c.String(http.StatusInternalServerError, "failed to load integration configuration")
		}
		// If not found, default to empty config and inactive
	} else {
		active = integration.Active
		if len(integration.Config) > 0 {
			if err := json.Unmarshal(integration.Config, &cfg); err != nil {
				return c.String(http.StatusInternalServerError, "failed to parse integration configuration")
			}
		}
	}

	return mw.Render(c, http.StatusOK, pages.ChatwootSettingsPage(workspaceID, cfg, active, "", ""))
}

// PostSettings handles saving Chatwoot integration credentials.
func (h *ChatwootAdminHandler) PostSettings(c *echo.Context) error {
	workspaceIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	apiURL := c.FormValue("api_url")
	accessToken := c.FormValue("access_token")
	accountIDStr := c.FormValue("account_id")
	inboxIDStr := c.FormValue("inbox_id")
	active := c.FormValue("active") == "true"

	var cfg pages.ChatwootConfig
	cfg.APIURL = apiURL
	cfg.AccessToken = accessToken

	accountID, err := strconv.ParseInt(accountIDStr, 10, 64)
	if err != nil {
		return mw.Render(c, http.StatusOK, pages.ChatwootSettingsPage(workspaceID, cfg, active, "", "ID da Conta inválido"))
	}
	cfg.AccountID = accountID

	inboxID, err := strconv.ParseInt(inboxIDStr, 10, 64)
	if err != nil {
		return mw.Render(c, http.StatusOK, pages.ChatwootSettingsPage(workspaceID, cfg, active, "", "ID da Inbox inválido"))
	}
	cfg.InboxID = inboxID

	if apiURL == "" || accessToken == "" {
		return mw.Render(c, http.StatusOK, pages.ChatwootSettingsPage(workspaceID, cfg, active, "", "API URL e Access Token são obrigatórios"))
	}

	configBytes, err := json.Marshal(cfg)
	if err != nil {
		return mw.Render(c, http.StatusOK, pages.ChatwootSettingsPage(workspaceID, cfg, active, "", "Erro ao serializar dados de configuração"))
	}

	// Try to get existing to preserve ID
	var integrationID uuid.UUID
	existing, err := h.integrationRepo.GetByProvider(c.Request().Context(), workspaceID, "chatwoot")
	if err == nil {
		integrationID = existing.ID
	} else {
		integrationID = uuid.New()
	}

	integration := &repository.Integration{
		ID:          integrationID,
		WorkspaceID: workspaceID,
		Name:        "Chatwoot Integration",
		Provider:    "chatwoot",
		Active:      active,
		Config:      configBytes,
	}

	if err := h.integrationRepo.Save(c.Request().Context(), integration); err != nil {
		return mw.Render(c, http.StatusOK, pages.ChatwootSettingsPage(workspaceID, cfg, active, "", "Erro ao salvar credenciais: "+err.Error()))
	}

	return mw.Render(c, http.StatusOK, pages.ChatwootSettingsPage(workspaceID, cfg, active, "Configurações salvas com sucesso!", ""))
}
