package admin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/handler/admin"
	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestSanitizeRedirect(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty string", "", "/admin/"},
		{"valid admin root", "/admin/", "/admin/"},
		{"valid subpath", "/admin/devices", "/admin/devices"},
		{"valid subpath with query", "/admin/inbox?tag=vip", "/admin/inbox?tag=vip"},
		{"absolute URL http", "http://evil.com/phishing", "/admin/"},
		{"absolute URL https", "https://evil.com/phishing", "/admin/"},
		{"protocol relative double slash", "//evil.com/phishing", "/admin/"},
		{"protocol relative triple slash", "///evil.com/phishing", "/admin/"},
		{"backslash attempt", "/\\evil.com", "/admin/"},
		{"embedded backslash", "/admin\\evil.com", "/admin/"},
		{"javascript scheme", "javascript:alert(1)", "/admin/"},
		{"data scheme", "data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==", "/admin/"},
		{"CRLF injection", "/admin/\r\nSet-Cookie: evil=true", "/admin/"},
		{"LF injection", "/admin/\nLocation: https://evil.com", "/admin/"},
		{"spaces trimmed", "  /admin/tags  ", "/admin/tags"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := admin.SanitizeRedirect(tt.input)
			if got != tt.expected {
				t.Errorf("SanitizeRedirect(%q) = %q; want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestVerifySSOToken(t *testing.T) {
	secret := []byte("test-sso-secret-32-bytes-long!")
	wsID := uuid.New().String()

	t.Run("valid token", func(t *testing.T) {
		token, err := admin.GenerateSSOToken(admin.SSOClaims{
			Sub:         "operator@example.com",
			WorkspaceID: wsID,
			Role:        "admin",
		}, secret)
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		claims, err := admin.VerifySSOToken(token, secret)
		if err != nil {
			t.Fatalf("expected valid token, got error: %v", err)
		}
		if claims.Sub != "operator@example.com" {
			t.Errorf("expected sub 'operator@example.com', got %q", claims.Sub)
		}
		if claims.WorkspaceID != wsID {
			t.Errorf("expected workspace_id %q, got %q", wsID, claims.WorkspaceID)
		}
		if claims.Role != "admin" {
			t.Errorf("expected role 'admin', got %q", claims.Role)
		}
	})

	t.Run("expired token", func(t *testing.T) {
		token, err := admin.GenerateSSOToken(admin.SSOClaims{
			Sub:         "operator@example.com",
			WorkspaceID: wsID,
			Iat:         time.Now().Unix() - 100,
			Exp:         time.Now().Unix() - 10,
		}, secret)
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		_, err = admin.VerifySSOToken(token, secret)
		if err == nil || !strings.Contains(err.Error(), "expired") {
			t.Errorf("expected token expired error, got: %v", err)
		}
	})

	t.Run("TTL exceeds 120 seconds", func(t *testing.T) {
		now := time.Now().Unix()
		token, err := admin.GenerateSSOToken(admin.SSOClaims{
			Sub:         "operator@example.com",
			WorkspaceID: wsID,
			Iat:         now,
			Exp:         now + 300, // 300 seconds TTL > 120
		}, secret)
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		_, err = admin.VerifySSOToken(token, secret)
		if err == nil || !strings.Contains(err.Error(), "ttl exceeds") {
			t.Errorf("expected TTL exceeded error, got: %v", err)
		}
	})

	t.Run("future issued token", func(t *testing.T) {
		now := time.Now().Unix()
		token, err := admin.GenerateSSOToken(admin.SSOClaims{
			Sub:         "operator@example.com",
			WorkspaceID: wsID,
			Iat:         now + 300,
			Exp:         now + 360,
		}, secret)
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		_, err = admin.VerifySSOToken(token, secret)
		if err == nil || !strings.Contains(err.Error(), "future") {
			t.Errorf("expected future token error, got: %v", err)
		}
	})

	t.Run("invalid HMAC signature", func(t *testing.T) {
		token, err := admin.GenerateSSOToken(admin.SSOClaims{
			Sub:         "operator@example.com",
			WorkspaceID: wsID,
		}, secret)
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		wrongSecret := []byte("completely-wrong-session-secret!")
		_, err = admin.VerifySSOToken(token, wrongSecret)
		if err == nil || !strings.Contains(err.Error(), "invalid signature") {
			t.Errorf("expected invalid signature error, got: %v", err)
		}
	})

	t.Run("invalid role claim", func(t *testing.T) {
		token, err := admin.GenerateSSOToken(admin.SSOClaims{
			Sub:         "operator@example.com",
			WorkspaceID: wsID,
			Role:        "guest",
		}, secret)
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		_, err = admin.VerifySSOToken(token, secret)
		if err == nil || !strings.Contains(err.Error(), "invalid role") {
			t.Errorf("expected invalid role error, got: %v", err)
		}
	})

	t.Run("malformed token parts", func(t *testing.T) {
		_, err := admin.VerifySSOToken("not-a-token", secret)
		if err == nil {
			t.Errorf("expected error for malformed token, got nil")
		}
	})
}

func TestHandleSSO(t *testing.T) {
	secret := []byte("test-sso-secret-for-handler-32b!")
	e := echo.New()

	dbURL := testDBURL
	if dbURL == "" {
		dbURL = os.Getenv("PERGO_DATABASE_URL")
	}

	var pool *pgxpool.Pool
	var wsRepo *repository.WorkspaceRepository
	var ws *repository.Workspace
	var cleanup func()

	if dbURL != "" {
		ctx := context.Background()
		var err error
		pool, err = postgres.NewPool(ctx, dbURL)
		if err == nil {
			wsRepo = repository.NewWorkspaceRepository(pool)
			ws, _ = wsRepo.Create(ctx, "sso_test_ws_"+uuid.New().String()[:8])
			cleanup = func() {
				if ws != nil {
					_ = wsRepo.Delete(ctx, ws.ID)
				}
				pool.Close()
			}
		}
	}

	if cleanup != nil {
		defer cleanup()
	}

	targetWsID := uuid.New()
	if ws != nil {
		targetWsID = ws.ID
	}

	handler := admin.NewSSOHandler(wsRepo, secret)

	t.Run("success with workspace and default redirect", func(t *testing.T) {
		token, err := admin.GenerateSSOToken(admin.SSOClaims{
			Sub:         "admin@example.com",
			WorkspaceID: targetWsID.String(),
			Role:        "admin",
		}, secret)
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/admin/sso?token="+token, nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		if err := handler.HandleSSO(c); err != nil {
			t.Fatalf("HandleSSO returned unexpected error: %v", err)
		}

		if rec.Code != http.StatusFound {
			t.Errorf("expected status 302, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		loc := rec.Header().Get("Location")
		if loc != "/admin/" {
			t.Errorf("expected Location '/admin/', got %q", loc)
		}

		// Verify cookies
		cookies := rec.Result().Cookies()
		var sessionCookie, wsCookie *http.Cookie
		for _, ck := range cookies {
			if ck.Name == "pergo-session" {
				sessionCookie = ck
			}
			if ck.Name == "pergo-active-workspace" {
				wsCookie = ck
			}
		}

		if sessionCookie == nil {
			t.Fatal("expected pergo-session cookie to be set")
		}
		if !mw.VerifySessionCookie(sessionCookie.Value, secret) {
			t.Errorf("pergo-session cookie failed cryptographic verification")
		}

		if wsCookie == nil {
			t.Fatal("expected pergo-active-workspace cookie to be set")
		}
		if wsCookie.Value != targetWsID.String() {
			t.Errorf("expected pergo-active-workspace %q, got %q", targetWsID.String(), wsCookie.Value)
		}
	})

	t.Run("success with custom safe redirect", func(t *testing.T) {
		token, err := admin.GenerateSSOToken(admin.SSOClaims{
			Sub:         "admin@example.com",
			WorkspaceID: targetWsID.String(),
		}, secret)
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/admin/sso?token="+token+"&redirect=/admin/devices", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		if err := handler.HandleSSO(c); err != nil {
			t.Fatalf("HandleSSO returned unexpected error: %v", err)
		}

		if rec.Code != http.StatusFound {
			t.Errorf("expected status 302, got %d", rec.Code)
		}

		loc := rec.Header().Get("Location")
		if loc != "/admin/devices" {
			t.Errorf("expected Location '/admin/devices', got %q", loc)
		}
	})

	t.Run("sanitizes open redirect attempts to /admin/", func(t *testing.T) {
		token, err := admin.GenerateSSOToken(admin.SSOClaims{
			Sub:         "admin@example.com",
			WorkspaceID: targetWsID.String(),
		}, secret)
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/admin/sso?token="+token+"&redirect=https://evil.com/steal-creds", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		if err := handler.HandleSSO(c); err != nil {
			t.Fatalf("HandleSSO returned unexpected error: %v", err)
		}

		if rec.Code != http.StatusFound {
			t.Errorf("expected status 302, got %d", rec.Code)
		}

		loc := rec.Header().Get("Location")
		if loc != "/admin/" {
			t.Errorf("expected open redirect to be sanitized to '/admin/', got %q", loc)
		}
	})

	t.Run("rejects missing token with 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/sso", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		if err := handler.HandleSSO(c); err != nil {
			t.Fatalf("HandleSSO returned unexpected error: %v", err)
		}

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401 for missing token, got %d", rec.Code)
		}
	})

	t.Run("rejects expired token with 401", func(t *testing.T) {
		token, err := admin.GenerateSSOToken(admin.SSOClaims{
			Sub:         "admin@example.com",
			WorkspaceID: targetWsID.String(),
			Iat:         time.Now().Unix() - 100,
			Exp:         time.Now().Unix() - 5,
		}, secret)
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/admin/sso?token="+token, nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		if err := handler.HandleSSO(c); err != nil {
			t.Fatalf("HandleSSO returned unexpected error: %v", err)
		}

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401 for expired token, got %d", rec.Code)
		}
	})

	t.Run("rejects forged token signature with 401", func(t *testing.T) {
		forgedSecret := []byte("forged-secret-attacker-cannot-guess")
		token, err := admin.GenerateSSOToken(admin.SSOClaims{
			Sub:         "admin@example.com",
			WorkspaceID: targetWsID.String(),
		}, forgedSecret)
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/admin/sso?token="+token, nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		if err := handler.HandleSSO(c); err != nil {
			t.Fatalf("HandleSSO returned unexpected error: %v", err)
		}

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401 for forged token, got %d", rec.Code)
		}
	})

	t.Run("rejects non-existent workspace ID when repo is configured", func(t *testing.T) {
		if wsRepo == nil {
			t.Skip("skipping workspace existence check because database is not available")
		}

		nonExistentID := uuid.New().String()
		token, err := admin.GenerateSSOToken(admin.SSOClaims{
			Sub:         "admin@example.com",
			WorkspaceID: nonExistentID,
		}, secret)
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/admin/sso?token="+token, nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		if err := handler.HandleSSO(c); err != nil {
			t.Fatalf("HandleSSO returned unexpected error: %v", err)
		}

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401 for non-existent workspace, got %d", rec.Code)
		}
	})

	t.Run("accepts token from Authorization Bearer header", func(t *testing.T) {
		token, err := admin.GenerateSSOToken(admin.SSOClaims{
			Sub:         "admin@example.com",
			WorkspaceID: targetWsID.String(),
		}, secret)
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/admin/sso", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		if err := handler.HandleSSO(c); err != nil {
			t.Fatalf("HandleSSO returned unexpected error: %v", err)
		}

		if rec.Code != http.StatusFound {
			t.Errorf("expected status 302, got %d", rec.Code)
		}
	})
}
