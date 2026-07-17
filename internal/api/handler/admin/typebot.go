package admin

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/pages"
)

type TypebotSettingsHandler struct {
	integrationRepo *repository.IntegrationRepository
}

func NewTypebotSettingsHandler(integrationRepo *repository.IntegrationRepository) *TypebotSettingsHandler {
	return &TypebotSettingsHandler{
		integrationRepo: integrationRepo,
	}
}

func (h *TypebotSettingsHandler) GetSettings(c *echo.Context) error {
	workspaceIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	var cfg pages.TypebotConfig
	var active bool

	integration, err := h.integrationRepo.GetByProvider(c.Request().Context(), workspaceID, "typebot")
	if err != nil {
		if !errors.Is(err, repository.ErrIntegrationNotFound) {
			return c.String(http.StatusInternalServerError, "failed to load integration configuration")
		}
	} else {
		active = integration.Active
		if len(integration.Config) > 0 {
			if err := json.Unmarshal(integration.Config, &cfg); err != nil {
				return c.String(http.StatusInternalServerError, "failed to parse integration configuration")
			}
		}
	}

	return mw.Render(c, http.StatusOK, pages.TypebotSettingsPage(workspaceID, cfg, active, "", ""))
}

func (h *TypebotSettingsHandler) PostSettings(c *echo.Context) error {
	workspaceIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	apiURL := c.FormValue("api_url")
	publicToken := c.FormValue("public_token")
	botID := c.FormValue("bot_id")
	triggerKeyword := c.FormValue("trigger_keyword")
	active := c.FormValue("active") == "true"

	var cfg pages.TypebotConfig
	cfg.APIURL = apiURL
	cfg.PublicToken = publicToken
	cfg.Bots = []pages.TypebotBot{
		{
			BotID:          botID,
			TriggerKeyword: triggerKeyword,
		},
	}

	if apiURL == "" || botID == "" {
		return mw.Render(c, http.StatusOK, pages.TypebotSettingsPage(workspaceID, cfg, active, "", "API URL and Bot ID are required"))
	}

	configBytes, err := json.Marshal(cfg)
	if err != nil {
		return mw.Render(c, http.StatusOK, pages.TypebotSettingsPage(workspaceID, cfg, active, "", "Error serializing configuration"))
	}

	var integrationID uuid.UUID
	existing, err := h.integrationRepo.GetByProvider(c.Request().Context(), workspaceID, "typebot")
	if err == nil {
		integrationID = existing.ID
	} else {
		integrationID = uuid.New()
	}

	integration := &repository.Integration{
		ID:          integrationID,
		WorkspaceID: workspaceID,
		Name:        "Typebot Integration",
		Provider:    "typebot",
		Active:      active,
		Config:      configBytes,
	}

	if err := h.integrationRepo.Save(c.Request().Context(), integration); err != nil {
		return mw.Render(c, http.StatusOK, pages.TypebotSettingsPage(workspaceID, cfg, active, "", "Error saving credentials: "+err.Error()))
	}

	return mw.Render(c, http.StatusOK, pages.TypebotSettingsPage(workspaceID, cfg, active, "Settings saved successfully!", ""))
}
