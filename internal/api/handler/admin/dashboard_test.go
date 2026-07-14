package admin_test

import (
	"context"
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
	ws, err := wsRepo.Create(ctx, "Dashboard Test Workspace")
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

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", rec.Code)
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
	ws, err := wsRepo.Create(ctx, "Webhook Simulation Test")
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
