package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/handler/admin"
	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/platform/audit"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/repository"
)

// setupTestRoutes creates a real Echo instance with admin routes wired up.
func setupTestRoutes(t *testing.T) *echo.Echo {
	t.Helper()
	t.Setenv("PERGO_ADMIN_PASSWORD", "testpass123")

	e := echo.New()
	e.Use(mw.HTMXMiddleware())

	// Public admin routes (no session auth)
	adminPublic := e.Group("/admin")
	adminPublic.GET("/login", func(c *echo.Context) error {
		return admin.LoginPage(c, false)
	})
	adminPublic.POST("/login", func(c *echo.Context) error {
		// Use a nil wsRepo for login tests — password check doesn't need DB
		return admin.LoginPost(c, nil, "testpass123")
	})
	adminPublic.POST("/logout", func(c *echo.Context) error {
		return admin.Logout(c)
	})

	// Protected admin routes (session auth required)
	adminGroup := e.Group("/admin")
	adminGroup.Use(mw.SessionAuthMiddleware())

	// Dashboard handler with optional DB dependencies
	pool := getTestPool(t)
	var dashboardHandler *admin.DashboardHandler
	if pool != nil {
		dashboardHandler = &admin.DashboardHandler{
			Pool:       pool,
			Workspaces: repository.NewWorkspaceRepository(pool),
			Audit:      audit.NewQuerier(pool),
		}
	} else {
		// No DB available — use a handler that returns minimal HTML
		dashboardHandler = &admin.DashboardHandler{
			Workspaces: nil,
			Audit:      nil,
		}
	}
	adminGroup.GET("/", dashboardHandler.Index)

	// Static files
	staticDir := "../../static"
	if _, err := os.Stat(staticDir); err == nil {
		e.Static("/static", staticDir)
	}

	return e
}

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := testDSN()
	pool, err := postgres.NewPool(t.Context(), dsn)
	if err != nil {
		return nil
	}
	if err := pool.Ping(t.Context()); err != nil {
		pool.Close()
		return nil
	}

	// Run migrations to ensure schema is up to date
	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("failed to wrap pool as sql.DB: %v", err)
	}
	defer db.Close()

	if err := postgres.RunMigrations(db); err != nil {
		pool.Close()
		t.Fatalf("failed to run migrations: %v", err)
	}

	return pool
}

// Test 1: GET /admin/ without session cookie returns 302 redirect to /admin/login
func TestAdminRedirectUnauthenticated(t *testing.T) {
	e := setupTestRoutes(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d: %s", rec.Code, rec.Body.String())
	}
	location := rec.Header().Get("Location")
	if !strings.Contains(location, "/admin/login") {
		t.Errorf("expected redirect to /admin/login, got %q", location)
	}
}

// Test 2: GET /admin/login returns 200 with login form containing password input
func TestAdminLoginPage(t *testing.T) {
	e := setupTestRoutes(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "password") {
		t.Error("expected login page to contain password input")
	}
	if !strings.Contains(body, "<form") {
		t.Error("expected login page to contain a form element")
	}
}

// Test 3: POST /admin/login with correct password returns 302 redirect to /admin/ with session cookie set
func TestAdminLoginSuccess(t *testing.T) {
	e := setupTestRoutes(t)

	form := strings.NewReader("password=testpass123")
	req := httptest.NewRequest(http.MethodPost, "/admin/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d: %s", rec.Code, rec.Body.String())
	}
	location := rec.Header().Get("Location")
	if location != "/admin/" {
		t.Errorf("expected redirect to /admin/, got %q", location)
	}

	// Verify session cookie is set
	cookies := rec.Result().Cookies()
	found := false
	for _, c := range cookies {
		if strings.Contains(c.Name, "session") {
			found = true
			if c.Value == "" {
				t.Error("session cookie value is empty")
			}
			if !c.HttpOnly {
				t.Error("session cookie should be HttpOnly")
			}
			break
		}
	}
	if !found {
		t.Error("expected session cookie to be set in response")
	}
}

// Test 4: POST /admin/login with wrong password returns 401 with error message
func TestAdminLoginWrongPassword(t *testing.T) {
	e := setupTestRoutes(t)

	form := strings.NewReader("password=wrongpassword")
	req := httptest.NewRequest(http.MethodPost, "/admin/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "invalid") && !strings.Contains(body, "error") && !strings.Contains(body, "wrong") {
		t.Errorf("expected error message in response body, got %q", body)
	}
}

// Test 5: GET /admin/ with valid session cookie returns 200 with sidebar navigation
func TestAdminDashboardAuthenticated(t *testing.T) {
	e := setupTestRoutes(t)

	// First, log in to get a session cookie
	form := strings.NewReader("password=testpass123")
	loginReq := httptest.NewRequest(http.MethodPost, "/admin/login", form)
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	e.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusFound {
		t.Fatalf("login failed: expected 302, got %d", loginRec.Code)
	}

	// Extract session cookie from login response
	var sessionCookie *http.Cookie
	for _, c := range loginRec.Result().Cookies() {
		if strings.Contains(c.Name, "session") {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("no session cookie found after login")
	}

	// Access dashboard with session cookie
	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	req.AddCookie(sessionCookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "/admin/") || !strings.Contains(body, "/admin/workspaces") || !strings.Contains(body, "/admin/audit") {
		t.Errorf("expected sidebar navigation with /admin/, /admin/workspaces, /admin/audit links, got %q", body)
	}
}

// Test 6: GET /admin/ with valid session returns dashboard content (workspace count, recent audit section)
func TestAdminDashboardContent(t *testing.T) {
	e := setupTestRoutes(t)

	// Log in to get session cookie
	form := strings.NewReader("password=testpass123")
	loginReq := httptest.NewRequest(http.MethodPost, "/admin/login", form)
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	e.ServeHTTP(loginRec, loginReq)

	var sessionCookie *http.Cookie
	for _, c := range loginRec.Result().Cookies() {
		if strings.Contains(c.Name, "session") {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("no session cookie found after login")
	}

	// Access dashboard with session cookie
	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	req.AddCookie(sessionCookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// Dashboard should contain workspace count and audit sections
	if !strings.Contains(body, "workspace") && !strings.Contains(body, "Workspace") {
		t.Error("expected dashboard to contain workspace information")
	}
	if !strings.Contains(body, "audit") && !strings.Contains(body, "Audit") {
		t.Error("expected dashboard to contain audit section")
	}
}

// Test 7: GET /admin/ with HX-Request header returns HTML fragment (no full page layout)
func TestAdminHTMXFragment(t *testing.T) {
	e := setupTestRoutes(t)

	// Log in to get session cookie
	form := strings.NewReader("password=testpass123")
	loginReq := httptest.NewRequest(http.MethodPost, "/admin/login", form)
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	e.ServeHTTP(loginRec, loginReq)

	var sessionCookie *http.Cookie
	for _, c := range loginRec.Result().Cookies() {
		if strings.Contains(c.Name, "session") {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("no session cookie found after login")
	}

	// Access dashboard with HTMX header
	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	req.AddCookie(sessionCookie)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// Fragment should NOT contain full page layout (DOCTYPE, <html>, <head>, <body>)
	if strings.Contains(body, "<!DOCTYPE") || strings.Contains(body, "<html") {
		t.Error("HTMX fragment should not contain full page layout (DOCTYPE, <html>)")
	}
	// Fragment should contain dashboard content
	if !strings.Contains(body, "dashboard") && !strings.Contains(body, "Dashboard") {
		t.Error("HTMX fragment should contain dashboard content")
	}
}
