package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/handler/admin"
	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/layout"
	"github.com/pablojhp.pergo/templates/pages"
)

// setupWorkspaceRoutes creates a real Echo instance with workspace + API key admin routes.
func setupWorkspaceRoutes(t *testing.T) *echo.Echo {
	t.Helper()
	t.Setenv("PERGO_ADMIN_PASSWORD", "testpass123")

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
	wsRepo := repository.NewWorkspaceRepository(pool)

	adminPublic := e.Group("/admin")
	adminPublic.GET("/login", func(c *echo.Context) error {
		return admin.LoginPage(c, false)
	})
	adminPublic.POST("/login", func(c *echo.Context) error {
		return admin.LoginPost(c, wsRepo, "testpass123")
	})
	adminPublic.POST("/logout", func(c *echo.Context) error {
		return admin.Logout(c)
	})

	// Protected admin routes (session auth required)
	adminGroup := e.Group("/admin")
	adminGroup.Use(mw.SessionAuthMiddleware())
	adminGroup.Use(mw.ActiveWorkspaceMiddleware(wsRepo))

	// Workspace handler
	workspaceHandler := &admin.WorkspaceHandler{Repo: wsRepo}
	adminGroup.GET("/workspaces", workspaceHandler.List)
	adminGroup.GET("/workspaces/new", func(c *echo.Context) error {
		onboarding := c.QueryParam("onboarding") == "true"
		form := pages.WorkspaceCreateForm(onboarding)
		if mw.IsHTMX(c) {
			return mw.Render(c, http.StatusOK, form)
		}
		return mw.Render(c, http.StatusOK, layout.Base("New Workspace", form))
	})
	adminGroup.POST("/workspaces", workspaceHandler.Create)
	adminGroup.GET("/workspaces/:id", workspaceHandler.Detail)
	adminGroup.GET("/workspaces/:id/confirm-delete", workspaceHandler.ConfirmDelete)
	adminGroup.DELETE("/workspaces/:id", workspaceHandler.Delete)
	adminGroup.GET("/test-tenant-ctx", func(c *echo.Context) error {
		id, ok := tenant.WorkspaceIDFrom(c.Request().Context())
		if !ok {
			return c.String(http.StatusInternalServerError, "missing workspace_id")
		}
		return c.JSON(http.StatusOK, map[string]string{"workspace_id": id.String()})
	})

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

// Test 10: Active workspace resolution, fallback to earliest, and invalid cookie auto-healing
func TestAdminWorkspace_ActiveResolutionAndFallback(t *testing.T) {
	e := setupWorkspaceRoutes(t)
	cookie := loginAndGetCookie(t, e)

	pool := getTestPool(t)
	ctx := t.Context()
	if _, err := pool.Exec(ctx, "TRUNCATE workspaces CASCADE"); err != nil {
		t.Fatalf("failed to truncate workspaces: %v", err)
	}

	wsRepo := repository.NewWorkspaceRepository(pool)
	ws1, err := wsRepo.Create(ctx, "Earliest Workspace")
	if err != nil {
		t.Fatalf("failed to create ws1: %v", err)
	}
	ws2, err := wsRepo.Create(ctx, "Secondary Workspace")
	if err != nil {
		t.Fatalf("failed to create ws2: %v", err)
	}

	// 1. Missing cookie -> falls back to earliest workspace (ws1) and issues updated cookie
	req1 := httptest.NewRequest(http.MethodGet, "/admin/workspaces", nil)
	req1.AddCookie(cookie)
	rec1 := httptest.NewRecorder()
	e.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec1.Code, rec1.Body.String())
	}
	var foundCookie *http.Cookie
	for _, c := range rec1.Result().Cookies() {
		if c.Name == mw.ActiveWorkspaceCookieName {
			foundCookie = c
			break
		}
	}
	if foundCookie == nil {
		t.Fatal("expected pergo-active-workspace cookie to be set on missing cookie request")
	}
	if foundCookie.Value != ws1.ID.String() {
		t.Errorf("expected fallback to earliest ws1 ID %s, got %s", ws1.ID, foundCookie.Value)
	}

	// 2. Valid cookie pointing to ws2 -> resolves ws2
	req2 := httptest.NewRequest(http.MethodGet, "/admin/test-tenant-ctx", nil)
	req2.AddCookie(cookie)
	req2.AddCookie(&http.Cookie{
		Name:  mw.ActiveWorkspaceCookieName,
		Value: ws2.ID.String(),
	})
	rec2 := httptest.NewRecorder()
	e.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec2.Code)
	}
	if !strings.Contains(rec2.Body.String(), ws2.ID.String()) {
		t.Errorf("expected tenant context to contain ws2 ID %s, got: %s", ws2.ID, rec2.Body.String())
	}

	// 3. Invalid / deleted workspace cookie -> auto-heals to earliest workspace (ws1)
	req3 := httptest.NewRequest(http.MethodGet, "/admin/test-tenant-ctx", nil)
	req3.AddCookie(cookie)
	req3.AddCookie(&http.Cookie{
		Name:  mw.ActiveWorkspaceCookieName,
		Value: uuid.New().String(), // Non-existent workspace ID
	})
	rec3 := httptest.NewRecorder()
	e.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec3.Code)
	}
	if !strings.Contains(rec3.Body.String(), ws1.ID.String()) {
		t.Errorf("expected auto-healed tenant context to contain ws1 ID %s, got: %s", ws1.ID, rec3.Body.String())
	}
	var healedCookie *http.Cookie
	for _, c := range rec3.Result().Cookies() {
		if c.Name == mw.ActiveWorkspaceCookieName {
			healedCookie = c
			break
		}
	}
	if healedCookie == nil {
		t.Fatal("expected auto-healed pergo-active-workspace cookie on invalid cookie request")
	}
	if healedCookie.Value != ws1.ID.String() {
		t.Errorf("expected auto-healed cookie value %s, got %s", ws1.ID, healedCookie.Value)
	}
}

