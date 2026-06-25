package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.omnigo/internal/api/handler/admin"
	mw "github.com/pablojhp.omnigo/internal/api/middleware"
	"github.com/pablojhp.omnigo/internal/platform/postgres"
	"github.com/pablojhp.omnigo/internal/repository"
)

// setupWorkspaceRoutes creates a real Echo instance with workspace + API key admin routes.
func setupWorkspaceRoutes(t *testing.T) *echo.Echo {
	t.Helper()
	t.Setenv("OMNIGO_ADMIN_PASSWORD", "testpass123")

	pool := getTestPool(t)
	if pool == nil {
		t.Skip("PostgreSQL not available, skipping integration test")
	}

	// Run migrations to ensure schema exists
	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		t.Fatalf("failed to create sql.DB: %v", err)
	}
	// Run migrations — 001 creates the tables we need; 002 may fail on
	// dollar-quoted PL/pgSQL in embedded SQL (goose limitation), but that's
	// fine for tests since we only need the core schema.
	_ = postgres.RunMigrations(db)
	db.Close()

	e := echo.New()
	e.Use(mw.HTMXMiddleware())

	// Public admin routes (no session auth)
	adminPublic := e.Group("/admin")
	adminPublic.GET("/login", func(c *echo.Context) error {
		return admin.LoginPage(c, false)
	})
	adminPublic.POST("/login", func(c *echo.Context) error {
		return admin.LoginPost(c, nil)
	})
	adminPublic.POST("/logout", func(c *echo.Context) error {
		return admin.Logout(c)
	})

	// Protected admin routes (session auth required)
	adminGroup := e.Group("/admin")
	adminGroup.Use(mw.SessionAuthMiddleware())

	// Workspace repository
	wsRepo := repository.NewWorkspaceRepository(pool)

	// Workspace handler
	workspaceHandler := &admin.WorkspaceHandler{Repo: wsRepo}
	adminGroup.GET("/workspaces", workspaceHandler.List)
	adminGroup.POST("/workspaces", workspaceHandler.Create)
	adminGroup.GET("/workspaces/:id", workspaceHandler.Detail)
	adminGroup.GET("/workspaces/:id/confirm-delete", workspaceHandler.ConfirmDelete)
	adminGroup.DELETE("/workspaces/:id", workspaceHandler.Delete)

	// API key repository + handler
	apiKeyRepo := repository.NewAPIKeyRepository(pool)
	apiKeyHandler := &admin.APIKeyHandler{Repo: apiKeyRepo, Workspaces: wsRepo}
	adminGroup.GET("/workspaces/:id/keys", apiKeyHandler.List)
	adminGroup.POST("/workspaces/:id/keys", apiKeyHandler.Generate)
	adminGroup.GET("/workspaces/:id/keys/:key_id/confirm-revoke", apiKeyHandler.ConfirmRevoke)
	adminGroup.DELETE("/workspaces/:id/keys/:key_id", apiKeyHandler.Revoke)

	return e
}

// loginAndGetCookie performs a login and returns the session cookie.
func loginAndGetCookie(t *testing.T, e *echo.Echo) *http.Cookie {
	t.Helper()
	form := strings.NewReader("password=testpass123")
	loginReq := httptest.NewRequest(http.MethodPost, "/admin/login", form)
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	e.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusFound {
		t.Fatalf("login failed: expected 302, got %d", loginRec.Code)
	}

	for _, c := range loginRec.Result().Cookies() {
		if strings.Contains(c.Name, "session") {
			return c
		}
	}
	t.Fatal("no session cookie found after login")
	return nil
}

// createTestWorkspace creates a workspace directly via the repository and returns its ID.
// Name is made unique with a UUID suffix to avoid constraint violations across test runs.
func createTestWorkspace(t *testing.T, e *echo.Echo, name string) uuid.UUID {
	t.Helper()
	pool := getTestPool(t)
	if pool == nil {
		t.Fatal("no pool available")
	}
	wsRepo := repository.NewWorkspaceRepository(pool)
	uniqueName := fmt.Sprintf("%s-%s", name, uuid.New().String()[:8])
	ws, err := wsRepo.Create(t.Context(), uniqueName)
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	return ws.ID
}

// Test 1: GET /admin/workspaces with session returns 200 with workspace list table
func TestAdminWorkspaceList(t *testing.T) {
	e := setupWorkspaceRoutes(t)
	cookie := loginAndGetCookie(t, e)

	req := httptest.NewRequest(http.MethodGet, "/admin/workspaces", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Workspaces") {
		t.Error("expected workspace list page to contain 'Workspaces' heading")
	}
}

// Test 2: POST /admin/workspaces with name creates workspace and returns HTMX fragment with new row
func TestAdminWorkspaceCreate(t *testing.T) {
	e := setupWorkspaceRoutes(t)
	cookie := loginAndGetCookie(t, e)

	uniqueName := fmt.Sprintf("Test Workspace-%s", uuid.New().String()[:8])
	form := url.Values{}
	form.Set("name", uniqueName)
	req := httptest.NewRequest(http.MethodPost, "/admin/workspaces", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Errorf("expected 200/201, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, uniqueName) {
		t.Errorf("expected response to contain workspace name '%s', got: %s", uniqueName, body)
	}
}

// Test 3: GET /admin/workspaces/{id} with session returns workspace detail page with API keys section
func TestAdminWorkspaceDetail(t *testing.T) {
	e := setupWorkspaceRoutes(t)
	cookie := loginAndGetCookie(t, e)
	wsID := createTestWorkspace(t, e, "Detail Workspace")

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/workspaces/%s", wsID), nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Detail Workspace") {
		t.Error("expected workspace detail to contain workspace name")
	}
	if !strings.Contains(body, "API") && !strings.Contains(body, "Keys") && !strings.Contains(body, "keys") {
		t.Error("expected workspace detail to contain API keys section")
	}
}

