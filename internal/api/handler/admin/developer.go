package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/pages"
)

// DeveloperHandler handles developer console, API keys, webhook secret rotation, and payload sandbox testing.
type DeveloperHandler struct {
	Workspaces    *repository.WorkspaceRepository
	APIKeys       *repository.APIKeyRepository
	SessionSecret []byte
	ExternalURL   string
}

// NewDeveloperHandler creates a new DeveloperHandler.
func NewDeveloperHandler(wsRepo *repository.WorkspaceRepository, apiKeyRepo *repository.APIKeyRepository, sessionSecret []byte, externalURL string) *DeveloperHandler {
	if len(sessionSecret) == 0 {
		sessionSecret = mw.GetSessionSecret()
	}
	return &DeveloperHandler{
		Workspaces:    wsRepo,
		APIKeys:       apiKeyRepo,
		SessionSecret: sessionSecret,
		ExternalURL:   externalURL,
	}
}

func (h *DeveloperHandler) getBaseURL(c *echo.Context) string {
	if h.ExternalURL != "" {
		return strings.TrimRight(h.ExternalURL, "/")
	}
	scheme := "http"
	if c.Request().TLS != nil || c.Request().Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := c.Request().Host
	if host == "" {
		host = "localhost:8080"
	}
	return fmt.Sprintf("%s://%s", scheme, host)
}

// GetPortal renders the Developer Console & Sandbox page.
func (h *DeveloperHandler) GetPortal(c *echo.Context) error {
	wsID, err := resolveWorkspaceID(c)
	if err != nil || wsID == uuid.Nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	ctx := c.Request().Context()
	ws, err := h.Workspaces.GetByID(ctx, wsID)
	if err != nil {
		return c.String(http.StatusNotFound, "workspace not found")
	}

	var keys []repository.APIKey
	if h.APIKeys != nil {
		keys, _ = h.APIKeys.ListByWorkspace(ctx, wsID)
	}

	baseURL := h.getBaseURL(c)

	initialClaims := SSOClaims{
		Sub:         "operador@crm.local",
		WorkspaceID: wsID.String(),
		Role:        "admin",
		Iat:         time.Now().Unix(),
		Exp:         time.Now().Unix() + 60,
	}
	initialToken, _ := GenerateSSOToken(initialClaims, h.SessionSecret)
	initialURL := fmt.Sprintf("%s/admin/sso?token=%s&redirect=%s", baseURL, initialToken, url.QueryEscape("/admin/connections"))

	data := pages.DeveloperPortalData{
		Workspace:       *ws,
		APIKeys:         keys,
		BaseURL:         baseURL,
		SSOInitialToken: initialToken,
		SSOInitialURL:   initialURL,
	}

	if mw.IsHTMX(c) && c.Request().Header.Get("HX-Target") == "main-content" {
		return mw.Render(c, http.StatusOK, pages.DeveloperContent(data))
	}
	return mw.Render(c, http.StatusOK, pages.DeveloperPage(data))
}

// CreateAPIKey generates a new API key for the active workspace.
func (h *DeveloperHandler) CreateAPIKey(c *echo.Context) error {
	wsID, err := resolveWorkspaceID(c)
	if err != nil || wsID == uuid.Nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		name = "API Key " + time.Now().Format("2006-01-02 15:04")
	}

	ctx := c.Request().Context()
	if h.APIKeys == nil {
		return c.String(http.StatusInternalServerError, "api key repository not configured")
	}

	_, rawKey, err := h.APIKeys.Create(ctx, wsID, name)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to create api key: "+err.Error())
	}

	keys, _ := h.APIKeys.ListByWorkspace(ctx, wsID)
	return mw.Render(c, http.StatusOK, pages.APIKeyCreatedResult(keys, rawKey, name))
}

// RevokeAPIKey revokes an API key for the active workspace.
func (h *DeveloperHandler) RevokeAPIKey(c *echo.Context) error {
	wsID, err := resolveWorkspaceID(c)
	if err != nil || wsID == uuid.Nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	keyIDStr, _ := echo.PathParam[string](c, "key_id")
	keyID, err := uuid.Parse(keyIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid key ID")
	}

	ctx := c.Request().Context()
	if h.APIKeys == nil {
		return c.String(http.StatusInternalServerError, "api key repository not configured")
	}

	if err := h.APIKeys.Revoke(ctx, keyID); err != nil {
		return c.String(http.StatusInternalServerError, "failed to revoke api key: "+err.Error())
	}

	keys, _ := h.APIKeys.ListByWorkspace(ctx, wsID)
	return mw.Render(c, http.StatusOK, pages.APIKeysList(keys))
}

