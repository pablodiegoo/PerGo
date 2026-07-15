package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/pages"
)

// WABATemplateHandler handles WABA template management.
type WABATemplateHandler struct {
	Repo            *repository.WABATemplateRepository
	ConnectionsRepo *repository.ConnectionRepository
	Client          *http.Client
	BaseURL         string
}

// NewWABATemplateHandler creates a new WABATemplateHandler.
func NewWABATemplateHandler(repo *repository.WABATemplateRepository, connectionsRepo *repository.ConnectionRepository) *WABATemplateHandler {
	return &WABATemplateHandler{
		Repo:            repo,
		ConnectionsRepo: connectionsRepo,
		Client:          http.DefaultClient,
		BaseURL:         "https://graph.facebook.com/v18.0",
	}
}

// List retrieves all templates for a workspace and renders the template list page or fragment.
func (h *WABATemplateHandler) List(c *echo.Context) error {
	workspaceIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	templates, err := h.Repo.ListByWorkspace(c.Request().Context(), workspaceID)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to retrieve templates")
	}

	if mw.IsHTMX(c) {
		return mw.Render(c, http.StatusOK, pages.WABATemplateListContent(workspaceID, templates))
	}
	return mw.Render(c, http.StatusOK, pages.WABATemplatePage(workspaceID, templates))
}

// Create handles WABA template registration on Meta and local database persistence.
func (h *WABATemplateHandler) Create(c *echo.Context) error {
	workspaceIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	name := c.FormValue("name")
	language := c.FormValue("language")
	category := c.FormValue("category")
	componentsRaw := c.FormValue("components")

	// Fallback to JSON binding if form is empty
	if name == "" {
		type createReq struct {
			Name       string          `json:"name"`
			Language   string          `json:"language"`
			Category   string          `json:"category"`
			Components json.RawMessage `json:"components"`
		}
		var req createReq
		if err := c.Bind(&req); err == nil && req.Name != "" {
			name = req.Name
			language = req.Language
			category = req.Category
			componentsRaw = string(req.Components)
		}
	}

	if name == "" || language == "" || category == "" {
		return c.String(http.StatusBadRequest, "name, language, and category are required")
	}

	var components json.RawMessage
	if componentsRaw != "" {
		components = json.RawMessage(componentsRaw)
		if !json.Valid(components) {
			return c.String(http.StatusBadRequest, "components must be valid JSON")
		}
	} else {
		components = json.RawMessage("[]")
	}

	// Retrieve WABA connections for this workspace
	conns, err := h.ConnectionsRepo.ListByWorkspace(c.Request().Context(), workspaceID)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to load connections")
	}
	var wabaConn *repository.Connection
	for _, conn := range conns {
		if conn.Channel == "whatsapp_cloud" && (conn.Status == "active" || conn.Status == "connected") {
			wabaConn = conn
			break
		}
	}
	if wabaConn == nil {
		return c.String(http.StatusBadRequest, "whatsapp_cloud connection not configured or active for this workspace")
	}

	type wabaConfig struct {
		Token         string `json:"token"`
		WABAAccountID string `json:"waba_account_id"`
	}
	var config wabaConfig
	if err := json.Unmarshal(wabaConn.Credentials, &config); err != nil || config.Token == "" || config.WABAAccountID == "" {
		return c.String(http.StatusBadRequest, "invalid WABA credentials configuration; token and waba_account_id are required")
	}

	// Call Meta API to register template
	metaURL := fmt.Sprintf("%s/%s/message_templates", h.BaseURL, config.WABAAccountID)
	metaPayload := map[string]interface{}{
		"name":       name,
		"language":   language,
		"category":   category,
		"components": components,
	}
	metaBytes, err := json.Marshal(metaPayload)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to serialize Meta payload")
	}

	req, err := http.NewRequestWithContext(c.Request().Context(), http.MethodPost, metaURL, bytes.NewReader(metaBytes))
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to create Meta API request")
	}
	req.Header.Set("Authorization", "Bearer "+config.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.Client.Do(req)
	if err != nil {
		return c.String(http.StatusBadGateway, "failed to communicate with Meta API: "+err.Error())
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.String(resp.StatusCode, "Meta API returned error: "+string(respBytes))
	}

	type metaResponse struct {
		ID       string `json:"id"`
		Status   string `json:"status"`
		Category string `json:"category"`
	}
	var metaResp metaResponse
	if err := json.Unmarshal(respBytes, &metaResp); err != nil || metaResp.ID == "" {
		return c.String(http.StatusInternalServerError, "failed to parse Meta API response")
	}

	status := metaResp.Status
	if status == "" {
		status = "PENDING"
	}

	// Save to database
	tmpl := &repository.WABATemplate{
		WorkspaceID:    workspaceID,
		MetaTemplateID: metaResp.ID,
		Name:           name,
		Language:       language,
		Status:         status,
		Category:       category,
		Components:     components,
	}

	dbTmpl, err := h.Repo.Create(c.Request().Context(), tmpl)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to save template locally: "+err.Error())
	}

	if mw.IsHTMX(c) {
		c.Response().Header().Set("HX-Redirect", fmt.Sprintf("/admin/workspaces/%s/templates", workspaceID))
		return c.NoContent(http.StatusOK)
	}
	return c.JSON(http.StatusCreated, dbTmpl)
}

