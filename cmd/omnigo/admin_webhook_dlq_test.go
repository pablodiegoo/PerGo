package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/nats-io/nats.go"

	"github.com/pablojhp.omnigo/internal/api/handler/admin"
	mw "github.com/pablojhp.omnigo/internal/api/middleware"
	"github.com/pablojhp.omnigo/internal/platform/crypto"
	"github.com/pablojhp.omnigo/internal/platform/postgres"
	"github.com/pablojhp.omnigo/internal/platform/queue"
	"github.com/pablojhp.omnigo/internal/repository"
)

func connectNATS(t *testing.T) *nats.Conn {
	t.Helper()
	url := os.Getenv("OMNIGO_NATS_URL")
	if url == "" {
		url = "nats://localhost:4222"
	}
	nc, err := nats.Connect(url)
	if err != nil {
		t.Skipf("NATS not available at %s: %v", url, err)
	}
	return nc
}

func setupWebhookRoutes(t *testing.T) (*echo.Echo, *repository.WebhookDLQRepository, *repository.WorkspaceRepository, *queue.JetStreamPublisher) {
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
	_ = postgres.RunMigrations(db)
	db.Close()

	e := echo.New()
	e.Use(mw.HTMXMiddleware())

	// Public login
	adminPublic := e.Group("/admin")
	adminPublic.POST("/login", func(c *echo.Context) error {
		return admin.LoginPost(c, nil, "testpass123")
	})

	// Protected admin routes
	adminGroup := e.Group("/admin")
	adminGroup.Use(mw.SessionAuthMiddleware())

	// Workspace repository
	wsRepo := repository.NewWorkspaceRepository(pool)

	// Webhook / DLQ repo
	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(i)
	}
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	dlqRepo := repository.NewWebhookDLQRepository(pool, enc)

	// Publisher
	publisher := queue.NewJetStreamPublisher(connectNATS(t))

	// Webhook / DLQ Handler
	whHandler := admin.NewWebhookDLQHandler(dlqRepo, wsRepo, publisher)

	adminGroup.GET("/webhooks", whHandler.GlobalPage)
	adminGroup.GET("/webhooks/dlq/badge", whHandler.GetBadgeCount)
	adminGroup.GET("/webhooks/dlq/:dlq_id/details", whHandler.GetDetails)
	adminGroup.POST("/webhooks/dlq/:dlq_id/retry", whHandler.RetryDLQ)
	adminGroup.DELETE("/webhooks/dlq/:dlq_id", whHandler.DeleteDLQ)

	adminGroup.GET("/workspaces/:workspace_id/webhooks", whHandler.Page)
	adminGroup.POST("/workspaces/:workspace_id/webhooks/config", whHandler.SaveConfig)
	adminGroup.DELETE("/workspaces/:workspace_id/webhooks/config", whHandler.DeleteConfig)

	return e, dlqRepo, wsRepo, publisher
}

func TestAdminWebhookDLQHandlers(t *testing.T) {
	e, dlqRepo, wsRepo, _ := setupWebhookRoutes(t)

	ctx := context.Background()
	ws, err := wsRepo.Create(ctx, "wh_admin_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	cookie := loginAndGetCookie(t, e)

	// 1. GET /admin/workspaces/:workspace_id/webhooks
	req := httptest.NewRequest(http.MethodGet, "/admin/workspaces/"+ws.ID.String()+"/webhooks", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Webhook Configuration") {
		t.Error("expected config section in body")
	}

	// 2. POST /admin/workspaces/:workspace_id/webhooks/config
	formData := url.Values{}
	formData.Set("url", "https://example.com/webhook-endpoint")
	formData.Set("secret", "supersecret123")
	req = httptest.NewRequest(http.MethodPost, "/admin/workspaces/"+ws.ID.String()+"/webhooks/config", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", rec.Code)
	}
	if rec.Header().Get("HX-Refresh") != "true" {
		t.Error("expected HX-Refresh header to refresh config form page")
	}

	// Verify config is saved in DB
	cfg, err := dlqRepo.GetConfig(ctx, ws.ID)
	if err != nil {
		t.Fatalf("failed to fetch config: %v", err)
	}
	if cfg.URL != "https://example.com/webhook-endpoint" || string(cfg.Secret) != "supersecret123" {
		t.Errorf("config fields mismatch: %+v", cfg)
	}

	// 3. GET /admin/webhooks
	req = httptest.NewRequest(http.MethodGet, "/admin/webhooks", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Workspaces Webhooks Config") {
		t.Error("expected workspaces header in body")
	}

	// 4. GET /admin/webhooks/dlq/badge
	req = httptest.NewRequest(http.MethodGet, "/admin/webhooks/dlq/badge", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "sidebar-dlq-badge") {
		t.Error("expected sidebar-dlq-badge element in body")
	}

	// 5. Insert dummy DLQ item and test GET Details
	err = dlqRepo.InsertDLQ(ctx, ws.ID, "trace-abc", "msg-def", "failed", []byte(`{"status":"failed"}`), "https://example.com/webhook-endpoint", 1, nil)
	if err != nil {
		t.Fatalf("failed to insert DLQ item: %v", err)
	}

	dlqItems, err := dlqRepo.ListDLQ(ctx, ws.ID, 10, 0)
	if err != nil || len(dlqItems) == 0 {
		t.Fatalf("failed to list DLQ items: %v", err)
	}
	dlqID := dlqItems[0].ID

	req = httptest.NewRequest(http.MethodGet, "/admin/webhooks/dlq/"+dlqID.String()+"/details", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Dead-Letter Log Details") {
		t.Error("expected modal header in details response")
	}

	// 6. DELETE /admin/webhooks/dlq/:dlq_id
	req = httptest.NewRequest(http.MethodDelete, "/admin/webhooks/dlq/"+dlqID.String(), nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", rec.Code)
	}

	// Verify deleted from DB
	_, err = dlqRepo.GetDLQByID(ctx, dlqID)
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not found error, got %v", err)
	}
}
