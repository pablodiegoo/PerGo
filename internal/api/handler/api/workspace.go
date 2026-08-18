package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/repository"
)

// WorkspaceRepo defines data access operations required for workspace provisioning and management.
type WorkspaceRepo interface {
	Create(ctx context.Context, name string) (*repository.Workspace, error)
	GenerateWebhookSecret(ctx context.Context, id uuid.UUID) (string, error)
	List(ctx context.Context, limit int) ([]repository.Workspace, error)
}

// APIKeyRepo defines data access operations required for API key provisioning.
type APIKeyRepo interface {
	Create(ctx context.Context, workspaceID uuid.UUID, name string) (*repository.APIKey, string, error)
}

// WorkspaceAPIHandler handles programmatic workspace provisioning and tenant management REST API requests.
type WorkspaceAPIHandler struct {
	wsRepo     WorkspaceRepo
	apiKeyRepo APIKeyRepo
}

// NewWorkspaceAPIHandler creates a new WorkspaceAPIHandler.
func NewWorkspaceAPIHandler(wsRepo WorkspaceRepo, apiKeyRepo APIKeyRepo) *WorkspaceAPIHandler {
	return &WorkspaceAPIHandler{
		wsRepo:     wsRepo,
		apiKeyRepo: apiKeyRepo,
	}
}

// RegisterRoutes registers the workspace API routes with Echo and applies master authentication.
func (h *WorkspaceAPIHandler) RegisterRoutes(e *echo.Echo, masterAuth echo.MiddlewareFunc) {
	g := e.Group("/api/v1/workspaces")
	if masterAuth != nil {
		g.Use(masterAuth)
	}
	g.POST("", h.Create)
	g.POST("/", h.Create)
	g.GET("", h.List)
	g.GET("/", h.List)
}

// WorkspaceItem defines the JSON representation of a workspace entity in list responses.
type WorkspaceItem struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	PIIOptIn      bool      `json:"pii_opt_in"`
	WebhookSecret *string   `json:"webhook_secret,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// ListWorkspacesResponse defines the JSON response returned by GET /api/v1/workspaces.
type ListWorkspacesResponse struct {
	Workspaces []WorkspaceItem `json:"workspaces"`
}

// CreateWorkspaceRequest defines the JSON payload for workspace provisioning.
type CreateWorkspaceRequest struct {
	Name                  string `json:"name"`
	GenerateAPIKey        *bool  `json:"generate_api_key,omitempty"`
	GenerateWebhookSecret *bool  `json:"generate_webhook_secret,omitempty"`
}

// CreateWorkspaceResponse defines the JSON response returned upon successful workspace provisioning.
type CreateWorkspaceResponse struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	APIKey        *string   `json:"api_key,omitempty"`
	WebhookSecret *string   `json:"webhook_secret,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// Create handles POST /api/v1/workspaces to provision a new workspace, default API key, and webhook secret.
func (h *WorkspaceAPIHandler) Create(c *echo.Context) error {
	var req CreateWorkspaceRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "invalid request body",
		})
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "workspace name is required",
		})
	}

	ctx := c.Request().Context()
	ws, err := h.wsRepo.Create(ctx, name)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "failed to create workspace",
		})
	}

	var rawAPIKey *string
	genKey := req.GenerateAPIKey == nil || *req.GenerateAPIKey
	if genKey && h.apiKeyRepo != nil {
		_, rawKey, err := h.apiKeyRepo.Create(ctx, ws.ID, "Default API Key")
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"code":    "internal_error",
				"message": "failed to generate api key",
			})
		}
		rawAPIKey = &rawKey
	}

	var webhookSec *string
	genSecret := req.GenerateWebhookSecret == nil || *req.GenerateWebhookSecret
	if genSecret {
		secret, err := h.wsRepo.GenerateWebhookSecret(ctx, ws.ID)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"code":    "internal_error",
				"message": "failed to generate webhook secret",
			})
		}
		webhookSec = &secret
	}

	res := CreateWorkspaceResponse{
		ID:            ws.ID,
		Name:          ws.Name,
		APIKey:        rawAPIKey,
		WebhookSecret: webhookSec,
		CreatedAt:     ws.CreatedAt,
	}

	return c.JSON(http.StatusCreated, res)
}

// List handles GET /api/v1/workspaces to list all tenant workspaces.
func (h *WorkspaceAPIHandler) List(c *echo.Context) error {
	ctx := c.Request().Context()
	limit := 50
	if limitStr := c.QueryParam("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			if parsedLimit > 500 {
				parsedLimit = 500
			}
			limit = parsedLimit
		}
	}

	workspaces, err := h.wsRepo.List(ctx, limit)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "failed to list workspaces",
		})
	}

	items := make([]WorkspaceItem, 0, len(workspaces))
	for _, ws := range workspaces {
		items = append(items, WorkspaceItem{
			ID:            ws.ID,
			Name:          ws.Name,
			PIIOptIn:      ws.PIIOptIn,
			WebhookSecret: ws.WebhookSecret,
			CreatedAt:     ws.CreatedAt,
			UpdatedAt:     ws.UpdatedAt,
		})
	}

	return c.JSON(http.StatusOK, ListWorkspacesResponse{
		Workspaces: items,
	})
}
