package admin

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/pages"
)

// HeadlessAdminHandler handles Headless CPaaS developer portal and SSO generator in Admin UI.
type HeadlessAdminHandler struct {
	Workspaces    *repository.WorkspaceRepository
	APIKeys       *repository.APIKeyRepository
	SessionSecret []byte
	ExternalURL   string
}

// NewHeadlessAdminHandler creates a new HeadlessAdminHandler instance.
func NewHeadlessAdminHandler(wsRepo *repository.WorkspaceRepository, apiKeyRepo *repository.APIKeyRepository, sessionSecret []byte, externalURL string) *HeadlessAdminHandler {
	if len(sessionSecret) == 0 {
		sessionSecret = mw.GetSessionSecret()
	}
	return &HeadlessAdminHandler{
		Workspaces:    wsRepo,
		APIKeys:       apiKeyRepo,
		SessionSecret: sessionSecret,
		ExternalURL:   externalURL,
	}
}

func (h *HeadlessAdminHandler) getBaseURL(c *echo.Context) string {
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

// GetPortal renders the Headless Developer & SSO Portal page.
func (h *HeadlessAdminHandler) GetPortal(c *echo.Context) error {
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

	data := pages.HeadlessPortalData{
		Workspace:       *ws,
		APIKeys:         keys,
		BaseURL:         baseURL,
		SSOInitialToken: initialToken,
		SSOInitialURL:   initialURL,
	}

	if mw.IsHTMX(c) && c.Request().Header.Get("HX-Target") == "main-content" {
		return mw.Render(c, http.StatusOK, pages.HeadlessIntegrationContent(data))
	}
	return mw.Render(c, http.StatusOK, pages.HeadlessIntegrationPage(data))
}

// GenerateSSO handles HTMX requests to generate signed SSO token and URL.
func (h *HeadlessAdminHandler) GenerateSSO(c *echo.Context) error {
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
