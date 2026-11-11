package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUniversalGateway_OAuthAuthorizeSecurity(t *testing.T) {
	secret := []byte("unit-test-session-secret-32bytes!")
	srv := NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, secret, "http://localhost:8080")
	gw := NewUniversalGateway(srv, "http://localhost:8080")
	gw.SetSessionSecret(secret)

	verifier := "pkce-verifier-with-high-entropy-0123456789abcdef"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	wsID := uuid.New()
	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {"cursor-agent"},
		"redirect_uri":          {"http://127.0.0.1:54321/callback"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {"xyz-state"},
		"workspace_id":          {wsID.String()},
	}
	targetURI := "/oauth/authorize?" + query.Encode()

	t.Run("unauthenticated GET redirects 302 to /admin/login with next param", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, targetURI, nil)
		rec := httptest.NewRecorder()

		gw.HandleOAuthAuthorize(rec, req)

		require.Equal(t, http.StatusFound, rec.Code)
		loc := rec.Header().Get("Location")
		assert.Equal(t, "/admin/login?next="+url.QueryEscape(targetURI), loc)
		assert.NotContains(t, loc, "code=")
	})

	t.Run("authenticated GET renders consent HTML and does NOT issue auth code", func(t *testing.T) {
		cookieVal := middleware.SignSessionCookieValue(secret)
		req := httptest.NewRequest(http.MethodGet, targetURI, nil)
		req.AddCookie(&http.Cookie{Name: "pergo-session", Value: cookieVal})
		rec := httptest.NewRecorder()

		gw.HandleOAuthAuthorize(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
		body := rec.Body.String()
		assert.Contains(t, body, "cursor-agent")
		assert.Contains(t, body, "http://127.0.0.1:54321/callback")
		assert.Contains(t, body, wsID.String())
		assert.Contains(t, body, gw.ComputeOAuthCSRFToken(cookieVal))
		assert.Empty(t, gw.OAuthManager().codes)
		assert.Empty(t, rec.Header().Get("Location"))
	})

	t.Run("authenticated POST with invalid CSRF token returns 403 Forbidden", func(t *testing.T) {
		cookieVal := middleware.SignSessionCookieValue(secret)
		form := url.Values{
			"response_type":         {"code"},
			"client_id":             {"cursor-agent"},
			"redirect_uri":          {"http://127.0.0.1:54321/callback"},
			"code_challenge":        {challenge},
			"code_challenge_method": {"S256"},
			"state":                 {"xyz-state"},
			"workspace_id":          {wsID.String()},
			"csrf_token":            {"forged-csrf-token"},
			"decision":              {"approve"},
		}
		req := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: "pergo-session", Value: cookieVal})
		rec := httptest.NewRecorder()

		gw.HandleOAuthAuthorize(rec, req)

		require.Equal(t, http.StatusForbidden, rec.Code)
		assert.Contains(t, rec.Body.String(), "invalid_csrf_token")
		assert.Empty(t, rec.Header().Get("Location"))
	})

	t.Run("authenticated POST with valid CSRF and decision=deny redirects with access_denied", func(t *testing.T) {
		cookieVal := middleware.SignSessionCookieValue(secret)
		csrf := gw.ComputeOAuthCSRFToken(cookieVal)
		form := url.Values{
			"response_type":         {"code"},
			"client_id":             {"cursor-agent"},
			"redirect_uri":          {"http://127.0.0.1:54321/callback"},
			"code_challenge":        {challenge},
			"code_challenge_method": {"S256"},
			"state":                 {"xyz-state"},
			"workspace_id":          {wsID.String()},
			"csrf_token":            {csrf},
			"decision":              {"deny"},
		}
		req := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: "pergo-session", Value: cookieVal})
		rec := httptest.NewRecorder()

		gw.HandleOAuthAuthorize(rec, req)

		require.Equal(t, http.StatusFound, rec.Code)
		loc, err := url.Parse(rec.Header().Get("Location"))
		require.NoError(t, err)
		assert.Equal(t, "access_denied", loc.Query().Get("error"))
		assert.Empty(t, loc.Query().Get("code"))
	})

	t.Run("authenticated POST with valid CSRF and decision=approve issues code exchangeable for token", func(t *testing.T) {
		cookieVal := middleware.SignSessionCookieValue(secret)
		csrf := gw.ComputeOAuthCSRFToken(cookieVal)
		form := url.Values{
			"response_type":         {"code"},
			"client_id":             {"cursor-agent"},
			"redirect_uri":          {"http://127.0.0.1:54321/callback"},
			"code_challenge":        {challenge},
			"code_challenge_method": {"S256"},
			"state":                 {"xyz-state"},
			"workspace_id":          {wsID.String()},
			"csrf_token":            {csrf},
			"decision":              {"approve"},
		}
		req := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: "pergo-session", Value: cookieVal})
		rec := httptest.NewRecorder()

		gw.HandleOAuthAuthorize(rec, req)

		require.Equal(t, http.StatusFound, rec.Code)
		loc, err := url.Parse(rec.Header().Get("Location"))
		require.NoError(t, err)
		code := loc.Query().Get("code")
		require.NotEmpty(t, code)
		assert.Equal(t, "xyz-state", loc.Query().Get("state"))

		tok, err := gw.OAuthManager().ExchangeToken("authorization_code", code, verifier, "cursor-agent", "http://127.0.0.1:54321/callback", "")
		require.NoError(t, err)
		require.NotEmpty(t, tok.AccessToken)

		boundWS, ok := gw.OAuthManager().ValidateAccessToken(tok.AccessToken)
		require.True(t, ok)
		assert.Equal(t, wsID, boundWS)
	})
}

func TestServer_ResolveWorkspaceID_SoloMode(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("skipping: PostgreSQL not available")
	}

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	ws, err := wsRepo.Create(ctx, "Solo Fallback Workspace")
	require.NoError(t, err)
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	srv := NewServer(wsRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, "")

	req := mcp.CallToolRequest{}
	// Default (non-solo) requires workspace_id when context has no scoped workspace
	_, errRes := srv.resolveWorkspaceID(ctx, req)
	require.NotNil(t, errRes)

	// Solo mode resolves the primary workspace automatically
	srv.SetSoloMode(true)
	resolvedID, errRes := srv.resolveWorkspaceID(ctx, req)
	require.Nil(t, errRes)
	assert.NotEqual(t, uuid.Nil, resolvedID)
}