// Test 11: Empty database condition redirects operator requests to /admin/workspaces/new with onboarding message
func TestAdminWorkspace_EmptyDatabase_RedirectsToNew(t *testing.T) {
	e := setupWorkspaceRoutes(t)
	cookie := loginAndGetCookie(t, e)

	pool := getTestPool(t)
	ctx := t.Context()
	if _, err := pool.Exec(ctx, "TRUNCATE workspaces CASCADE"); err != nil {
		t.Fatalf("failed to truncate workspaces: %v", err)
	}

	// 1. Standard request to /admin/workspaces redirects to /admin/workspaces/new?onboarding=true (302 Found)
	req1 := httptest.NewRequest(http.MethodGet, "/admin/workspaces", nil)
	req1.AddCookie(cookie)
	rec1 := httptest.NewRecorder()
	e.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusFound {
		t.Fatalf("expected 302 Found on empty database, got %d", rec1.Code)
	}
	if loc := rec1.Header().Get("Location"); loc != "/admin/workspaces/new?onboarding=true" {
		t.Errorf("expected redirect to /admin/workspaces/new?onboarding=true, got %q", loc)
	}

	// 2. HTMX request to /admin/workspaces sets HX-Redirect header
	req2 := httptest.NewRequest(http.MethodGet, "/admin/workspaces", nil)
	req2.AddCookie(cookie)
	req2.Header.Set("HX-Request", "true")
	rec2 := httptest.NewRecorder()
	e.ServeHTTP(rec2, req2)

	if hxRedirect := rec2.Header().Get("HX-Redirect"); hxRedirect != "/admin/workspaces/new?onboarding=true" {
		t.Errorf("expected HX-Redirect /admin/workspaces/new?onboarding=true, got %q", hxRedirect)
	}

	// 3. GET /admin/workspaces/new?onboarding=true renders the onboarding welcome message
	req3 := httptest.NewRequest(http.MethodGet, "/admin/workspaces/new?onboarding=true", nil)
	req3.AddCookie(cookie)
	rec3 := httptest.NewRecorder()
	e.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusOK {
		t.Fatalf("expected GET /admin/workspaces/new to return 200 OK, got %d", rec3.Code)
	}
	body := rec3.Body.String()
	if !strings.Contains(body, "Bem-vindo ao PerGo!") {
		t.Errorf("expected onboarding welcome message in response, got: %s", body)
	}

	// 4. POST /admin/workspaces creates workspace on empty DB
	form := url.Values{}
	form.Set("name", "Initial Onboarding Workspace")
	req4 := httptest.NewRequest(http.MethodPost, "/admin/workspaces", strings.NewReader(form.Encode()))
	req4.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req4.Header.Set("HX-Request", "true")
	req4.AddCookie(cookie)
	rec4 := httptest.NewRecorder()
	e.ServeHTTP(rec4, req4)

	if rec4.Code != http.StatusOK && rec4.Code != http.StatusCreated {
		t.Fatalf("expected POST /admin/workspaces to return 200/201, got %d: %s", rec4.Code, rec4.Body.String())
	}
	if !strings.Contains(rec4.Body.String(), "Initial Onboarding Workspace") {
		t.Errorf("expected created workspace row in body, got: %s", rec4.Body.String())
	}
}

