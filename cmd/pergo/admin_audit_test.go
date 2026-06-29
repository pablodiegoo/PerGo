package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// seedAuditEvent inserts a single audit event directly into the database.
func seedAuditEvent(t *testing.T, pool *pgxpool.Pool,
	wsID uuid.UUID, traceID, eventType string, payload string, createdAt time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO audit_logs (id, workspace_id, trace_id, event_type, payload, created_at) VALUES ($1, $2, $3, $4, $5::jsonb, $6)`,
		uuid.New(), wsID, traceID, eventType, payload, createdAt,
	)
	if err != nil {
		t.Fatalf("seed audit event: %v", err)
	}
}

// setupAuditTestRoutes creates an Echo instance with session auth and audit admin routes.
func setupAuditTestRoutes(t *testing.T) *echo.Echo {
	t.Helper()
	t.Setenv("PERGO_ADMIN_PASSWORD", "testpass123")

	e := echo.New()
	e.Use(mw.HTMXMiddleware())

	// Public admin routes (login/logout)
	adminPublic := e.Group("/admin")
	adminPublic.POST("/login", func(c *echo.Context) error {
		return admin.LoginPost(c, nil, "testpass123")
	})
	adminPublic.POST("/logout", func(c *echo.Context) error {
		return admin.Logout(c)
	})

	// Protected admin routes (session auth required)
	adminGroup := e.Group("/admin")
	adminGroup.Use(mw.SessionAuthMiddleware())

	pool := getTestPool(t)
	if pool == nil {
		t.Skip("skipping: no PostgreSQL available")
	}

	// Run migrations to ensure schema exists
	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		t.Fatalf("failed to create sql.DB: %v", err)
	}
	_ = postgres.RunMigrations(db)
	db.Close()

	auditRepo := repository.NewAuditRepository(pool)
	wsRepo := repository.NewWorkspaceRepository(pool)
	auditHandler := &admin.AuditHandler{Repo: auditRepo, Workspaces: wsRepo}

	adminGroup.GET("/audit", auditHandler.Redirect)
	adminGroup.GET("/audit/inbound", auditHandler.ListInbound)
	adminGroup.GET("/audit/inbound/export", auditHandler.ExportInboundCSV)
	adminGroup.GET("/audit/outbound", auditHandler.ListOutbound)
	adminGroup.GET("/audit/outbound/export", auditHandler.ExportOutboundCSV)

	return e
}

// getAuditSessionCookie logs in and returns the session cookie.
func getAuditSessionCookie(t *testing.T, e *echo.Echo) *http.Cookie {
	t.Helper()
	form := strings.NewReader("password=testpass123")
	loginReq := httptest.NewRequest(http.MethodPost, "/admin/login", form)
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	e.ServeHTTP(loginRec, loginReq)

	for _, c := range loginRec.Result().Cookies() {
		if strings.Contains(c.Name, "session") {
			return c
		}
	}
	t.Fatal("no session cookie found after login")
	return nil
}

// Test 1: GET /admin/audit/inbound with session returns 200 with audit log table
func TestAdminAuditList(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("skipping: no PostgreSQL available")
	}

	wsID := uuid.New()
	seedAuditEvent(t, pool, wsID, uuid.New().String(), "inbound_message", `{"msg":"hello"}`, time.Now())

	e := setupAuditTestRoutes(t)
	cookie := getAuditSessionCookie(t, e)

	req := httptest.NewRequest(http.MethodGet, "/admin/audit/inbound", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<table") {
		t.Error("expected audit log table in response")
	}
}

// Test 2: GET /admin/audit/inbound?workspace_id={id} filters logs to that workspace
func TestAdminAuditFilterWorkspace(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("skipping: no PostgreSQL available")
	}

	ws1 := uuid.New()
	ws2 := uuid.New()
	trace1 := uuid.New().String()
	trace2 := uuid.New().String()
	now := time.Now()

	seedAuditEvent(t, pool, ws1, trace1, "inbound_message", `{"ws":1}`, now)
	seedAuditEvent(t, pool, ws2, trace2, "inbound_message", `{"ws":2}`, now)

	e := setupAuditTestRoutes(t)
	cookie := getAuditSessionCookie(t, e)

	req := httptest.NewRequest(http.MethodGet, "/admin/audit/inbound?workspace_id="+ws1.String(), nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, trace1) {
		t.Error("expected filtered results to contain ws1 trace_id")
	}
	if strings.Contains(body, trace2) {
		t.Error("expected filtered results to NOT contain ws2 trace_id")
	}
}

// Test 3: GET /admin/audit/inbound?trace_id={id} filters logs to exact trace_id match
func TestAdminAuditFilterTraceID(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("skipping: no PostgreSQL available")
	}

	wsID := uuid.New()
	trace1 := "exact-trace-match-12345"
	trace2 := "different-trace-id-67890"
	now := time.Now()

	seedAuditEvent(t, pool, wsID, trace1, "inbound_message", `{"t":1}`, now)
	seedAuditEvent(t, pool, wsID, trace2, "inbound_message", `{"t":2}`, now)

	e := setupAuditTestRoutes(t)
	cookie := getAuditSessionCookie(t, e)

	req := httptest.NewRequest(http.MethodGet, "/admin/audit/inbound?trace_id="+trace1, nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, trace1) {
		t.Error("expected filtered results to contain exact trace_id")
	}
	if strings.Contains(body, trace2) {
		t.Error("expected filtered results to NOT contain other trace_id")
	}
}

// Test 4: GET /admin/audit/inbound and /admin/audit/outbound filters by event type
func TestAdminAuditFilterEventType(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("skipping: no PostgreSQL available")
	}

	wsID := uuid.New()
	trace1 := uuid.New().String()
	trace2 := uuid.New().String()
	now := time.Now()

	seedAuditEvent(t, pool, wsID, trace1, "inbound_message", `{"type":"inbound"}`, now)
	seedAuditEvent(t, pool, wsID, trace2, "outbound_message", `{"type":"outbound"}`, now)

	e := setupAuditTestRoutes(t)
	cookie := getAuditSessionCookie(t, e)

	// Inbound endpoint should only return trace1
	reqIn := httptest.NewRequest(http.MethodGet, "/admin/audit/inbound", nil)
	reqIn.AddCookie(cookie)
	recIn := httptest.NewRecorder()
	e.ServeHTTP(recIn, reqIn)

	if recIn.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", recIn.Code, recIn.Body.String())
	}
	bodyIn := recIn.Body.String()
	if !strings.Contains(bodyIn, trace1) {
		t.Error("expected inbound logs to contain trace1")
	}
	if strings.Contains(bodyIn, trace2) {
		t.Error("expected inbound logs to NOT contain trace2")
	}

	// Outbound endpoint should only return trace2
	reqOut := httptest.NewRequest(http.MethodGet, "/admin/audit/outbound", nil)
	reqOut.AddCookie(cookie)
	recOut := httptest.NewRecorder()
	e.ServeHTTP(recOut, reqOut)

	if recOut.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", recOut.Code, recOut.Body.String())
	}
	bodyOut := recOut.Body.String()
	if !strings.Contains(bodyOut, trace2) {
		t.Error("expected outbound logs to contain trace2")
	}
	if strings.Contains(bodyOut, trace1) {
		t.Error("expected outbound logs to NOT contain trace1")
	}
}

// Test 5: GET /admin/audit/inbound?page=2 returns second page of results (50 rows per page)
func TestAdminAuditPagination(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("skipping: no PostgreSQL available")
	}

	wsID := uuid.New()
	now := time.Now()

	// Insert 75 events
	for i := 0; i < 75; i++ {
		traceID := fmt.Sprintf("pagination-trace-%d", i)
		seedAuditEvent(t, pool, wsID, traceID, "inbound_message", `{"page":"test"}`, now.Add(-time.Duration(i)*time.Second))
	}

	e := setupAuditTestRoutes(t)
	cookie := getAuditSessionCookie(t, e)

	wsFilter := "?workspace_id=" + wsID.String()

	// Page 1
	req1 := httptest.NewRequest(http.MethodGet, "/admin/audit/inbound"+wsFilter, nil)
	req1.AddCookie(cookie)
	rec1 := httptest.NewRecorder()
	e.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Errorf("page 1: expected 200, got %d", rec1.Code)
	}
	body1 := rec1.Body.String()
	if !strings.Contains(body1, "pagination-trace-0") {
		t.Error("page 1: expected to contain first events")
	}

	// Page 2
	req2 := httptest.NewRequest(http.MethodGet, "/admin/audit/inbound"+wsFilter+"&page=2", nil)
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	e.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Errorf("page 2: expected 200, got %d", rec2.Code)
	}
	body2 := rec2.Body.String()
	// Page 2 should contain events from the second batch
	if !strings.Contains(body2, "pagination-trace-50") {
		t.Error("page 2: expected to contain events from second page")
	}
}

// Test 6: GET /admin/audit/inbound?start=2026-01-01&end=2026-12-31 filters by time range
func TestAdminAuditFilterTimeRange(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("skipping: no PostgreSQL available")
	}

	wsID := uuid.New()
	traceInRange := uuid.New().String()
	traceOutOfRange := uuid.New().String()

	seedAuditEvent(t, pool, wsID, traceInRange, "inbound_message", `{"range":"in"}`, time.Now().Add(-1*time.Second))
	seedAuditEvent(t, pool, wsID, traceOutOfRange, "inbound_message", `{"range":"out"}`, time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC))

	e := setupAuditTestRoutes(t)
	cookie := getAuditSessionCookie(t, e)

	req := httptest.NewRequest(http.MethodGet, "/admin/audit/inbound?start=2026-01-01&end=2026-12-31", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, traceInRange) {
		t.Error("expected filtered results to contain in-range event")
	}
	if strings.Contains(body, traceOutOfRange) {
		t.Error("expected filtered results to NOT contain out-of-range event")
	}
}

// Test 7: GET /admin/audit/inbound/export?workspace_id={id} returns CSV with Content-Type text/csv
func TestAdminAuditExportCSV(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("skipping: no PostgreSQL available")
	}

	wsID := uuid.New()
	traceID := uuid.New().String()
	now := time.Now()

	seedAuditEvent(t, pool, wsID, traceID, "inbound_message", `{"csv":"data"}`, now)

	e := setupAuditTestRoutes(t)
	cookie := getAuditSessionCookie(t, e)

	req := httptest.NewRequest(http.MethodGet, "/admin/audit/inbound/export?workspace_id="+wsID.String(), nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/csv") {
		t.Errorf("expected Content-Type text/csv, got %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "timestamp") {
		t.Error("expected CSV to contain timestamp header")
	}
	if !strings.Contains(body, traceID) {
		t.Error("expected CSV to contain audit event trace_id")
	}
}

// Test 8: GET /admin/audit/inbound with HX-Request header returns HTML fragment (table body only)
func TestAdminAuditHTMXFragment(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("skipping: no PostgreSQL available")
	}

	wsID := uuid.New()
	seedAuditEvent(t, pool, wsID, uuid.New().String(), "inbound_message", `{"htmx":"data"}`, time.Now())

	e := setupAuditTestRoutes(t)
	cookie := getAuditSessionCookie(t, e)

	req := httptest.NewRequest(http.MethodGet, "/admin/audit/inbound", nil)
	req.AddCookie(cookie)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// Fragment should NOT contain full page layout
	if strings.Contains(body, "<!DOCTYPE") || strings.Contains(body, "<html") {
		t.Error("HTMX fragment should not contain full page layout")
	}
	// Fragment should contain table rows
	if !strings.Contains(body, "<tr") {
		t.Error("HTMX fragment should contain table rows")
	}
}

// Test 9: GET /admin/audit/inbound returns pagination controls (page 1 of N, next/prev links)
func TestAdminAuditPaginationControls(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("skipping: no PostgreSQL available")
	}

	wsID := uuid.New()
	now := time.Now()

	// Insert 75 events to ensure multiple pages
	for i := 0; i < 75; i++ {
		traceID := fmt.Sprintf("controls-trace-%d", i)
		seedAuditEvent(t, pool, wsID, traceID, "inbound_message", `{"controls":"test"}`, now.Add(-time.Duration(i)*time.Second))
	}

	e := setupAuditTestRoutes(t)
	cookie := getAuditSessionCookie(t, e)

	wsFilter := "?workspace_id=" + wsID.String()
	req := httptest.NewRequest(http.MethodGet, "/admin/audit/inbound"+wsFilter, nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	// Should have pagination controls indicating page 1 of at least 2
	if !strings.Contains(body, "page=2") && !strings.Contains(body, "Page 1") {
		t.Error("expected pagination controls with page 1 of N")
	}
}
