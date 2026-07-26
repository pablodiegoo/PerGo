package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/client"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
)

// WABATemplateAPIHandler exposes REST API endpoints for WABA message template management under /api/v1/waba/templates.
type WABATemplateAPIHandler struct {
	repo            *repository.WABATemplateRepository
	connectionsRepo *repository.ConnectionRepository
	metaClient      *client.WABAMetaClient
}

func NewWABATemplateAPIHandler(
	repo *repository.WABATemplateRepository,
	connectionsRepo *repository.ConnectionRepository,
	metaClient *client.WABAMetaClient,
) *WABATemplateAPIHandler {
	if metaClient == nil {
		metaClient = client.NewWABAMetaClient(nil, "")
	}
	return &WABATemplateAPIHandler{
		repo:            repo,
		connectionsRepo: connectionsRepo,
		metaClient:      metaClient,
	}
}

func (h *WABATemplateAPIHandler) RegisterRoutes(e *echo.Echo) {
	g := e.Group("/api/v1/waba/templates")
	g.POST("", h.Create)
	g.GET("", h.List)
	g.GET("/:id", h.Get)
	g.PUT("/:id", h.Update)
	g.DELETE("/:id", h.Delete)
	g.POST("/sync", h.Sync)
}

type createTemplateReq struct {
	ConnectionID uuid.UUID       `json:"connection_id"`
	Name         string          `json:"name"`
	Language     string          `json:"language"`
	Category     string          `json:"category"`
	Components   json.RawMessage `json:"components"`
}

func (h *WABATemplateAPIHandler) resolveWABAConfig(c *echo.Context, connectionID uuid.UUID, workspaceID uuid.UUID) (*repository.Connection, string, string, error) {
	conn, err := h.connectionsRepo.GetByID(c.Request().Context(), connectionID)
	if err != nil {
		return nil, "", "", errors.New("connection not found")
	}
	if conn.WorkspaceID != workspaceID {
		return nil, "", "", errors.New("connection does not belong to active workspace")
	}
	if conn.Channel != "whatsapp_cloud" {
		return nil, "", "", errors.New("connection is not a whatsapp_cloud channel")
	}

	type wabaCreds struct {
		Token         string `json:"token"`
		WABAAccountID string `json:"waba_account_id"`
	}
	var creds wabaCreds
	if err := json.Unmarshal(conn.Credentials, &creds); err != nil || creds.Token == "" || creds.WABAAccountID == "" {
		return nil, "", "", errors.New("invalid WABA connection credentials")
	}
	return conn, creds.WABAAccountID, creds.Token, nil
}

// POST /api/v1/waba/templates
func (h *WABATemplateAPIHandler) Create(c *echo.Context) error {
	wsID, ok := tenant.WorkspaceIDFrom(c.Request().Context())
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "missing workspace context"})
	}

	var req createTemplateReq
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	if req.Name == "" || req.Language == "" || req.Category == "" || req.ConnectionID == uuid.Nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "connection_id, name, language, and category are required"})
	}
	if req.Components == nil {
		req.Components = json.RawMessage("[]")
	}

	conn, wabaAccountID, token, err := h.resolveWABAConfig(c, req.ConnectionID, wsID)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	metaID, status, err := h.metaClient.CreateTemplate(c.Request().Context(), wabaAccountID, token, req.Name, req.Language, req.Category, req.Components)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "Meta API error: " + err.Error()})
	}

	tmpl := &repository.WABATemplate{
		WorkspaceID:    wsID,
		ConnectionID:   conn.ID,
		MetaTemplateID: metaID,
		Name:           req.Name,
		Language:       req.Language,
		Status:         status,
		Category:       req.Category,
		Components:     req.Components,
	}

	saved, err := h.repo.Create(c.Request().Context(), tmpl)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to store template locally: " + err.Error()})
	}
	return c.JSON(http.StatusCreated, saved)
}

