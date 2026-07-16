package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/nats-io/nats.go"

	"github.com/pablojhp.pergo/internal/api/handler/admin"
	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/platform/queue"
	"github.com/pablojhp.pergo/internal/repository"
)

func connectNATS(t *testing.T) *nats.Conn {
	t.Helper()
	url := os.Getenv("PERGO_NATS_URL")
	if url == "" {
		url = "nats://localhost:4222"
	}
	nc, err := nats.Connect(url)
	if err != nil {
		t.Skipf("NATS not available at %s: %v", url, err)
	}
	return nc
}

func setupWebhookRoutes(t *testing.T) (*echo.Echo, *repository.WebhookDLQRepository, *repository.WebhookSubscriptionRepository, *repository.WorkspaceRepository, *queue.JetStreamPublisher) {
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

	// Webhook / DLQ / Subscription repos
	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(i)
	}
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	dlqRepo := repository.NewWebhookDLQRepository(pool, enc)
	subRepo := repository.NewWebhookSubscriptionRepository(pool, enc)

	// Publisher
	publisher := queue.NewJetStreamPublisher(connectNATS(t))

	// Webhook / DLQ Handler
	whHandler := admin.NewWebhookDLQHandler(dlqRepo, subRepo, wsRepo, publisher)

	adminGroup.GET("/webhooks", whHandler.GlobalPage)
	adminGroup.GET("/webhooks/dlq/badge", whHandler.GetBadgeCount)
	adminGroup.GET("/webhooks/dlq/:dlq_id/details", whHandler.GetDetails)
	adminGroup.POST("/webhooks/dlq/:dlq_id/retry", whHandler.RetryDLQ)
	adminGroup.DELETE("/webhooks/dlq/:dlq_id", whHandler.DeleteDLQ)

	adminGroup.GET("/workspaces/:workspace_id/webhooks", whHandler.Page)
	adminGroup.GET("/workspaces/:workspace_id/webhooks/subscriptions/new", whHandler.GetSubscriptionNewForm)
	adminGroup.GET("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id/edit", whHandler.GetSubscriptionEditForm)
	adminGroup.POST("/workspaces/:workspace_id/webhooks/subscriptions", whHandler.CreateSubscription)
	adminGroup.POST("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id", whHandler.UpdateSubscription)
	adminGroup.DELETE("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id", whHandler.DeleteSubscription)
	adminGroup.GET("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id/test-form", whHandler.GetSubscriptionTestForm)
	adminGroup.POST("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id/test", whHandler.TestSubscription)

	return e, dlqRepo, subRepo, wsRepo, publisher
}

func TestAdminWebhookDLQHandlers(t *testing.T) {
	e, dlqRepo, subRepo, wsRepo, _ := setupWebhookRoutes(t)

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
	if !strings.Contains(rec.Body.String(), "Active Subscriptions") {
		t.Error("expected config section in body")
	}

	// 2. POST /admin/workspaces/:workspace_id/webhooks/subscriptions
	formData := url.Values{}
	formData.Set("url", "https://example.com/webhook-endpoint")
	formData.Set("secret", "supersecret123")
	formData.Add("event_types", "*")
	req = httptest.NewRequest(http.MethodPost, "/admin/workspaces/"+ws.ID.String()+"/webhooks/subscriptions", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", rec.Code)
	}
	if rec.Header().Get("HX-Refresh") != "true" {
		t.Error("expected HX-Refresh header to refresh page")
	}

	// Verify subscription is saved in DB
	subs, err := subRepo.ListByWorkspace(ctx, ws.ID)
	if err != nil {
		t.Fatalf("failed to list subscriptions: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(subs))
	}
	sub := subs[0]
	if sub.URL != "https://example.com/webhook-endpoint" || string(sub.Secret) != "supersecret123" {
		t.Errorf("subscription fields mismatch: %+v", sub)
	}

	// 3. POST /admin/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id (Edit URL and event types)
	editFormData := url.Values{}
	editFormData.Set("url", "https://example.com/updated-endpoint")
	editFormData.Set("secret", "********") // Placeholder preserves secret
	editFormData.Set("active", "true")
	editFormData.Add("event_types", "message.sent")
	editFormData.Add("event_types", "message.received")
	req = httptest.NewRequest(
		http.MethodPost,
		"/admin/workspaces/"+ws.ID.String()+"/webhooks/subscriptions/"+sub.ID.String(),
		strings.NewReader(editFormData.Encode()),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", rec.Code)
	}

	// Verify updated fields in DB
	updatedSub, err := subRepo.Get(ctx, sub.ID)
	if err != nil {
		t.Fatalf("failed to retrieve updated sub: %v", err)
	}
	if updatedSub.URL != "https://example.com/updated-endpoint" {
		t.Errorf("expected URL to update, got %q", updatedSub.URL)
	}
	if string(updatedSub.Secret) != "supersecret123" {
		t.Errorf("expected secret to be preserved, got %q", string(updatedSub.Secret))
	}
	if len(updatedSub.EventTypes) != 2 || updatedSub.EventTypes[0] != "message.sent" || updatedSub.EventTypes[1] != "message.received" {
		t.Errorf("unexpected event types array: %v", updatedSub.EventTypes)
	}

	// 4. GET /admin/webhooks
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

	// 5. GET /admin/webhooks/dlq/badge
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

	// 6. Insert dummy DLQ item and test GET Details
	err = dlqRepo.InsertDLQ(
		ctx,
		ws.ID,
		sub.ID,
		"trace-abc",
		"msg-def",
		"failed",
		[]byte(`{"status":"failed"}`),
		"https://example.com/updated-endpoint",
		1,
		nil,
	)
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

	// 7. DELETE /admin/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id
	req = httptest.NewRequest(http.MethodDelete, "/admin/workspaces/"+ws.ID.String()+"/webhooks/subscriptions/"+sub.ID.String(), nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", rec.Code)
	}

	// Verify deleted from DB
	_, err = subRepo.Get(ctx, sub.ID)
	if !errors.Is(err, repository.ErrWebhookSubscriptionNotFound) {
		t.Errorf("expected subscription not found error, got %v", err)
	}
}
