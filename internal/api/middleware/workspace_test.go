package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
)

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("PERGO_TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("PERGO_DATABASE_URL")
	}
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, dsn)
	if err != nil {
		t.Skipf("skipping: cannot create postgres pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping: cannot ping postgres: %v", err)
	}

	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		t.Fatalf("failed to create sql.DB: %v", err)
	}
	defer db.Close()
	_ = postgres.RunMigrations(db)

	return pool
}

// TestActiveWorkspaceMiddleware_ValidResolution verifies valid cookie resolves active workspace into context.
func TestActiveWorkspaceMiddleware_ValidResolution(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	_, _ = pool.Exec(ctx, "DELETE FROM workspaces")

	repo := repository.NewWorkspaceRepository(pool)
	_, err := repo.Create(ctx, "Workspace 1")
	if err != nil {
		t.Fatalf("failed to create ws1: %v", err)
	}
	ws2, err := repo.Create(ctx, "Workspace 2")
	if err != nil {
		t.Fatalf("failed to create ws2: %v", err)
	}

	e := echo.New()
	e.Use(middleware.ActiveWorkspaceMiddleware(repo))

	var capturedID uuid.UUID
	var capturedWs *repository.Workspace
	e.GET("/admin/test", func(c *echo.Context) error {
		id, ok := tenant.WorkspaceIDFrom(c.Request().Context())
		if !ok {
			return c.String(http.StatusInternalServerError, "workspace_id missing from context")
		}
		reqID, err := tenant.RequireWorkspaceID(c.Request().Context())
		if err != nil {
			return c.String(http.StatusInternalServerError, "RequireWorkspaceID error: "+err.Error())
		}
		if id != reqID {
			return c.String(http.StatusInternalServerError, "id mismatch")
		}
		capturedID = id
		if ws, ok := c.Request().Context().Value("active_workspace").(*repository.Workspace); ok {
			capturedWs = ws
		}
		return c.String(http.StatusOK, "ok")
	})

	// Request with ws2 cookie
	req := httptest.NewRequest(http.MethodGet, "/admin/test", nil)
	req.AddCookie(&http.Cookie{
		Name:  middleware.ActiveWorkspaceCookieName,
		Value: ws2.ID.String(),
	})
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if capturedID != ws2.ID {
		t.Errorf("expected tenant workspace ID %s, got %s", ws2.ID, capturedID)
	}
	if capturedWs == nil || capturedWs.ID != ws2.ID {
		t.Errorf("expected active_workspace in context to be ws2 (%s), got %+v", ws2.ID, capturedWs)
	}
}

// TestActiveWorkspaceMiddleware_MissingCookie_FallbackEarliest verifies missing cookie falls back to earliest workspace and sets cookie.
func TestActiveWorkspaceMiddleware_MissingCookie_FallbackEarliest(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	_, _ = pool.Exec(ctx, "DELETE FROM workspaces")

	repo := repository.NewWorkspaceRepository(pool)
	ws1, err := repo.Create(ctx, "Earliest Workspace")
	if err != nil {
		t.Fatalf("failed to create ws1: %v", err)
	}
	_, err = repo.Create(ctx, "Later Workspace")
	if err != nil {
		t.Fatalf("failed to create ws2: %v", err)
	}

	e := echo.New()
	e.Use(middleware.ActiveWorkspaceMiddleware(repo))

	var capturedID uuid.UUID
	e.GET("/admin/test", func(c *echo.Context) error {
		id, ok := tenant.WorkspaceIDFrom(c.Request().Context())
		if !ok {
			return c.String(http.StatusInternalServerError, "workspace_id missing from context")
		}
		capturedID = id
		return c.String(http.StatusOK, "ok")
	})

	// Request with NO cookie
	req := httptest.NewRequest(http.MethodGet, "/admin/test", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if capturedID != ws1.ID {
		t.Errorf("expected fallback to earliest workspace ID %s, got %s", ws1.ID, capturedID)
	}

	// Verify Set-Cookie header was issued
	cookies := rec.Result().Cookies()
	var foundCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == middleware.ActiveWorkspaceCookieName {
			foundCookie = c
			break
		}
	}
	if foundCookie == nil {
		t.Fatal("expected pergo-active-workspace cookie in response")
	}
	if foundCookie.Value != ws1.ID.String() {
		t.Errorf("expected cookie value %s, got %s", ws1.ID.String(), foundCookie.Value)
	}
}