// Sync retrieves the current approval status of a template from Meta and updates local storage.
func (h *WABATemplateHandler) Sync(c *echo.Context) error {
	workspaceIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	templateIDStr, err := echo.PathParam[string](c, "template_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid template ID")
	}
	templateID, err := uuid.Parse(templateIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid template ID")
	}

	tmpl, err := h.Repo.GetByID(c.Request().Context(), templateID)
	if err != nil {
		return c.String(http.StatusNotFound, "template not found")
	}
	if tmpl.WorkspaceID != workspaceID {
		return c.String(http.StatusForbidden, "template does not belong to workspace")
	}

	// Retrieve WABA connections for this workspace
	conns, err := h.ConnectionsRepo.ListByWorkspace(c.Request().Context(), workspaceID)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to load connections")
	}
	var wabaConn *repository.Connection
	for _, conn := range conns {
		if conn.Channel == "whatsapp_cloud" && (conn.Status == "active" || conn.Status == "connected") {
			wabaConn = conn
			break
		}
	}
	if wabaConn == nil {
		return c.String(http.StatusBadRequest, "whatsapp_cloud connection not configured or active for this workspace")
	}

	type wabaConfig struct {
		Token string `json:"token"`
	}
	var config wabaConfig
	if err := json.Unmarshal(wabaConn.Credentials, &config); err != nil || config.Token == "" {
		return c.String(http.StatusBadRequest, "invalid credentials config; token is required")
	}

	// Call Meta API GET /{meta_template_id}
	metaURL := fmt.Sprintf("%s/%s", h.BaseURL, tmpl.MetaTemplateID)
	req, err := http.NewRequestWithContext(c.Request().Context(), http.MethodGet, metaURL, nil)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to create Meta API request")
	}
	req.Header.Set("Authorization", "Bearer "+config.Token)

	resp, err := h.Client.Do(req)
	if err != nil {
		return c.String(http.StatusBadGateway, "failed to connect to Meta API: "+err.Error())
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.String(resp.StatusCode, "Meta API returned error: "+string(respBytes))
	}

	type metaSyncResponse struct {
		Status string `json:"status"`
	}
	var metaResp metaSyncResponse
	if err := json.Unmarshal(respBytes, &metaResp); err != nil || metaResp.Status == "" {
		return c.String(http.StatusInternalServerError, "failed to parse Meta status response")
	}

	// Update locally
	err = h.Repo.UpdateStatus(c.Request().Context(), templateID, metaResp.Status)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to update template status locally")
	}

	tmpl.Status = metaResp.Status

	if mw.IsHTMX(c) {
		return mw.Render(c, http.StatusOK, pages.WABATemplateRow(workspaceID, *tmpl))
	}
	return c.JSON(http.StatusOK, tmpl)
}

// Delete handles local deletion of a WABA template.
func (h *WABATemplateHandler) Delete(c *echo.Context) error {
	workspaceIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	templateIDStr, err := echo.PathParam[string](c, "template_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid template ID")
	}
	templateID, err := uuid.Parse(templateIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid template ID")
	}

	tmpl, err := h.Repo.GetByID(c.Request().Context(), templateID)
	if err != nil {
		return c.String(http.StatusNotFound, "template not found")
	}
	if tmpl.WorkspaceID != workspaceID {
		return c.String(http.StatusForbidden, "template does not belong to workspace")
	}

	if err := h.Repo.Delete(c.Request().Context(), templateID); err != nil {
		return c.String(http.StatusInternalServerError, "failed to delete template locally")
	}

	if mw.IsHTMX(c) {
		return c.NoContent(http.StatusOK)
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}

// NewForm renders the creation form.
func (h *WABATemplateHandler) NewForm(c *echo.Context) error {
	workspaceIDStr, err := echo.PathParam[string](c, "workspace_id")
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}
	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	return mw.Render(c, http.StatusOK, pages.WABATemplateCreateForm(workspaceID))
}
