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
	SetFlowWebhookURL(ctx context.Context, id uuid.UUID, flowWebhookURL *string) error
	SetMediaRetentionDays(ctx context.Context, id uuid.UUID, days int) error
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
	g.PATCH("/:id/retention", h.UpdateRetention)
}

// WorkspaceItem defines the JSON representation of a workspace entity in list responses.
type WorkspaceItem struct {
	ID                 uuid.UUID `json:"id"`
	Name               string    `json:"name"`
	PIIOptIn           bool      `json:"pii_opt_in"`
	WebhookSecret      *string   `json:"webhook_secret,omitempty"`
	FlowWebhookURL     *string   `json:"flow_webhook_url,omitempty"`
	MediaRetentionDays int       `json:"media_retention_days"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// ListWorkspacesResponse defines the JSON response returned by GET /api/v1/workspaces.
type ListWorkspacesResponse struct {
	Workspaces []WorkspaceItem `json:"workspaces"`
}

// CreateWorkspaceRequest defines the JSON payload for workspace provisioning.
type CreateWorkspaceRequest struct {
	Name                  string  `json:"name"`
	FlowWebhookURL        *string `json:"flow_webhook_url,omitempty"`
	MediaRetentionDays    *int    `json:"media_retention_days,omitempty"`
	GenerateAPIKey        *bool   `json:"generate_api_key,omitempty"`
	GenerateWebhookSecret *bool   `json:"generate_webhook_secret,omitempty"`
}

// CreateWorkspaceResponse defines the JSON response returned upon successful workspace provisioning.
type CreateWorkspaceResponse struct {
	ID                 uuid.UUID `json:"id"`
	Name               string    `json:"name"`
	APIKey             *string   `json:"api_key,omitempty"`
	WebhookSecret      *string   `json:"webhook_secret,omitempty"`
	FlowWebhookURL     *string   `json:"flow_webhook_url,omitempty"`
	MediaRetentionDays int       `json:"media_retention_days"`
	CreatedAt          time.Time `json:"created_at"`
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

	if req.FlowWebhookURL != nil && *req.FlowWebhookURL != "" {
		if err := h.wsRepo.SetFlowWebhookURL(ctx, ws.ID, req.FlowWebhookURL); err == nil {
			ws.FlowWebhookURL = req.FlowWebhookURL
		}
	}

	if req.MediaRetentionDays != nil {
		if err := h.wsRepo.SetMediaRetentionDays(ctx, ws.ID, *req.MediaRetentionDays); err == nil {
			ws.MediaRetentionDays = *req.MediaRetentionDays
		}
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
		ID:                 ws.ID,
		Name:               ws.Name,
		APIKey:             rawAPIKey,
		WebhookSecret:      webhookSec,
		FlowWebhookURL:     ws.FlowWebhookURL,
		MediaRetentionDays: ws.MediaRetentionDays,
		CreatedAt:          ws.CreatedAt,
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
			ID:                 ws.ID,
			Name:               ws.Name,
			PIIOptIn:           ws.PIIOptIn,
			WebhookSecret:      ws.WebhookSecret,
			FlowWebhookURL:     ws.FlowWebhookURL,
			MediaRetentionDays: ws.MediaRetentionDays,
			CreatedAt:          ws.CreatedAt,
			UpdatedAt:          ws.UpdatedAt,
		})
	}

	return c.JSON(http.StatusOK, ListWorkspacesResponse{
		Workspaces: items,
	})
}

// UpdateRetentionRequest defines the payload for updating workspace retention policy.
type UpdateRetentionRequest struct {
	MediaRetentionDays int `json:"media_retention_days"`
}

// UpdateRetention handles PATCH /api/v1/workspaces/:id/retention.
func (h *WorkspaceAPIHandler) UpdateRetention(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil || idStr == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "invalid workspace id",
		})
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "invalid workspace id",
		})
	}

	var req UpdateRetentionRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "invalid request body",
		})
	}

	if req.MediaRetentionDays < 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "media_retention_days cannot be negative",
		})
	}

	ctx := c.Request().Context()
	if err := h.wsRepo.SetMediaRetentionDays(ctx, id, req.MediaRetentionDays); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "failed to update retention policy",
		})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"status":               "updated",
		"workspace_id":         id,
		"media_retention_days": req.MediaRetentionDays,
	})
}