// RotateWebhookSecret rotates the workspace HMAC-SHA256 signing secret.
func (h *DeveloperHandler) RotateWebhookSecret(c *echo.Context) error {
	wsID, err := resolveWorkspaceID(c)
	if err != nil || wsID == uuid.Nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	ctx := c.Request().Context()
	newSecret, err := h.Workspaces.GenerateWebhookSecret(ctx, wsID)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to generate webhook secret: "+err.Error())
	}

	return mw.Render(c, http.StatusOK, pages.WebhookSecretRow(&newSecret))
}

// SandboxTest validates and simulates an interactive message payload.
func (h *DeveloperHandler) SandboxTest(c *echo.Context) error {
	payloadRaw := strings.TrimSpace(c.FormValue("payload"))
	if payloadRaw == "" {
		res := pages.SandboxResultData{
			Status:       "invalid",
			ErrorMessage: "Payload JSON não pode ser vazio.",
		}
		return mw.Render(c, http.StatusOK, pages.SandboxValidationResult(res))
	}

	var req domain.CreateMessageRequest
	if err := json.Unmarshal([]byte(payloadRaw), &req); err != nil {
		res := pages.SandboxResultData{
			Status:       "invalid",
			ErrorMessage: "JSON inválido ou malformado: " + err.Error(),
		}
		return mw.Render(c, http.StatusOK, pages.SandboxValidationResult(res))
	}

	valErr := domain.ValidateMessage(&req)
	if valErr != nil {
		res := pages.SandboxResultData{
			Status:       "invalid",
			ErrorMessage: valErr.Message,
			FieldErrors:  valErr.Details,
		}
		return mw.Render(c, http.StatusOK, pages.SandboxValidationResult(res))
	}

	msgType := req.Type
	if msgType == "" {
		if req.Interactive != nil {
			msgType = "interactive (" + req.Interactive.Type + ")"
		} else if req.Product != nil {
			msgType = "product"
		} else if req.Media != nil {
			msgType = "media (" + req.Media.MediaType + ")"
		} else if req.TemplateName != "" {
			msgType = "template (" + req.TemplateName + ")"
		} else {
			msgType = "text"
		}
	}

	fallbackPolicy := req.FallbackBehavior
	if fallbackPolicy == "" {
		fallbackPolicy = "degrade (padrão)"
	}

	var degradedText string
	if req.Interactive != nil {
		degradedText = req.Interactive.DegradeToText()
	}

	res := pages.SandboxResultData{
		Status:         "valid",
		Channel:        req.Channel,
		Recipient:      req.To,
		MessageType:    msgType,
		DegradedText:   degradedText,
		FallbackPolicy: fallbackPolicy,
		RawJSON:        payloadRaw,
	}

	return mw.Render(c, http.StatusOK, pages.SandboxValidationResult(res))
}

// GenerateSSO handles HTMX requests to generate signed SSO token and URL.
func (h *DeveloperHandler) GenerateSSO(c *echo.Context) error {
	wsID, err := resolveWorkspaceID(c)
	if err != nil || wsID == uuid.Nil {
		return c.String(http.StatusBadRequest, "invalid workspace ID")
	}

	sub := strings.TrimSpace(c.FormValue("sub"))
	if sub == "" {
		sub = "operador@crm.local"
	}

	redirect := SanitizeRedirect(c.FormValue("redirect"))
	ttlStr := c.FormValue("ttl_seconds")
	ttl := 60
	if parsedTTL, err := strconv.Atoi(ttlStr); err == nil && parsedTTL >= 5 && parsedTTL <= 120 {
		ttl = parsedTTL
	}

	claims := SSOClaims{
		Sub:         sub,
		WorkspaceID: wsID.String(),
		Role:        "admin",
		Iat:         time.Now().Unix(),
		Exp:         time.Now().Unix() + int64(ttl),
	}

	token, err := GenerateSSOToken(claims, h.SessionSecret)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to generate sso token: "+err.Error())
	}

	baseURL := h.getBaseURL(c)
	fullURL := fmt.Sprintf("%s/admin/sso?token=%s&redirect=%s", baseURL, token, url.QueryEscape(redirect))
	curlCmd := fmt.Sprintf(`curl -i -L "%s"`, fullURL)

	result := pages.SSOGeneratorResultData{
		Token:       token,
		FullURL:     fullURL,
		Subject:     sub,
		Redirect:    redirect,
		ExpiresAt:   time.Unix(claims.Exp, 0).Format("15:04:05 (02/01/2006)"),
		TTLSeconds:  ttl,
		CurlCommand: curlCmd,
	}

	return mw.Render(c, http.StatusOK, pages.SSOGeneratorResult(result))
}
