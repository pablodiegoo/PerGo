package admin_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/handler/admin"
	"github.com/pablojhp.pergo/internal/platform/audit"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestDashboardHandler_Index_Onboarding(t *testing.T) {
	dsn := os.Getenv("PERGO_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skip("PostgreSQL not available for testing")
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Skip("PostgreSQL ping failed")
	}

	_, err = postgres.NewSQLDB(pool)
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}

	// Initialize repositories
	wsRepo := repository.NewWorkspaceRepository(pool)
	auditQuerier := audit.NewQuerier(pool)
	apiKeyRepo := repository.NewAPIKeyRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, nil) // nil encryptor for test is fine if not saving keys

	// Create workspace
	ws, err := wsRepo.Create(ctx, fmt.Sprintf("Dashboard Test Workspace %s", uuid.New().String()))
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM workspaces WHERE id = $1", ws.ID)
	}()

	h := &admin.DashboardHandler{
		Pool:        pool,
		Workspaces:  wsRepo,
		Audit:       auditQuerier,
		APIKeys:     apiKeyRepo,
		Connections: connRepo,
		Publisher:   nil,
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	// Inject active workspace cookie
	req.AddCookie(&http.Cookie{Name: "pergo-active-workspace", Value: ws.ID.String()})
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err = h.Index(c)
	if err != nil {
		t.Errorf("Index returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	// Verify onboarding checklist is rendered because connections and API keys are 0
	body := rec.Body.String()
	if !strings.Contains(body, "Get Started with PerGo") {
		t.Errorf("expected body to contain onboarding checklist, got: %s", body)
	}
	if !strings.Contains(body, "Link Messaging Connection") {
		t.Errorf("expected body to contain link messaging connection step")
	}
}

func TestDashboardHandler_Index_Onboarded(t *testing.T) {
	dsn := os.Getenv("PERGO_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skip("PostgreSQL not available for testing")
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Skip("PostgreSQL ping failed")
	}

	// Initialize repositories
	wsRepo := repository.NewWorkspaceRepository(pool)
	auditQuerier := audit.NewQuerier(pool)
	apiKeyRepo := repository.NewAPIKeyRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, nil)

	// Clean tables to avoid collisions
	_, _ = pool.Exec(ctx, "DELETE FROM api_keys")
	_, _ = pool.Exec(ctx, "DELETE FROM connections")
	_, _ = pool.Exec(ctx, "DELETE FROM workspaces")

	// Create workspace
	ws, err := wsRepo.Create(ctx, "Dashboard Test Workspace")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM workspaces WHERE id = $1", ws.ID)
	}()

	// Create active API Key
	_, _, err = apiKeyRepo.Create(ctx, ws.ID, "Test API Key")
	if err != nil {
		t.Fatalf("failed to create api key: %v", err)
	}

	// Create active connection
	conn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "Test Active Connection",
		Channel:        "telegram",
		SenderIdentity: "active_sender",
		Status:         "active",
	}
	err = connRepo.Create(ctx, conn)
	if err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}
	defer func() {
		_ = connRepo.Delete(ctx, conn.ID)
	}()

	h := &admin.DashboardHandler{
		Pool:        pool,
		Workspaces:  wsRepo,
		Audit:       auditQuerier,
		APIKeys:     apiKeyRepo,
		Connections: connRepo,
		Publisher:   nil,
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	req.AddCookie(&http.Cookie{Name: "pergo-active-workspace", Value: ws.ID.String()})
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err = h.Index(c)
	if err != nil {
		t.Errorf("Index returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	// Should not contain onboarding checklist
	if strings.Contains(body, "Get Started with PerGo") {
		t.Errorf("expected body NOT to contain onboarding checklist, got: %s", body)
	}
	// Should contain telemetry/operational dashboard elements
	if !strings.Contains(body, "System Status") {
		t.Errorf("expected body to contain operational dashboard 'System Status', got: %s", body)
	}
	if !strings.Contains(body, "Active Channel Connections") {
		t.Errorf("expected body to contain 'Active Channel Connections', got: %s", body)
	}
}

func TestResolveWorkspaceSwitchTarget(t *testing.T) {
	tests := []struct {
		url        string
		wantDetail bool
		wantTarget string
	}{
		// List pages (should NOT be detail, should stay/refresh)
		{url: "/admin", wantDetail: false, wantTarget: "/admin"},
		{url: "/admin/", wantDetail: false, wantTarget: "/admin/"},
		{url: "/admin/inbox", wantDetail: false, wantTarget: "/admin/inbox"},
		{url: "/admin/connections", wantDetail: false, wantTarget: "/admin/connections"},
		{url: "/admin/devices", wantDetail: false, wantTarget: "/admin/devices"},
		{url: "/admin/campaigns", wantDetail: false, wantTarget: "/admin/campaigns"},
		{url: "/admin/campaigns/new", wantDetail: false, wantTarget: "/admin/campaigns/new"},
		{url: "/admin/tags", wantDetail: false, wantTarget: "/admin/tags"},
		{url: "/admin/templates", wantDetail: false, wantTarget: "/admin/templates"},
		{url: "/admin/templates/new", wantDetail: false, wantTarget: "/admin/templates/new"},
		{url: "/admin/webhooks", wantDetail: false, wantTarget: "/admin/webhooks"},
		{url: "/admin/webhooks/subscriptions/new", wantDetail: false, wantTarget: "/admin/webhooks/subscriptions/new"},
		{url: "/admin/telemetry", wantDetail: false, wantTarget: "/admin/telemetry"},
		{url: "/admin/integrations/headless", wantDetail: false, wantTarget: "/admin/integrations/headless"},
		{url: "/admin/integrations/chatwoot", wantDetail: false, wantTarget: "/admin/integrations/chatwoot"},
		{url: "/admin/integrations/typebot", wantDetail: false, wantTarget: "/admin/integrations/typebot"},
		{url: "/admin/workspaces", wantDetail: false, wantTarget: "/admin/workspaces"},
		{url: "/admin/workspace", wantDetail: false, wantTarget: "/admin/workspace"},
		{url: "/admin/workspaces/new", wantDetail: false, wantTarget: "/admin/workspaces/new"},
		{url: "/admin/logs", wantDetail: false, wantTarget: "/admin/logs"},
		{url: "/admin/logs/outbound", wantDetail: false, wantTarget: "/admin/logs/outbound"},
		{url: "/admin/logs/inbound", wantDetail: false, wantTarget: "/admin/logs/inbound"},
		{url: "/admin/logs/actions", wantDetail: false, wantTarget: "/admin/logs/actions"},

		// Detail pages (should be detail, should redirect to parent section root)
		{url: "/admin/workspaces/a1b2c3d4-e5f6-7890-abcd-ef1234567890", wantDetail: true, wantTarget: "/admin/workspaces"},
		{url: "/admin/workspaces/a1b2c3d4-e5f6-7890-abcd-ef1234567890/keys", wantDetail: true, wantTarget: "/admin/workspaces"},
		{url: "/admin/workspaces/a1b2c3d4-e5f6-7890-abcd-ef1234567890/confirm-delete", wantDetail: true, wantTarget: "/admin/workspaces"},
		{url: "/admin/workspace/a1b2c3d4-e5f6-7890-abcd-ef1234567890", wantDetail: true, wantTarget: "/admin/workspaces"},
		{url: "/admin/campaigns/c123/row", wantDetail: true, wantTarget: "/admin/campaigns"},
		{url: "/admin/campaigns/c123/skipped/download", wantDetail: true, wantTarget: "/admin/campaigns"},
		{url: "/admin/webhooks/subscriptions/s123/edit", wantDetail: true, wantTarget: "/admin/webhooks"},
		{url: "/admin/webhooks/subscriptions/s123/rotate-form", wantDetail: true, wantTarget: "/admin/webhooks"},
		{url: "/admin/webhooks/subscriptions/s123/test-form", wantDetail: true, wantTarget: "/admin/webhooks"},
		{url: "/admin/webhooks/dlq/d123/details", wantDetail: true, wantTarget: "/admin/webhooks"},
		{url: "/admin/logs/actions/l123/metadata", wantDetail: true, wantTarget: "/admin/logs/actions"},
		{url: "http://localhost:8080/admin/campaigns/c123/row?tab=1", wantDetail: true, wantTarget: "/admin/campaigns"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			isDetail, target := admin.ResolveWorkspaceSwitchTarget(tt.url)
			if isDetail != tt.wantDetail {
				t.Errorf("url %s: got isDetail=%v, want %v", tt.url, isDetail, tt.wantDetail)
			}
			if target != tt.wantTarget {
				t.Errorf("url %s: got target=%q, want %q", tt.url, target, tt.wantTarget)
			}
		})
	}
}

func TestDashboardHandler_SelectWorkspace(t *testing.T) {
	h := &admin.DashboardHandler{}
	e := echo.New()

	wsID := uuid.New().String()
	fValues := make(url.Values)
	fValues.Set("workspace_id", wsID)

	req := httptest.NewRequest(http.MethodPost, "/admin/workspaces/active", strings.NewReader(fValues.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.SelectWorkspace(c)
	if err != nil {
		t.Errorf("SelectWorkspace returned error: %v", err)
	}

	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Errorf("expected status 200 or 204, got %d", rec.Code)
	}

	// Cookie should be set with workspace ID
	cookies := rec.Result().Cookies()
	var wsCookie *http.Cookie
	for _, ck := range cookies {
		if ck.Name == "pergo-active-workspace" {
			wsCookie = ck
			break
		}
	}
	if wsCookie == nil {
		t.Fatal("expected pergo-active-workspace cookie to be set")
	}
	if wsCookie.Value != wsID {
		t.Errorf("expected cookie value %s, got %s", wsID, wsCookie.Value)
	}
}

func TestDashboardHandler_SelectWorkspace_SmartNavigation(t *testing.T) {
	h := &admin.DashboardHandler{}
	e := echo.New()
	wsID := uuid.New().String()

	t.Run("HTMX list view reloads in place with HX-Refresh", func(t *testing.T) {
		fValues := make(url.Values)
		fValues.Set("workspace_id", wsID)

		req := httptest.NewRequest(http.MethodPost, "/admin/workspaces/active", strings.NewReader(fValues.Encode()))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
		req.Header.Set("HX-Request", "true")
		req.Header.Set("HX-Current-URL", "http://localhost:8080/admin/campaigns")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := h.SelectWorkspace(c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if rec.Header().Get("HX-Refresh") != "true" {
			t.Errorf("expected HX-Refresh header 'true', got %q", rec.Header().Get("HX-Refresh"))
		}
		if rec.Header().Get("HX-Redirect") != "" {
			t.Errorf("expected no HX-Redirect on list view, got %q", rec.Header().Get("HX-Redirect"))
		}
	})

	t.Run("HTMX campaign detail redirects to section root with HX-Redirect", func(t *testing.T) {
		fValues := make(url.Values)
		fValues.Set("workspace_id", wsID)

		req := httptest.NewRequest(http.MethodPost, "/admin/workspaces/active", strings.NewReader(fValues.Encode()))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
		req.Header.Set("HX-Request", "true")
		req.Header.Set("HX-Current-URL", "http://localhost:8080/admin/campaigns/c-12345/row")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := h.SelectWorkspace(c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if rec.Header().Get("HX-Redirect") != "/admin/campaigns" {
			t.Errorf("expected HX-Redirect '/admin/campaigns', got %q", rec.Header().Get("HX-Redirect"))
		}
	})

	t.Run("HTMX webhook subscription edit redirects to section root with HX-Redirect", func(t *testing.T) {
		fValues := make(url.Values)
		fValues.Set("workspace_id", wsID)

		req := httptest.NewRequest(http.MethodPost, "/admin/workspaces/active", strings.NewReader(fValues.Encode()))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
		req.Header.Set("HX-Request", "true")
		req.Header.Set("HX-Current-URL", "http://localhost:8080/admin/webhooks/subscriptions/sub-999/edit")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := h.SelectWorkspace(c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if rec.Header().Get("HX-Redirect") != "/admin/webhooks" {
			t.Errorf("expected HX-Redirect '/admin/webhooks', got %q", rec.Header().Get("HX-Redirect"))
		}
	})

	t.Run("HTMX workspace detail redirects to /admin/workspaces", func(t *testing.T) {
		fValues := make(url.Values)
		fValues.Set("workspace_id", wsID)

		req := httptest.NewRequest(http.MethodPost, "/admin/workspaces/active", strings.NewReader(fValues.Encode()))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
		req.Header.Set("HX-Request", "true")
		req.Header.Set("HX-Current-URL", "http://localhost:8080/admin/workspaces/ws-999/keys")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := h.SelectWorkspace(c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if rec.Header().Get("HX-Redirect") != "/admin/workspaces" {
			t.Errorf("expected HX-Redirect '/admin/workspaces', got %q", rec.Header().Get("HX-Redirect"))
		}
	})
}

func TestDashboardHandler_SimulateWebhook(t *testing.T) {
	dsn := os.Getenv("PERGO_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skip("PostgreSQL not available for testing")
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Skip("PostgreSQL ping failed")
	}

	_, err = postgres.NewSQLDB(pool)
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}

	wsRepo := repository.NewWorkspaceRepository(pool)
	ws, err := wsRepo.Create(ctx, fmt.Sprintf("Webhook Simulation Test %s", uuid.New().String()))
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM workspaces WHERE id = $1", ws.ID)
	}()

	h := &admin.DashboardHandler{
		Pool: pool,
	}

	e := echo.New()
	fValues := make(url.Values)
	fValues.Set("workspace_id", ws.ID.String())
	fValues.Set("event_type", "message.failed")

	req := httptest.NewRequest(http.MethodPost, "/admin/webhook/simulate", strings.NewReader(fValues.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err = h.SimulateWebhook(c)
	if err != nil {
		t.Errorf("SimulateWebhook returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	if !strings.Contains(rec.Body.String(), "Simulation Sent!") {
		t.Errorf("expected response to indicate success, got: %s", rec.Body.String())
	}

	// Verify audit log entry was written
	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_logs WHERE workspace_id = $1 AND event_type = $2", ws.ID, "message.failed").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query audit logs count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 audit log entry written, got %d", count)
	}

	// Cleanup audit log
	_, _ = pool.Exec(ctx, "DELETE FROM audit_logs WHERE workspace_id = $1", ws.ID)
}