// TestActiveWorkspaceMiddleware_InvalidCookie_AutoHealing verifies invalid/deleted cookie auto-heals to earliest workspace.
func TestActiveWorkspaceMiddleware_InvalidCookie_AutoHealing(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	_, _ = pool.Exec(ctx, "DELETE FROM workspaces")

	repo := repository.NewWorkspaceRepository(pool)
	ws1, err := repo.Create(ctx, "Earliest Workspace")
	if err != nil {
		t.Fatalf("failed to create ws1: %v", err)
	}

	e := echo.New()
	e.Use(middleware.ActiveWorkspaceMiddleware(repo))

	var capturedID uuid.UUID
	e.GET("/admin/test", func(c *echo.Context) error {
		id, ok := tenant.WorkspaceIDFrom(c.Request().Context())
		if !ok {
			return c.String(http.StatusInternalServerError, "workspace_id missing from context")
		}
		capturedID = id
		return c.String(http.StatusOK, "ok")
	})

	// 1. Garbage cookie value
	req1 := httptest.NewRequest(http.MethodGet, "/admin/test", nil)
	req1.AddCookie(&http.Cookie{
		Name:  middleware.ActiveWorkspaceCookieName,
		Value: "garbage-invalid-uuid",
	})
	rec1 := httptest.NewRecorder()
	e.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with garbage cookie, got %d", rec1.Code)
	}
	if capturedID != ws1.ID {
		t.Errorf("expected auto-healing to ws1 ID %s, got %s", ws1.ID, capturedID)
	}

	// 2. Non-existent UUID (e.g. deleted workspace)
	req2 := httptest.NewRequest(http.MethodGet, "/admin/test", nil)
	deletedID := uuid.New()
	req2.AddCookie(&http.Cookie{
		Name:  middleware.ActiveWorkspaceCookieName,
		Value: deletedID.String(),
	})
	rec2 := httptest.NewRecorder()
	e.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with deleted workspace cookie, got %d", rec2.Code)
	}
	if capturedID != ws1.ID {
		t.Errorf("expected auto-healing to ws1 ID %s, got %s", ws1.ID, capturedID)
	}
}

// TestActiveWorkspaceMiddleware_EmptyDatabase_RedirectToNew verifies empty database redirects to /admin/workspaces/new.
func TestActiveWorkspaceMiddleware_EmptyDatabase_RedirectToNew(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	_, _ = pool.Exec(ctx, "DELETE FROM workspaces")

	repo := repository.NewWorkspaceRepository(pool)

	e := echo.New()
	e.Use(middleware.ActiveWorkspaceMiddleware(repo))

	e.GET("/admin/dashboard", func(c *echo.Context) error {
		return c.String(http.StatusOK, "should not be reached")
	})

	// Standard request -> 302 redirect
	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("expected 302 Found redirect on empty DB, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/workspaces/new" {
		t.Errorf("expected Location /admin/workspaces/new, got %q", loc)
	}

	// HTMX request -> HX-Redirect header
	reqHTMX := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	reqHTMX.Header.Set("HX-Request", "true")
	recHTMX := httptest.NewRecorder()
	e.ServeHTTP(recHTMX, reqHTMX)

	if hxRedirect := recHTMX.Header().Get("HX-Redirect"); hxRedirect != "/admin/workspaces/new" {
		t.Errorf("expected HX-Redirect /admin/workspaces/new, got %q", hxRedirect)
	}
}

// TestActiveWorkspaceMiddleware_EmptyDatabase_AllowCreateRoutes verifies creation routes are accessible on empty DB.
func TestActiveWorkspaceMiddleware_EmptyDatabase_AllowCreateRoutes(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	_, _ = pool.Exec(ctx, "DELETE FROM workspaces")

	repo := repository.NewWorkspaceRepository(pool)

	e := echo.New()
	e.Use(middleware.ActiveWorkspaceMiddleware(repo))

	e.GET("/admin/workspaces/new", func(c *echo.Context) error {
		return c.String(http.StatusOK, "create form page")
	})
	e.POST("/admin/workspaces", func(c *echo.Context) error {
		return c.String(http.StatusCreated, "workspace created")
	})

	// GET /admin/workspaces/new allowed
	reqGet := httptest.NewRequest(http.MethodGet, "/admin/workspaces/new", nil)
	recGet := httptest.NewRecorder()
	e.ServeHTTP(recGet, reqGet)

	if recGet.Code != http.StatusOK {
		t.Errorf("expected GET /admin/workspaces/new to be 200, got %d", recGet.Code)
	}

	// POST /admin/workspaces allowed
	reqPost := httptest.NewRequest(http.MethodPost, "/admin/workspaces", nil)
	recPost := httptest.NewRecorder()
	e.ServeHTTP(recPost, reqPost)

	if recPost.Code != http.StatusCreated {
		t.Errorf("expected POST /admin/workspaces to be 201, got %d", recPost.Code)
	}
}