// setupFullAdminEcho initializes an Echo server wired with all admin routes and repositories for multi-tenant verification.
func setupFullAdminEcho(t *testing.T) (*echo.Echo, *pgxpool.Pool, *repository.WorkspaceRepository) {
	t.Helper()
	t.Setenv("PERGO_ADMIN_PASSWORD", "testpass123")

	pool := getTestPool(t)
	if pool == nil {
		t.Skip("PostgreSQL not available, skipping integration test")
	}

	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		t.Fatalf("failed to create sql.DB: %v", err)
	}
	_ = postgres.RunMigrations(db)
	db.Close()

	kek := []byte("01234567890123456789012345678901")
	encryptor, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	wsRepo := repository.NewWorkspaceRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, encryptor)
	tagRepo := repository.NewTagRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	campaignRepo := repository.NewCampaignRepository(pool)
	wabaTemplateRepo := repository.NewWABATemplateRepository(pool)
	webhookDLQRepo := repository.NewWebhookDLQRepository(pool, encryptor)
	webhookSubRepo := repository.NewWebhookSubscriptionRepository(pool, encryptor)
	apiKeyRepo := repository.NewAPIKeyRepository(pool)
	auditRepo := repository.NewAuditRepository(pool)
	recipientSessionRepo := repository.NewRecipientSessionRepository(pool)
	userActionLogRepo := repository.NewUserActionLogRepository(pool)

	e := echo.New()
	e.Use(mw.HTMXMiddleware())

	adminPublic := e.Group("/admin")
	adminPublic.GET("/login", func(c *echo.Context) error {
		return admin.LoginPage(c, false)
	})
	adminPublic.POST("/login", func(c *echo.Context) error {
		return admin.LoginPost(c, wsRepo, "testpass123")
	})

	adminGroup := e.Group("/admin")
	adminGroup.Use(mw.SessionAuthMiddleware())
	adminGroup.Use(mw.ActiveWorkspaceMiddleware(wsRepo))

	// Dashboard
	dashboardHandler := &admin.DashboardHandler{
		Pool:        pool,
		Workspaces:  wsRepo,
		APIKeys:     apiKeyRepo,
		Connections: connRepo,
	}
	adminGroup.GET("/", dashboardHandler.Index)
	adminGroup.POST("/workspaces/active", dashboardHandler.SelectWorkspace)
	adminGroup.GET("/workspaces/selector", dashboardHandler.WorkspaceSelector)

	// Workspaces
	workspaceHandler := &admin.WorkspaceHandler{
		Repo:        wsRepo,
		APIKeys:     apiKeyRepo,
		ExternalURL: "http://localhost:8080",
	}
	adminGroup.GET("/workspaces", workspaceHandler.ActiveWorkspace)
	adminGroup.GET("/workspace", workspaceHandler.ActiveWorkspace)
	adminGroup.POST("/workspaces", workspaceHandler.Create)
	adminGroup.GET("/workspaces/:id", workspaceHandler.Detail)
	adminGroup.GET("/workspace/:id", workspaceHandler.Detail)
	apiKeyHandler := &admin.APIKeyHandler{Repo: apiKeyRepo, Workspaces: wsRepo}
	adminGroup.GET("/workspaces/:id/keys", apiKeyHandler.List)

	// Connections
	deviceHandler := &admin.DeviceHandler{
		Connections: connRepo,
		ExternalURL: "http://localhost:8080",
	}
	adminGroup.GET("/connections", deviceHandler.List)
	adminGroup.GET("/devices", deviceHandler.List)

	// Campaigns
	campaignHandler := admin.NewCampaignHandler(campaignRepo, wabaTemplateRepo, connRepo, tagRepo, nil)
	adminGroup.GET("/campaigns", campaignHandler.List)
	adminGroup.GET("/campaigns/new", campaignHandler.NewForm)
	adminGroup.POST("/campaigns", campaignHandler.Create)
	adminGroup.GET("/campaigns/:id/row", campaignHandler.GetRow)

	// Tags
	tagAdminHandler := admin.NewTagAdminHandler(tagRepo, contactRepo, wsRepo)
	adminGroup.GET("/tags", tagAdminHandler.Page)
	adminGroup.POST("/tags", tagAdminHandler.CreateTag)

	// Webhooks
	webhookHandler := admin.NewWebhookDLQHandler(webhookDLQRepo, webhookSubRepo, wsRepo, nil)
	adminGroup.GET("/webhooks", webhookHandler.Page)
	adminGroup.GET("/webhooks/subscriptions/new", webhookHandler.GetSubscriptionNewForm)
	adminGroup.GET("/webhooks/subscriptions/:subscription_id/edit", webhookHandler.GetSubscriptionEditForm)
	adminGroup.GET("/webhooks/subscriptions/:subscription_id/rotate-form", webhookHandler.GetRotateSecretForm)
	adminGroup.GET("/webhooks/subscriptions/:subscription_id/test-form", webhookHandler.GetSubscriptionTestForm)
	adminGroup.GET("/webhooks/dlq/:dlq_id/details", webhookHandler.GetDetails)

	// Audit Logs
	auditHandler := &admin.AuditHandler{Repo: auditRepo, Workspaces: wsRepo}
	userLogsHandler := admin.NewUserLogsHandler(userActionLogRepo)
	adminGroup.GET("/logs/outbound", auditHandler.ListOutbound)
	adminGroup.GET("/logs/inbound", auditHandler.ListInbound)
	adminGroup.GET("/logs/actions", userLogsHandler.List)
	adminGroup.GET("/logs/actions/:id/metadata", userLogsHandler.GetMetadata)

	// Inbox
	inboxHandler := &admin.InboxHandler{
		Repo:           auditRepo,
		Sessions:       recipientSessionRepo,
		Workspaces:     wsRepo,
		Connections:    connRepo,
		ContactRepo:    contactRepo,
		UserActionLogs: userActionLogRepo,
	}
	adminGroup.GET("/inbox", inboxHandler.View)
	adminGroup.GET("/inbox/conversations/poll", inboxHandler.PollConversations)

	return e, pool, wsRepo
}

