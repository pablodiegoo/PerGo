package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.pergo/internal/api/middleware"
	typebot "github.com/pablojhp.pergo/internal/integration/typebot"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/pages"
)

type TypebotSettingsHandler struct {
	integrationRepo *repository.IntegrationRepository
	connectionRepo  *repository.ConnectionRepository
}

func NewTypebotSettingsHandler(
	integrationRepo *repository.IntegrationRepository,
	connectionRepo *repository.ConnectionRepository,
) *TypebotSettingsHandler {
	return &TypebotSettingsHandler{
		integrationRepo: integrationRepo,
		connectionRepo:  connectionRepo,
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
			var storeCfg typebot.Config
			// Tolerant of legacy phase-22 shape: missing fields default to zero values
			if err := json.Unmarshal(integration.Config, &storeCfg); err == nil {
				cfg.APIURL = storeCfg.APIURL
				cfg.Bots = make([]pages.TypebotBot, len(storeCfg.Bots))
				for i, b := range storeCfg.Bots {
					cfg.Bots[i] = pages.TypebotBot{
						BotID:           b.BotID,
						PublicToken:     b.PublicToken,
						ConnectionID:    b.ConnectionID,
						TriggerKeywords: strings.Join(b.TriggerWords, ","),
						IsDefault:       b.IsDefault,
					}
				}
			}
		}
	}

	conns, err := h.connectionRepo.ListByWorkspace(c.Request().Context(), workspaceID)
	if err != nil {
		conns = []*repository.Connection{}
	}
	connectionsOpts := make([]pages.ConnectionOption, len(conns))
	for i, conn := range conns {
		connectionsOpts[i] = pages.ConnectionOption{
			ID:      conn.ID.String(),
			Name:    conn.Name,
			Channel: conn.Channel,
		}
	}

	return mw.Render(c, http.StatusOK, pages.TypebotSettingsPage(workspaceID, cfg, connectionsOpts, active, "", ""))
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
	botID := c.FormValue("bot_id")
	botPublicToken := c.FormValue("bot_public_token")
	connectionID := c.FormValue("connection_id")
	triggerKeywords := c.FormValue("trigger_keywords")
	isDefault := c.FormValue("is_default") == "true"
	active := c.FormValue("active") == "true"

	displayCfg := pages.TypebotConfig{
		APIURL: apiURL,
		Bots: []pages.TypebotBot{
			{
				BotID:           botID,
				PublicToken:     botPublicToken,
				ConnectionID:    connectionID,
				TriggerKeywords: triggerKeywords,
				IsDefault:       isDefault,
			},
		},
	}

	conns, err := h.connectionRepo.ListByWorkspace(c.Request().Context(), workspaceID)
	if err != nil {
		conns = []*repository.Connection{}
	}
	connectionsOpts := make([]pages.ConnectionOption, len(conns))
	for i, conn := range conns {
		connectionsOpts[i] = pages.ConnectionOption{
			ID:      conn.ID.String(),
			Name:    conn.Name,
			Channel: conn.Channel,
		}
	}

	if apiURL == "" || botID == "" || botPublicToken == "" || connectionID == "" {
		return mw.Render(c, http.StatusOK, pages.TypebotSettingsPage(workspaceID, displayCfg, connectionsOpts, active, "", "API URL, Bot ID, Bot Public Token, and Connection are required"))
	}

	connUUID, err := uuid.Parse(connectionID)
	if err != nil {
		return mw.Render(c, http.StatusOK, pages.TypebotSettingsPage(workspaceID, displayCfg, connectionsOpts, active, "", "Invalid Connection ID"))
	}

	var triggerWords []string
	if triggerKeywords != "" {
		parts := strings.Split(triggerKeywords, ",")
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				triggerWords = append(triggerWords, trimmed)
			}
		}
	}

	storeCfg := typebot.Config{
		APIURL: apiURL,
		Bots: []typebot.BotConfig{
			{
				BotID:          botID,
				PublicToken:    botPublicToken,
				ConnectionID:   connUUID.String(),
				TriggerWords:   triggerWords,
				IsDefault:      isDefault,
				SessionTimeout: 0,
			},
		},
	}

	configBytes, err := json.Marshal(storeCfg)
	if err != nil {
		return mw.Render(c, http.StatusOK, pages.TypebotSettingsPage(workspaceID, displayCfg, connectionsOpts, active, "", "Error serializing configuration"))
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
		return mw.Render(c, http.StatusOK, pages.TypebotSettingsPage(workspaceID, displayCfg, connectionsOpts, active, "", "Error saving credentials: "+err.Error()))
	}

	return mw.Render(c, http.StatusOK, pages.TypebotSettingsPage(workspaceID, displayCfg, connectionsOpts, active, "Settings saved successfully!", ""))
}
