package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/platform/netpolicy"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
)

// WebhookSubscriptionRepo defines the repository operations required by WebhookSubscriptionAPIHandler.
type WebhookSubscriptionRepo interface {
	Create(ctx context.Context, wsID uuid.UUID, url string, eventTypes []string, secretPlaintext []byte) (*repository.WebhookSubscription, error)
	Get(ctx context.Context, id uuid.UUID) (*repository.WebhookSubscription, error)
	ListByWorkspace(ctx context.Context, wsID uuid.UUID) ([]*repository.WebhookSubscription, error)
	Update(ctx context.Context, id uuid.UUID, url string, eventTypes []string, active bool, secretPlaintext []byte) error
	Delete(ctx context.Context, id uuid.UUID) error
}

// WebhookSubscriptionAPIHandler exposes RESTful endpoints for managing webhook subscriptions programmatically.
type WebhookSubscriptionAPIHandler struct {
	repo      WebhookSubscriptionRepo
	allowlist []string
}

// WebhookSubscriptionOption configures WebhookSubscriptionAPIHandler.
type WebhookSubscriptionOption func(*WebhookSubscriptionAPIHandler)

// WithSubscriptionAllowlist configures permitted IP/host ranges for webhook URLs (e.g. for dev/testing).
func WithSubscriptionAllowlist(allowlist ...string) WebhookSubscriptionOption {
	return func(h *WebhookSubscriptionAPIHandler) {
		h.allowlist = append(h.allowlist, allowlist...)
	}
}

