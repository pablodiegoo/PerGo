package api

import (
	"context"
	"net/http"
	"net/url"
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
	CreateWithID(ctx context.Context, id uuid.UUID, name string) (*repository.Workspace, error)
	GenerateWebhookSecret(ctx context.Context, id uuid.UUID) (string, error)
	SetFlowWebhookURL(ctx context.Context, id uuid.UUID, flowWebhookURL *string) error
	SetMediaRetentionDays(ctx context.Context, id uuid.UUID, days int) error
	List(ctx context.Context, limit int) ([]repository.Workspace, error)
	AddMember(ctx context.Context, member *repository.WorkspaceMember) error
	RemoveMemberByEmail(ctx context.Context, workspaceID uuid.UUID, email string) error
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
	for _, prefix := range []string{"/api/v1/workspaces", "/api/v1/master/workspaces"} {
		g := e.Group(prefix)
		if masterAuth != nil {
			g.Use(masterAuth)
		}
		g.POST("", h.Create)
		g.POST("/", h.Create)
		g.GET("", h.List)
		g.GET("/", h.List)
		g.PATCH("/:id/retention", h.UpdateRetention)
		g.POST("/:id/members", h.UpsertMember)
		g.POST("/:id/members/", h.UpsertMember)
		g.DELETE("/:id/members/:email", h.RemoveMember)
	}
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

// WorkspaceMemberInput defines the JSON payload for workspace member creation or upsert.
type WorkspaceMemberInput struct {
	Name                  string      `json:"name"`
	Email                 string      `json:"email"`
	Role                  string      `json:"role"`
	AssignedConnectionIDs []uuid.UUID `json:"assigned_connection_ids,omitempty"`
}

// CreateWorkspaceRequest defines the JSON payload for workspace provisioning.
type CreateWorkspaceRequest struct {
	WorkspaceID           *uuid.UUID             `json:"workspace_id,omitempty"`
	Name                  string                 `json:"name"`
	FlowWebhookURL        *string                `json:"flow_webhook_url,omitempty"`
	MediaRetentionDays    *int                   `json:"media_retention_days,omitempty"`
	GenerateAPIKey        *bool                  `json:"generate_api_key,omitempty"`
	GenerateWebhookSecret *bool                  `json:"generate_webhook_secret,omitempty"`
	Members               []WorkspaceMemberInput `json:"members,omitempty"`
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
	var ws *repository.Workspace
	var err error
	if req.WorkspaceID != nil && *req.WorkspaceID != uuid.Nil {
		ws, err = h.wsRepo.CreateWithID(ctx, *req.WorkspaceID, name)
	} else {
		ws, err = h.wsRepo.Create(ctx, name)
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "failed to create workspace",
		})
	}

	for _, mInput := range req.Members {
		email := strings.TrimSpace(mInput.Email)
		if email == "" {
			continue
		}
		role := strings.TrimSpace(mInput.Role)
		if role == "" {
			role = "operator"
		}
		name := strings.TrimSpace(mInput.Name)
		if name == "" {
			name = email
		}
		assignedConns := mInput.AssignedConnectionIDs
		if assignedConns == nil {
			assignedConns = []uuid.UUID{}
		}
		m := &repository.WorkspaceMember{
			WorkspaceID:           ws.ID,
			Name:                  name,
			Email:                 email,
			Role:                  role,
			AssignedConnectionIDs: assignedConns,
		}
		if err := h.wsRepo.AddMember(ctx, m); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"code":    "internal_error",
				"message": "failed to add member",
			})
		}
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

// UpsertMember handles POST /api/v1/master/workspaces/:id/members
// and POST /api/v1/workspaces/:id/members.
func (h *WorkspaceAPIHandler) UpsertMember(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil || idStr == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "invalid workspace id",
		})
	}
	wsID, err := uuid.Parse(idStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "invalid workspace id",
		})
	}

	var req WorkspaceMemberInput
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "invalid request body",
		})
	}

	email := strings.TrimSpace(req.Email)
	if email == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "email is required",
		})
	}

	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = "operator"
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = email
	}
	assignedConns := req.AssignedConnectionIDs
	if assignedConns == nil {
		assignedConns = []uuid.UUID{}
	}

	member := &repository.WorkspaceMember{
		WorkspaceID:           wsID,
		Name:                  name,
		Email:                 email,
		Role:                  role,
		AssignedConnectionIDs: assignedConns,
	}

	ctx := c.Request().Context()
	if err := h.wsRepo.AddMember(ctx, member); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "failed to upsert member",
		})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"status": "ok",
		"member": member,
	})
}

// RemoveMember handles DELETE /api/v1/master/workspaces/:id/members/:email
// and DELETE /api/v1/workspaces/:id/members/:email.
func (h *WorkspaceAPIHandler) RemoveMember(c *echo.Context) error {
	idStr, err := echo.PathParam[string](c, "id")
	if err != nil || idStr == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "invalid workspace id",
		})
	}
	wsID, err := uuid.Parse(idStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "invalid workspace id",
		})
	}

	email, err := echo.PathParam[string](c, "email")
	if err != nil || strings.TrimSpace(email) == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "invalid member email",
		})
	}

	if unescaped, err := url.PathUnescape(email); err == nil && unescaped != "" {
		email = unescaped
	}

	ctx := c.Request().Context()
	if err := h.wsRepo.RemoveMemberByEmail(ctx, wsID, email); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "failed to remove member",
		})
	}

	return c.JSON(http.StatusOK, map[string]string{
		"status": "ok",
	})
}