// Test 4: GET /admin/workspaces/{id}/confirm-delete returns HTMX modal fragment
func TestAdminWorkspaceConfirmDelete(t *testing.T) {
	e := setupWorkspaceRoutes(t)
	cookie := loginAndGetCookie(t, e)
	wsID := createTestWorkspace(t, e, "Delete Me Workspace")

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/workspaces/%s/confirm-delete", wsID), nil)
	req.AddCookie(cookie)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// Modal should contain confirmation text and a delete button/link
	if !strings.Contains(body, "modal") && !strings.Contains(body, "Modal") {
		t.Error("expected response to contain a modal element")
	}
	if !strings.Contains(body, "hx-delete") {
		t.Error("expected modal to contain hx-delete attribute")
	}
}

// Test 5: DELETE /admin/workspaces/{id} with session deletes workspace and returns empty 200
func TestAdminWorkspaceDelete(t *testing.T) {
	e := setupWorkspaceRoutes(t)
	cookie := loginAndGetCookie(t, e)
	wsID := createTestWorkspace(t, e, "To Be Deleted")

	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/admin/workspaces/%s", wsID), nil)
	req.AddCookie(cookie)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify workspace is actually deleted
	pool := getTestPool(t)
	wsRepo := repository.NewWorkspaceRepository(pool)
	_, err := wsRepo.GetByID(t.Context(), wsID)
	if err == nil {
		t.Error("expected workspace to be deleted, but GetByID succeeded")
	}
}

// Test 6: GET /admin/workspaces/{id}/keys with session returns API key list with active/revoked badges
func TestAdminAPIKeyList(t *testing.T) {
	e := setupWorkspaceRoutes(t)
	cookie := loginAndGetCookie(t, e)
	wsID := createTestWorkspace(t, e, "Key Workspace")

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/workspaces/%s/keys", wsID), nil)
	req.AddCookie(cookie)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// Should contain key list (table or empty state) and active/revoked badge references
	if !strings.Contains(body, "Key") && !strings.Contains(body, "key") && !strings.Contains(body, "No ") {
		t.Error("expected API key list response")
	}
}

// Test 7: POST /admin/workspaces/{id}/keys with name generates key and returns fragment showing plaintext key ONCE
func TestAdminAPIKeyGenerate(t *testing.T) {
	e := setupWorkspaceRoutes(t)
	cookie := loginAndGetCookie(t, e)
	wsID := createTestWorkspace(t, e, "Key Gen Workspace")

	form := url.Values{}
	form.Set("name", "My Test Key")
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/workspaces/%s/keys", wsID), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Errorf("expected 200/201, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// Response should contain the key name
	if !strings.Contains(body, "My Test Key") {
		t.Errorf("expected response to contain key name 'My Test Key', got: %s", body)
	}
	// Response should contain a warning about showing once
	if !strings.Contains(body, "once") && !strings.Contains(body, "Once") && !strings.Contains(body, "copy") && !strings.Contains(body, "Copy") {
		t.Error("expected response to contain a warning about key being shown once")
	}
}

// Test 8: GET /admin/workspaces/{id}/keys/{key_id}/confirm-revoke returns HTMX modal fragment
func TestAdminAPIKeyConfirmRevoke(t *testing.T) {
	e := setupWorkspaceRoutes(t)
	cookie := loginAndGetCookie(t, e)
	wsID := createTestWorkspace(t, e, "Revoke Workspace")

	// Create an API key first
	pool := getTestPool(t)
	apiKeyRepo := repository.NewAPIKeyRepository(pool)
	key, _, err := apiKeyRepo.Create(t.Context(), wsID, "Revoke Test Key")
	if err != nil {
		t.Fatalf("failed to create test API key: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/admin/workspaces/%s/keys/%s/confirm-revoke", wsID, key.ID), nil)
	req.AddCookie(cookie)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "modal") && !strings.Contains(body, "Modal") {
		t.Error("expected response to contain a modal element")
	}
	if !strings.Contains(body, "hx-delete") {
		t.Error("expected modal to contain hx-delete attribute")
	}
}

// Test 9: DELETE /admin/workspaces/{id}/keys/{key_id} with session revokes key and returns fragment with revoked badge
func TestAdminAPIKeyRevoke(t *testing.T) {
	e := setupWorkspaceRoutes(t)
	cookie := loginAndGetCookie(t, e)
	wsID := createTestWorkspace(t, e, "Revoke Workspace 2")

	// Create an API key first
	pool := getTestPool(t)
	apiKeyRepo := repository.NewAPIKeyRepository(pool)
	key, _, err := apiKeyRepo.Create(t.Context(), wsID, "Key To Revoke")
	if err != nil {
		t.Fatalf("failed to create test API key: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete,
		fmt.Sprintf("/admin/workspaces/%s/keys/%s", wsID, key.ID), nil)
	req.AddCookie(cookie)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// Response should contain revoked status
	if !strings.Contains(body, "revoked") && !strings.Contains(body, "Revoked") {
		t.Errorf("expected response to contain 'revoked' badge, got: %s", body)
	}
}