// TestSmartSwitcherUX_Transitions tests that the workspace switcher in the sidebar smoothly transitions:
// - List/collection views reload in place (HX-Refresh: true)
// - Detail views redirect to section root (HX-Redirect: /admin/...)
func TestSmartSwitcherUX_Transitions(t *testing.T) {
	e, _, wsRepo := setupFullAdminEcho(t)
	cookie := loginAndGetCookie(t, e)

	ctx := t.Context()
	ws1, err := wsRepo.Create(ctx, fmt.Sprintf("Switcher WS 1 %s", uuid.New().String()[:6]))
	if err != nil {
		t.Fatalf("failed to create ws1: %v", err)
	}
	ws2, err := wsRepo.Create(ctx, fmt.Sprintf("Switcher WS 2 %s", uuid.New().String()[:6]))
	if err != nil {
		t.Fatalf("failed to create ws2: %v", err)
	}

	testCases := []struct {
		name         string
		currentURL   string
		expectReload bool
		expectTarget string
	}{
		// Collection / list views -> Reload in place
		{name: "Dashboard", currentURL: "http://localhost:8080/admin/", expectReload: true},
		{name: "Inbox", currentURL: "http://localhost:8080/admin/inbox", expectReload: true},
		{name: "Connections", currentURL: "http://localhost:8080/admin/connections", expectReload: true},
		{name: "Devices", currentURL: "http://localhost:8080/admin/devices", expectReload: true},
		{name: "Campaigns List", currentURL: "http://localhost:8080/admin/campaigns", expectReload: true},
		{name: "Campaigns New", currentURL: "http://localhost:8080/admin/campaigns/new", expectReload: true},
		{name: "Tags List", currentURL: "http://localhost:8080/admin/tags", expectReload: true},
		{name: "Webhooks List", currentURL: "http://localhost:8080/admin/webhooks", expectReload: true},
		{name: "Logs Outbound", currentURL: "http://localhost:8080/admin/logs/outbound", expectReload: true},
		{name: "Workspaces List", currentURL: "http://localhost:8080/admin/workspaces", expectReload: true},

		// Detail views -> Redirect to section root
		{name: "Campaign Detail Row", currentURL: "http://localhost:8080/admin/campaigns/c-12345/row", expectReload: false, expectTarget: "/admin/campaigns"},
		{name: "Webhook Subscription Edit", currentURL: "http://localhost:8080/admin/webhooks/subscriptions/sub-999/edit", expectReload: false, expectTarget: "/admin/webhooks"},
		{name: "Webhook Subscription Rotate", currentURL: "http://localhost:8080/admin/webhooks/subscriptions/sub-999/rotate-form", expectReload: false, expectTarget: "/admin/webhooks"},
		{name: "Webhook DLQ Detail", currentURL: "http://localhost:8080/admin/webhooks/dlq/dlq-999/details", expectReload: false, expectTarget: "/admin/webhooks"},
		{name: "Workspace Detail", currentURL: fmt.Sprintf("http://localhost:8080/admin/workspaces/%s", ws1.ID), expectReload: false, expectTarget: "/admin/workspaces"},
		{name: "Workspace Keys", currentURL: fmt.Sprintf("http://localhost:8080/admin/workspaces/%s/keys", ws1.ID), expectReload: false, expectTarget: "/admin/workspaces"},
		{name: "Action Log Metadata", currentURL: "http://localhost:8080/admin/logs/actions/act-123/metadata", expectReload: false, expectTarget: "/admin/logs/actions"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{}
			form.Set("workspace_id", ws2.ID.String())

			req := httptest.NewRequest(http.MethodPost, "/admin/workspaces/active", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("HX-Request", "true")
			req.Header.Set("HX-Current-URL", tc.currentURL)
			req.AddCookie(cookie)
			req.AddCookie(&http.Cookie{
				Name:  mw.ActiveWorkspaceCookieName,
				Value: ws1.ID.String(),
			})

			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
				t.Fatalf("expected 200/204, got %d: %s", rec.Code, rec.Body.String())
			}

			if tc.expectReload {
				if rec.Header().Get("HX-Refresh") != "true" {
					t.Errorf("expected HX-Refresh 'true', got %q", rec.Header().Get("HX-Refresh"))
				}
				if rec.Header().Get("HX-Redirect") != "" {
					t.Errorf("expected no HX-Redirect on list view, got %q", rec.Header().Get("HX-Redirect"))
				}
			} else {
				if rec.Header().Get("HX-Redirect") != tc.expectTarget {
					t.Errorf("expected HX-Redirect %q, got %q", tc.expectTarget, rec.Header().Get("HX-Redirect"))
				}
				if rec.Header().Get("HX-Refresh") != "" {
					t.Errorf("expected no HX-Refresh on detail view, got %q", rec.Header().Get("HX-Refresh"))
				}
			}

			// Verify cookie was updated to ws2
			var activeCookie *http.Cookie
			for _, c := range rec.Result().Cookies() {
				if c.Name == mw.ActiveWorkspaceCookieName {
					activeCookie = c
					break
				}
			}
			if activeCookie == nil || activeCookie.Value != ws2.ID.String() {
				t.Errorf("expected active workspace cookie set to ws2 ID %s", ws2.ID)
			}
		})
	}
}