// NewWebhookSubscriptionAPIHandler creates a new WebhookSubscriptionAPIHandler instance.
func NewWebhookSubscriptionAPIHandler(repo WebhookSubscriptionRepo, opts ...WebhookSubscriptionOption) *WebhookSubscriptionAPIHandler {
	h := &WebhookSubscriptionAPIHandler{
		repo: repo,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// RegisterRoutes registers the webhook subscription endpoints on Echo router.
func (h *WebhookSubscriptionAPIHandler) RegisterRoutes(e *echo.Echo) {
	// Canonical REST routes (workspace inferred from Bearer token)
	subGroup := e.Group("/api/v1/webhooks/subscriptions")
	subGroup.POST("", h.Create)
	subGroup.POST("/", h.Create)
	subGroup.GET("", h.List)
	subGroup.GET("/", h.List)
	subGroup.GET("/:id", h.Get)
	subGroup.PUT("/:id", h.Update)
	subGroup.DELETE("/:id", h.Delete)

	// Workspace-scoped aliases
	wsSubGroup := e.Group("/api/v1/workspaces/:workspace_id/webhooks/subscriptions")
	wsSubGroup.POST("", h.Create)
	wsSubGroup.POST("/", h.Create)
	wsSubGroup.GET("", h.List)
	wsSubGroup.GET("/", h.List)
	wsSubGroup.GET("/:id", h.Get)
	wsSubGroup.PUT("/:id", h.Update)
	wsSubGroup.DELETE("/:id", h.Delete)
}

// CreateWebhookSubscriptionRequest represents the JSON request body to create a subscription.
type CreateWebhookSubscriptionRequest struct {
	URL        string   `json:"url"`
	Events     []string `json:"events,omitempty"`
	EventTypes []string `json:"event_types,omitempty"`
	Secret     string   `json:"secret,omitempty"`
	IsActive   *bool    `json:"is_active,omitempty"`
	Active     *bool    `json:"active,omitempty"`
}

// UpdateWebhookSubscriptionRequest represents the JSON request body to update a subscription.
type UpdateWebhookSubscriptionRequest struct {
	URL        *string  `json:"url,omitempty"`
	Events     []string `json:"events,omitempty"`
	EventTypes []string `json:"event_types,omitempty"`
	Secret     *string  `json:"secret,omitempty"`
	IsActive   *bool    `json:"is_active,omitempty"`
	Active     *bool    `json:"active,omitempty"`
}

// WebhookSubscriptionDTO represents the JSON model returned by subscription endpoints.
type WebhookSubscriptionDTO struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	URL         string    `json:"url"`
	Events      []string  `json:"events"`
	Secret      string    `json:"secret,omitempty"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// WebhookSubscriptionResponse wraps a single subscription.
type WebhookSubscriptionResponse struct {
	Subscription WebhookSubscriptionDTO `json:"subscription"`
}

// WebhookSubscriptionListResponse wraps a list of subscriptions.
type WebhookSubscriptionListResponse struct {
	Subscriptions []WebhookSubscriptionDTO `json:"subscriptions"`
}

func (h *WebhookSubscriptionAPIHandler) resolveWorkspaceID(c *echo.Context) (uuid.UUID, error) {
	wsID, ok := tenant.WorkspaceIDFrom(c.Request().Context())
	if ok && wsID != uuid.Nil {
		return wsID, nil
	}

	wsIDParam, _ := echo.PathParam[string](c, "workspace_id")
	if wsIDParam != "" {
		id, err := uuid.Parse(wsIDParam)
		if err == nil && id != uuid.Nil {
			return id, nil
		}
	}

	return uuid.Nil, errors.New("workspace context required")
}

// Create registers a new webhook subscription for the workspace.
// POST /api/v1/webhooks/subscriptions
func (h *WebhookSubscriptionAPIHandler) Create(c *echo.Context) error {
	wsID, err := h.resolveWorkspaceID(c)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{
			"code":    "unauthorized",
			"message": "workspace context required",
		})
	}

	var req CreateWebhookSubscriptionRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "invalid JSON body",
		})
	}

	urlStr := strings.TrimSpace(req.URL)
	if urlStr == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "url is required",
		})
	}

	// SSRF validation
	if err := netpolicy.ValidateURL(urlStr, h.allowlist...); err != nil {
		if errors.Is(err, netpolicy.ErrRestrictedIP) {
			return c.JSON(http.StatusUnprocessableEntity, map[string]string{
				"code":    "invalid_url",
				"message": "destination URL blocked by SSRF netpolicy (private/loopback IPs not allowed)",
			})
		}
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "invalid_url",
			"message": fmt.Sprintf("invalid webhook URL: %v", err),
		})
	}

	// Resolve events
	events := req.Events
	if len(events) == 0 {
		events = req.EventTypes
	}
	if len(events) == 0 {
		events = []string{"*"}
	}

	// Resolve signing secret
	secretStr := strings.TrimSpace(req.Secret)
	if secretStr == "" {
		randomSecret, err := generateRandomSecret()
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"code":    "internal_error",
				"message": "failed to generate signing secret",
			})
		}
		secretStr = randomSecret
	}

	// Resolve active state
	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	} else if req.Active != nil {
		active = *req.Active
	}

	sub, err := h.repo.Create(c.Request().Context(), wsID, urlStr, events, []byte(secretStr))
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "failed to create webhook subscription",
		})
	}

	// If requested active is false, update subscription active flag
	if !active {
		if err := h.repo.Update(c.Request().Context(), sub.ID, sub.URL, sub.EventTypes, false, nil); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"code":    "internal_error",
				"message": "failed to set subscription active status",
			})
		}
		sub.Active = false
	}

	resp := WebhookSubscriptionResponse{
		Subscription: WebhookSubscriptionDTO{
			ID:          sub.ID,
			WorkspaceID: sub.WorkspaceID,
			URL:         sub.URL,
			Events:      sub.EventTypes,
			Secret:      secretStr,
			IsActive:    sub.Active,
			CreatedAt:   sub.CreatedAt,
			UpdatedAt:   sub.UpdatedAt,
		},
	}

	return c.JSON(http.StatusCreated, resp)
}

// List returns all webhook subscriptions for the workspace.
// GET /api/v1/webhooks/subscriptions
func (h *WebhookSubscriptionAPIHandler) List(c *echo.Context) error {
	wsID, err := h.resolveWorkspaceID(c)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{
			"code":    "unauthorized",
			"message": "workspace context required",
		})
	}

	subs, err := h.repo.ListByWorkspace(c.Request().Context(), wsID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "failed to list webhook subscriptions",
		})
	}

	dtos := make([]WebhookSubscriptionDTO, 0, len(subs))
	for _, s := range subs {
		dtos = append(dtos, WebhookSubscriptionDTO{
			ID:          s.ID,
			WorkspaceID: s.WorkspaceID,
			URL:         s.URL,
			Events:      s.EventTypes,
			IsActive:    s.Active,
			CreatedAt:   s.CreatedAt,
			UpdatedAt:   s.UpdatedAt,
		})
	}

	return c.JSON(http.StatusOK, WebhookSubscriptionListResponse{
		Subscriptions: dtos,
	})
}

// Get returns a single webhook subscription by ID.
// GET /api/v1/webhooks/subscriptions/:id
func (h *WebhookSubscriptionAPIHandler) Get(c *echo.Context) error {
	wsID, err := h.resolveWorkspaceID(c)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{
			"code":    "unauthorized",
			"message": "workspace context required",
		})
	}

	idParam, _ := echo.PathParam[string](c, "id")
	subID, err := uuid.Parse(idParam)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "invalid subscription id",
		})
	}

	sub, err := h.repo.Get(c.Request().Context(), subID)
	if err != nil {
		if errors.Is(err, repository.ErrWebhookSubscriptionNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{
				"code":    "not_found",
				"message": "webhook subscription not found",
			})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "failed to get webhook subscription",
		})
	}

	// Workspace isolation check
	if sub.WorkspaceID != wsID {
		return c.JSON(http.StatusNotFound, map[string]string{
			"code":    "not_found",
			"message": "webhook subscription not found",
		})
	}

	return c.JSON(http.StatusOK, WebhookSubscriptionResponse{
		Subscription: WebhookSubscriptionDTO{
			ID:          sub.ID,
			WorkspaceID: sub.WorkspaceID,
			URL:         sub.URL,
			Events:      sub.EventTypes,
			IsActive:    sub.Active,
			CreatedAt:   sub.CreatedAt,
			UpdatedAt:   sub.UpdatedAt,
		},
	})
}

// Update modifies an existing webhook subscription.
// PUT /api/v1/webhooks/subscriptions/:id
func (h *WebhookSubscriptionAPIHandler) Update(c *echo.Context) error {
	wsID, err := h.resolveWorkspaceID(c)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{
			"code":    "unauthorized",
			"message": "workspace context required",
		})
	}

	idParam, _ := echo.PathParam[string](c, "id")
	subID, err := uuid.Parse(idParam)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "invalid subscription id",
		})
	}

	sub, err := h.repo.Get(c.Request().Context(), subID)
	if err != nil {
		if errors.Is(err, repository.ErrWebhookSubscriptionNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{
				"code":    "not_found",
				"message": "webhook subscription not found",
			})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "failed to fetch webhook subscription",
		})
	}

	// Workspace isolation check
	if sub.WorkspaceID != wsID {
		return c.JSON(http.StatusNotFound, map[string]string{
			"code":    "not_found",
			"message": "webhook subscription not found",
		})
	}

	var req UpdateWebhookSubscriptionRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "invalid JSON body",
		})
	}

	targetURL := sub.URL
	if req.URL != nil {
		trimmed := strings.TrimSpace(*req.URL)
		if trimmed == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"code":    "bad_request",
				"message": "url cannot be empty",
			})
		}
		if err := netpolicy.ValidateURL(trimmed, h.allowlist...); err != nil {
			if errors.Is(err, netpolicy.ErrRestrictedIP) {
				return c.JSON(http.StatusUnprocessableEntity, map[string]string{
					"code":    "invalid_url",
					"message": "destination URL blocked by SSRF netpolicy (private/loopback IPs not allowed)",
				})
			}
			return c.JSON(http.StatusBadRequest, map[string]string{
				"code":    "invalid_url",
				"message": fmt.Sprintf("invalid webhook URL: %v", err),
			})
		}
		targetURL = trimmed
	}

	targetEvents := sub.EventTypes
	if len(req.Events) > 0 {
		targetEvents = req.Events
	} else if len(req.EventTypes) > 0 {
		targetEvents = req.EventTypes
	}

	targetActive := sub.Active
	if req.IsActive != nil {
		targetActive = *req.IsActive
	} else if req.Active != nil {
		targetActive = *req.Active
	}

	var secretBytes []byte
	if req.Secret != nil && *req.Secret != "" && *req.Secret != "********" {
		secretBytes = []byte(*req.Secret)
	}

	if err := h.repo.Update(c.Request().Context(), subID, targetURL, targetEvents, targetActive, secretBytes); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "failed to update webhook subscription",
		})
	}

	sub.URL = targetURL
	sub.EventTypes = targetEvents
	sub.Active = targetActive
	sub.UpdatedAt = time.Now()

	return c.JSON(http.StatusOK, WebhookSubscriptionResponse{
		Subscription: WebhookSubscriptionDTO{
			ID:          sub.ID,
			WorkspaceID: sub.WorkspaceID,
			URL:         sub.URL,
			Events:      sub.EventTypes,
			IsActive:    sub.Active,
			CreatedAt:   sub.CreatedAt,
			UpdatedAt:   sub.UpdatedAt,
		},
	})
}

// Delete removes a webhook subscription.
// DELETE /api/v1/webhooks/subscriptions/:id
func (h *WebhookSubscriptionAPIHandler) Delete(c *echo.Context) error {
	wsID, err := h.resolveWorkspaceID(c)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{
			"code":    "unauthorized",
			"message": "workspace context required",
		})
	}

	idParam, _ := echo.PathParam[string](c, "id")
	subID, err := uuid.Parse(idParam)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"code":    "bad_request",
			"message": "invalid subscription id",
		})
	}

	sub, err := h.repo.Get(c.Request().Context(), subID)
	if err != nil {
		if errors.Is(err, repository.ErrWebhookSubscriptionNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{
				"code":    "not_found",
				"message": "webhook subscription not found",
			})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "failed to fetch webhook subscription",
		})
	}

	// Workspace isolation check
	if sub.WorkspaceID != wsID {
		return c.JSON(http.StatusNotFound, map[string]string{
			"code":    "not_found",
			"message": "webhook subscription not found",
		})
	}

	if err := h.repo.Delete(c.Request().Context(), subID); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"code":    "internal_error",
			"message": "failed to delete webhook subscription",
		})
	}

	return c.JSON(http.StatusOK, map[string]string{
		"status": "deleted",
		"id":     subID.String(),
	})
}

func generateRandomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}