// GET /api/v1/waba/templates
func (h *WABATemplateAPIHandler) List(c *echo.Context) error {
	wsID, ok := tenant.WorkspaceIDFrom(c.Request().Context())
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "missing workspace context"})
	}

	connIDStr := c.QueryParam("connection_id")
	if connIDStr != "" {
		connID, err := uuid.Parse(connIDStr)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid connection_id"})
		}
		templates, err := h.repo.ListByConnection(c.Request().Context(), connID)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, templates)
	}

	templates, err := h.repo.ListByWorkspace(c.Request().Context(), wsID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, templates)
}

// GET /api/v1/waba/templates/:id
func (h *WABATemplateAPIHandler) Get(c *echo.Context) error {
	wsID, ok := tenant.WorkspaceIDFrom(c.Request().Context())
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "missing workspace context"})
	}

	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid template ID"})
	}

	tmpl, err := h.repo.GetByID(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "template not found"})
	}
	if tmpl.WorkspaceID != wsID {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "template does not belong to active workspace"})
	}
	return c.JSON(http.StatusOK, tmpl)
}

// PUT /api/v1/waba/templates/:id
func (h *WABATemplateAPIHandler) Update(c *echo.Context) error {
	wsID, ok := tenant.WorkspaceIDFrom(c.Request().Context())
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "missing workspace context"})
	}

	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid template ID"})
	}

	existing, err := h.repo.GetByID(c.Request().Context(), id)
	if err != nil || existing == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "template not found"})
	}
	if existing.WorkspaceID != wsID {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "template does not belong to active workspace"})
	}

	var req createTemplateReq
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	_, wabaAccountID, token, err := h.resolveWABAConfig(c, existing.ConnectionID, wsID)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	if req.Category != "" {
		existing.Category = req.Category
	}
	if req.Components != nil {
		existing.Components = req.Components
	}

	metaID, status, err := h.metaClient.CreateTemplate(c.Request().Context(), wabaAccountID, token, existing.Name, existing.Language, existing.Category, existing.Components)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "Meta API update error: " + err.Error()})
	}
	existing.MetaTemplateID = metaID
	existing.Status = status

	updated, err := h.repo.Upsert(c.Request().Context(), existing)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, updated)
}

// DELETE /api/v1/waba/templates/:id
func (h *WABATemplateAPIHandler) Delete(c *echo.Context) error {
	wsID, ok := tenant.WorkspaceIDFrom(c.Request().Context())
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "missing workspace context"})
	}

	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid template ID"})
	}

	tmpl, err := h.repo.GetByID(c.Request().Context(), id)
	if err != nil || tmpl == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "template not found"})
	}
	if tmpl.WorkspaceID != wsID {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "template does not belong to active workspace"})
	}

	_, wabaAccountID, token, err := h.resolveWABAConfig(c, tmpl.ConnectionID, wsID)
	if err == nil {
		_ = h.metaClient.DeleteTemplate(c.Request().Context(), wabaAccountID, token, tmpl.Name)
	}

	if err := h.repo.Delete(c.Request().Context(), id); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to delete template locally"})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}

// POST /api/v1/waba/templates/sync
func (h *WABATemplateAPIHandler) Sync(c *echo.Context) error {
	wsID, ok := tenant.WorkspaceIDFrom(c.Request().Context())
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "missing workspace context"})
	}

	type syncReq struct {
		ConnectionID uuid.UUID `json:"connection_id"`
	}
	var req syncReq
	if err := c.Bind(&req); err != nil || req.ConnectionID == uuid.Nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "valid connection_id is required"})
	}

	_, wabaAccountID, token, err := h.resolveWABAConfig(c, req.ConnectionID, wsID)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	synced, err := h.metaClient.SyncTemplates(c.Request().Context(), req.ConnectionID, wabaAccountID, token, wsID, h.repo, false)
	if err != nil {
		if errors.Is(err, client.ErrSyncRateLimited) {
			return c.JSON(http.StatusTooManyRequests, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "sync failed: " + err.Error()})
	}

	return c.JSON(http.StatusOK, synced)
}