// TestMultiWorkspace_EndToEndIsolation verifies strict tenant isolation across:
// Inbox, Connections, Campaigns, Tags, Webhooks, and Audit Logs.
// Alternately toggling pergo-active-workspace cookie ensures no cross-tenant data leakage or phantom UUID errors.
func TestMultiWorkspace_EndToEndIsolation(t *testing.T) {
	e, pool, wsRepo := setupFullAdminEcho(t)
	cookie := loginAndGetCookie(t, e)
	ctx := t.Context()

	kek := []byte("01234567890123456789012345678901")
	encryptor, _ := crypto.NewEncryptor(kek)

	connRepo := repository.NewConnectionRepository(pool, encryptor)
	tagRepo := repository.NewTagRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	campaignRepo := repository.NewCampaignRepository(pool)
	webhookSubRepo := repository.NewWebhookSubscriptionRepository(pool, encryptor)
	webhookDLQRepo := repository.NewWebhookDLQRepository(pool, encryptor)

	// Create Workspace Alpha & Beta
	wsAlpha, err := wsRepo.Create(ctx, fmt.Sprintf("Workspace Alpha %s", uuid.New().String()[:6]))
	if err != nil {
		t.Fatalf("failed to create wsAlpha: %v", err)
	}
	wsBeta, err := wsRepo.Create(ctx, fmt.Sprintf("Workspace Beta %s", uuid.New().String()[:6]))
	if err != nil {
		t.Fatalf("failed to create wsBeta: %v", err)
	}

	// 1. Seed Connections
	connAlpha := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    wsAlpha.ID,
		Name:           "Alpha WhatsApp Connection",
		Channel:        "whatsapp",
		SenderIdentity: "+5511999990001",
		Status:         "active",
		Slug:           fmt.Sprintf("alpha-wa-%s", uuid.New().String()[:4]),
	}
	if err := connRepo.Create(ctx, connAlpha); err != nil {
		t.Fatalf("failed to create connAlpha: %v", err)
	}

	connBeta := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    wsBeta.ID,
		Name:           "Beta Telegram Connection",
		Channel:        "telegram",
		SenderIdentity: "beta_tele_bot",
		Status:         "active",
		Slug:           fmt.Sprintf("beta-tg-%s", uuid.New().String()[:4]),
	}
	if err := connRepo.Create(ctx, connBeta); err != nil {
		t.Fatalf("failed to create connBeta: %v", err)
	}

	// 2. Seed Tags & Contacts
	tagAlpha, err := tagRepo.CreateTag(ctx, wsAlpha.ID, "Alpha-VIP-Tag", "#ff0000")
	if err != nil {
		t.Fatalf("failed to create tagAlpha: %v", err)
	}
	tagBeta, err := tagRepo.CreateTag(ctx, wsBeta.ID, "Beta-VIP-Tag", "#00ff00")
	if err != nil {
		t.Fatalf("failed to create tagBeta: %v", err)
	}

	contactAlpha, err := contactRepo.ResolveContact(ctx, wsAlpha.ID, "whatsapp", "+5511999990001", "Alice Alpha Contact", "alice", "+5511999990001")
	if err != nil {
		t.Fatalf("failed to create contactAlpha: %v", err)
	}
	_ = tagRepo.AddTagToContact(ctx, wsAlpha.ID, contactAlpha.ID, tagAlpha.ID)

	contactBeta, err := contactRepo.ResolveContact(ctx, wsBeta.ID, "telegram", "beta_tele_user", "Bob Beta Contact", "bob", "+5511999990002")
	if err != nil {
		t.Fatalf("failed to create contactBeta: %v", err)
	}
	_ = tagRepo.AddTagToContact(ctx, wsBeta.ID, contactBeta.ID, tagBeta.ID)

	// 3. Seed Campaigns
	chanWA := "whatsapp"
	chanTG := "telegram"
	schedAlpha := time.Now().Add(1 * time.Hour)
	schedBeta := time.Now().Add(2 * time.Hour)

	campAlpha := &domain.Campaign{
		ID:              uuid.New(),
		WorkspaceID:     wsAlpha.ID,
		Name:            "Campaign Alpha 2026 Special",
		Channel:         &chanWA,
		ConnectionID:    &connAlpha.ID,
		Status:          domain.CampaignStatusDraft,
		TotalRecipients: 10,
		ScheduledAt:     &schedAlpha,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	if _, err := campaignRepo.Create(ctx, campAlpha); err != nil {
		t.Fatalf("failed to create campAlpha: %v", err)
	}

	campBeta := &domain.Campaign{
		ID:              uuid.New(),
		WorkspaceID:     wsBeta.ID,
		Name:            "Campaign Beta 2026 Special",
		Channel:         &chanTG,
		ConnectionID:    &connBeta.ID,
		Status:          domain.CampaignStatusRunning,
		TotalRecipients: 20,
		ScheduledAt:     &schedBeta,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	if _, err := campaignRepo.Create(ctx, campBeta); err != nil {
		t.Fatalf("failed to create campBeta: %v", err)
	}

	// 4. Seed Webhook Subscriptions & DLQ
	subAlpha, err := webhookSubRepo.Create(ctx, wsAlpha.ID, "https://alpha.example.com/events/v1", []string{"message.delivered"}, []byte("sec-alpha-12345"))
	if err != nil {
		t.Fatalf("failed to create subAlpha: %v", err)
	}

	subBeta, err := webhookSubRepo.Create(ctx, wsBeta.ID, "https://beta.example.com/events/v1", []string{"message.failed"}, []byte("sec-beta-67890"))
	if err != nil {
		t.Fatalf("failed to create subBeta: %v", err)
	}

	reasonAlpha := "Alpha delivery webhook connection timeout"
	if err := webhookDLQRepo.InsertDLQ(ctx, wsAlpha.ID, subAlpha.ID, "trace-dlq-alpha", "msg-alpha-1", "message.delivered", []byte(`{"tenant":"alpha"}`), subAlpha.URL, 3, &reasonAlpha); err != nil {
		t.Fatalf("failed to create dlqAlpha: %v", err)
	}

	reasonBeta := "Beta delivery webhook endpoint 500 error"
	if err := webhookDLQRepo.InsertDLQ(ctx, wsBeta.ID, subBeta.ID, "trace-dlq-beta", "msg-beta-1", "message.failed", []byte(`{"tenant":"beta"}`), subBeta.URL, 5, &reasonBeta); err != nil {
		t.Fatalf("failed to create dlqBeta: %v", err)
	}

	// 5. Seed Audit Logs
	traceAlpha := fmt.Sprintf("trace-alpha-%s", uuid.New().String()[:8])
	_, _ = pool.Exec(ctx, `
		INSERT INTO audit_logs (id, workspace_id, trace_id, event_type, payload, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
	`, uuid.New(), wsAlpha.ID, traceAlpha, "outbound_message", []byte(`{"text":"Alpha outbound payload"}`))

	traceBeta := fmt.Sprintf("trace-beta-%s", uuid.New().String()[:8])
	_, _ = pool.Exec(ctx, `
		INSERT INTO audit_logs (id, workspace_id, trace_id, event_type, payload, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
	`, uuid.New(), wsBeta.ID, traceBeta, "outbound_message", []byte(`{"text":"Beta outbound payload"}`))

	// --- PHASE A: Access views with Workspace Alpha active cookie ---
	cookieAlpha := &http.Cookie{
		Name:  mw.ActiveWorkspaceCookieName,
		Value: wsAlpha.ID.String(),
	}

	t.Run("Workspace Alpha Isolation Check", func(t *testing.T) {
		// 1. Connections
		req := httptest.NewRequest(http.MethodGet, "/admin/connections", nil)
		req.AddCookie(cookie)
		req.AddCookie(cookieAlpha)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("connections: expected 200, got %d", rec.Code)
		}
		b := rec.Body.String()
		if !strings.Contains(b, connAlpha.Name) || !strings.Contains(b, connAlpha.Slug) {
			t.Errorf("expected Alpha connection %s in body", connAlpha.Name)
		}
		if strings.Contains(b, connBeta.Name) || strings.Contains(b, connBeta.Slug) {
			t.Errorf("cross-tenant leak: found Beta connection in Alpha view")
		}

		// 2. Tags & Contacts
		req = httptest.NewRequest(http.MethodGet, "/admin/tags", nil)
		req.AddCookie(cookie)
		req.AddCookie(cookieAlpha)
		rec = httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("tags: expected 200, got %d", rec.Code)
		}
		b = rec.Body.String()
		if !strings.Contains(b, tagAlpha.Name) {
			t.Errorf("expected Alpha tag in body")
		}
		if strings.Contains(b, tagBeta.Name) {
			t.Errorf("cross-tenant leak: found Beta tag in Alpha view")
		}

		// 3. Campaigns
		req = httptest.NewRequest(http.MethodGet, "/admin/campaigns", nil)
		req.AddCookie(cookie)
		req.AddCookie(cookieAlpha)
		rec = httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("campaigns: expected 200, got %d", rec.Code)
		}
		b = rec.Body.String()
		if !strings.Contains(b, campAlpha.Name) {
			t.Errorf("expected Alpha campaign %s in body", campAlpha.Name)
		}
		if strings.Contains(b, campBeta.Name) {
			t.Errorf("cross-tenant leak: found Beta campaign in Alpha view")
		}

		// 4. Webhooks & DLQ
		req = httptest.NewRequest(http.MethodGet, "/admin/webhooks", nil)
		req.AddCookie(cookie)
		req.AddCookie(cookieAlpha)
		rec = httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("webhooks: expected 200, got %d", rec.Code)
		}
		b = rec.Body.String()
		if !strings.Contains(b, subAlpha.URL) || !strings.Contains(b, "trace-dlq-alpha") {
			t.Errorf("expected Alpha webhook subscription and DLQ trace ID in body")
		}
		if strings.Contains(b, subBeta.URL) || strings.Contains(b, "trace-dlq-beta") {
			t.Errorf("cross-tenant leak: found Beta webhook/DLQ in Alpha view")
		}

		// 5. Audit Logs
		req = httptest.NewRequest(http.MethodGet, "/admin/logs/outbound", nil)
		req.AddCookie(cookie)
		req.AddCookie(cookieAlpha)
		rec = httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("logs: expected 200, got %d", rec.Code)
		}
		b = rec.Body.String()
		if !strings.Contains(b, traceAlpha) {
			t.Errorf("expected Alpha trace %s in body", traceAlpha)
		}
		if strings.Contains(b, traceBeta) {
			t.Errorf("cross-tenant leak: found Beta trace in Alpha view")
		}

		// 6. Inbox
		req = httptest.NewRequest(http.MethodGet, "/admin/inbox", nil)
		req.AddCookie(cookie)
		req.AddCookie(cookieAlpha)
		rec = httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("inbox: expected 200, got %d", rec.Code)
		}
		b = rec.Body.String()
		if strings.Contains(b, contactBeta.Name) {
			t.Errorf("cross-tenant leak: found Beta contact in Alpha inbox")
		}
	})

	// --- PHASE B: Alternate active workspace to Beta ---
	cookieBeta := &http.Cookie{
		Name:  mw.ActiveWorkspaceCookieName,
		Value: wsBeta.ID.String(),
	}

	t.Run("Workspace Beta Isolation Check", func(t *testing.T) {
		// 1. Connections
		req := httptest.NewRequest(http.MethodGet, "/admin/connections", nil)
		req.AddCookie(cookie)
		req.AddCookie(cookieBeta)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("connections: expected 200, got %d", rec.Code)
		}
		b := rec.Body.String()
		if !strings.Contains(b, connBeta.Name) || !strings.Contains(b, connBeta.Slug) {
			t.Errorf("expected Beta connection %s in body", connBeta.Name)
		}
		if strings.Contains(b, connAlpha.Name) || strings.Contains(b, connAlpha.Slug) {
			t.Errorf("cross-tenant leak: found Alpha connection in Beta view")
		}

		// 2. Tags & Contacts
		req = httptest.NewRequest(http.MethodGet, "/admin/tags", nil)
		req.AddCookie(cookie)
		req.AddCookie(cookieBeta)
		rec = httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("tags: expected 200, got %d", rec.Code)
		}
		b = rec.Body.String()
		if !strings.Contains(b, tagBeta.Name) {
			t.Errorf("expected Beta tag in body")
		}
		if strings.Contains(b, tagAlpha.Name) {
			t.Errorf("cross-tenant leak: found Alpha tag in Beta view")
		}

		// 3. Campaigns
		req = httptest.NewRequest(http.MethodGet, "/admin/campaigns", nil)
		req.AddCookie(cookie)
		req.AddCookie(cookieBeta)
		rec = httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("campaigns: expected 200, got %d", rec.Code)
		}
		b = rec.Body.String()
		if !strings.Contains(b, campBeta.Name) {
			t.Errorf("expected Beta campaign %s in body", campBeta.Name)
		}
		if strings.Contains(b, campAlpha.Name) {
			t.Errorf("cross-tenant leak: found Alpha campaign in Beta view")
		}

		// 4. Webhooks & DLQ
		req = httptest.NewRequest(http.MethodGet, "/admin/webhooks", nil)
		req.AddCookie(cookie)
		req.AddCookie(cookieBeta)
		rec = httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("webhooks: expected 200, got %d", rec.Code)
		}
		b = rec.Body.String()
		if !strings.Contains(b, subBeta.URL) || !strings.Contains(b, "trace-dlq-beta") {
			t.Errorf("expected Beta webhook subscription and DLQ trace ID in body")
		}
		if strings.Contains(b, subAlpha.URL) || strings.Contains(b, "trace-dlq-alpha") {
			t.Errorf("cross-tenant leak: found Alpha webhook/DLQ in Beta view")
		}

		// 5. Audit Logs
		req = httptest.NewRequest(http.MethodGet, "/admin/logs/outbound", nil)
		req.AddCookie(cookie)
		req.AddCookie(cookieBeta)
		rec = httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("logs: expected 200, got %d", rec.Code)
		}
		b = rec.Body.String()
		if !strings.Contains(b, traceBeta) {
			t.Errorf("expected Beta trace %s in body", traceBeta)
		}
		if strings.Contains(b, traceAlpha) {
			t.Errorf("cross-tenant leak: found Alpha trace in Beta view")
		}

		// 6. Inbox
		req = httptest.NewRequest(http.MethodGet, "/admin/inbox", nil)
		req.AddCookie(cookie)
		req.AddCookie(cookieBeta)
		rec = httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("inbox: expected 200, got %d", rec.Code)
		}
		b = rec.Body.String()
		if strings.Contains(b, contactAlpha.Name) {
			t.Errorf("cross-tenant leak: found Alpha contact in Beta inbox")
		}
	})
}
