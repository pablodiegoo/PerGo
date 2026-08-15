package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
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
	"github.com/pablojhp.pergo/internal/webhook"
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
	adminGroup.GET("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id/rotate-form", whHandler.GetRotateSecretForm)
	adminGroup.POST("/workspaces/:workspace_id/webhooks/subscriptions", whHandler.CreateSubscription)
	adminGroup.POST("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id", whHandler.UpdateSubscription)
	adminGroup.POST("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id/rotate", whHandler.RotateSubscriptionSecret)
	adminGroup.POST("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id/ping", whHandler.PingSubscription)
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

	// 2. POST /admin/workspaces/:workspace_id/webhooks/subscriptions (with explicit secret -> reveals modal)
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
	if !strings.Contains(rec.Body.String(), "Webhook Endpoint Created Successfully") {
		t.Error("expected secret reveal modal title in response")
	}
	if !strings.Contains(rec.Body.String(), "supersecret123") {
		t.Error("expected secret plaintext to be revealed in one-time banner/modal")
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

func TestAdminWebhookSecretAutoGenerationAndSSRFFilter(t *testing.T) {
	e, _, subRepo, wsRepo, _ := setupWebhookRoutes(t)

	ctx := context.Background()
	ws, err := wsRepo.Create(ctx, "wh_auto_sec_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	cookie := loginAndGetCookie(t, e)

	// 1. Test SSRF blocked endpoint
	ssrfForm := url.Values{}
	ssrfForm.Set("url", "http://127.0.0.1:8080/internal/webhook")
	req := httptest.NewRequest(http.MethodPost, "/admin/workspaces/"+ws.ID.String()+"/webhooks/subscriptions", strings.NewReader(ssrfForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 Unprocessable Entity for SSRF blocked URL, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "SSRF netpolicy") {
		t.Errorf("expected SSRF error message, got: %s", rec.Body.String())
	}

	// 2. Test auto-generation of secret when left blank
	autoForm := url.Values{}
	autoForm.Set("url", "https://api.external.com/webhooks")
	autoForm.Set("secret", "") // left blank
	autoForm.Add("event_types", "connection.status")
	autoForm.Add("event_types", "message.delivered")

	req = httptest.NewRequest(http.MethodPost, "/admin/workspaces/"+ws.ID.String()+"/webhooks/subscriptions", strings.NewReader(autoForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK on create with auto secret, got %d", rec.Code)
	}
	bodyStr := rec.Body.String()
	if !strings.Contains(bodyStr, "Webhook Endpoint Created Successfully") {
		t.Error("expected reveal modal in response")
	}

	// Verify secret in DB has length 64 (32 hex bytes)
	subs, err := subRepo.ListByWorkspace(ctx, ws.ID)
	if err != nil || len(subs) != 1 {
		t.Fatalf("expected 1 sub in DB, got err=%v, count=%d", err, len(subs))
	}
	if len(subs[0].Secret) != 64 {
		t.Errorf("expected auto-generated 64-char hex secret, got length %d (%s)", len(subs[0].Secret), string(subs[0].Secret))
	}
}

func TestAdminWebhookSecretRotation(t *testing.T) {
	e, _, subRepo, wsRepo, _ := setupWebhookRoutes(t)

	ctx := context.Background()
	ws, err := wsRepo.Create(ctx, "wh_rotate_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	cookie := loginAndGetCookie(t, e)

	// Create initial subscription
	sub, err := subRepo.Create(ctx, ws.ID, "https://api.crm.com/webhooks", []string{"*"}, []byte("initial_secret_123"))
	if err != nil {
		t.Fatalf("failed to create initial sub: %v", err)
	}

	// 1. GET rotate-form
	req := httptest.NewRequest(http.MethodGet, "/admin/workspaces/"+ws.ID.String()+"/webhooks/subscriptions/"+sub.ID.String()+"/rotate-form", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for rotate form, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Rotate Signing Secret") {
		t.Error("expected rotate modal title in body")
	}

	// 2. POST rotate with blank secret -> auto-generates 64-char secret
	rotateForm := url.Values{}
	rotateForm.Set("secret", "")
	req = httptest.NewRequest(
		http.MethodPost,
		"/admin/workspaces/"+ws.ID.String()+"/webhooks/subscriptions/"+sub.ID.String()+"/rotate",
		strings.NewReader(rotateForm.Encode()),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for rotate action, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Webhook Secret Rotated Successfully") {
		t.Error("expected rotated modal title in response")
	}

	// Verify rotated secret in DB
	rotatedSub, err := subRepo.Get(ctx, sub.ID)
	if err != nil {
		t.Fatalf("failed to retrieve rotated sub: %v", err)
	}
	if string(rotatedSub.Secret) == "initial_secret_123" || len(rotatedSub.Secret) != 64 {
		t.Errorf("expected secret to be changed to 64-char random hex, got %s", string(rotatedSub.Secret))
	}
}

func TestAdminWebhookPing(t *testing.T) {
	e, _, subRepo, wsRepo, _ := setupWebhookRoutes(t)

	ctx := context.Background()
	ws, err := wsRepo.Create(ctx, "wh_ping_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	cookie := loginAndGetCookie(t, e)

	var mu sync.Mutex
	var receivedSig string
	var receivedPayload []byte

	// Mock receiver server
	mockReceiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		receivedSig = r.Header.Get("X-PerGo-Signature")
		receivedPayload, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"pong"}`))
	}))
	defer mockReceiver.Close()

	signingSecret := "ping_test_secret_key_456"
	sub, err := subRepo.Create(ctx, ws.ID, mockReceiver.URL, []string{"*"}, []byte(signingSecret))
	if err != nil {
		t.Fatalf("failed to create subscription with mock receiver URL: %v", err)
	}

	// POST /admin/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id/ping
	req := httptest.NewRequest(
		http.MethodPost,
		"/admin/workspaces/"+ws.ID.String()+"/webhooks/subscriptions/"+sub.ID.String()+"/ping",
		nil,
	)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK on ping, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "200 OK") {
		t.Errorf("expected 200 OK ping badge, got: %s", rec.Body.String())
	}

	// Validate received signature and payload
	mu.Lock()
	defer mu.Unlock()

	if receivedSig == "" {
		t.Fatal("expected X-PerGo-Signature header on ping request, got empty")
	}

	valid := webhook.VerifyPerGoSignature(receivedPayload, receivedSig, signingSecret)
	if !valid {
		t.Errorf("signature verification failed for ping payload: sig=%s", receivedSig)
	}

	var pingData map[string]any
	if err := json.Unmarshal(receivedPayload, &pingData); err != nil {
		t.Fatalf("failed to parse ping payload: %v", err)
	}
	if pingData["event"] != "ping" {
		t.Errorf("expected event 'ping', got %v", pingData["event"])
	}
}

func TestAdminWebhookDomainEventsSelection(t *testing.T) {
	e, _, subRepo, wsRepo, _ := setupWebhookRoutes(t)

	ctx := context.Background()
	ws, err := wsRepo.Create(ctx, "wh_events_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	cookie := loginAndGetCookie(t, e)

	allDomainEvents := []string{
		"connection.status",
		"message.received",
		"message.delivered",
		"message.read",
		"message.failed",
		"message.sent",
	}

	formData := url.Values{}
	formData.Set("url", "https://events.receiver.io/inbox")
	formData.Set("secret", "event_secret_123")
	for _, evt := range allDomainEvents {
		formData.Add("event_types", evt)
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/workspaces/"+ws.ID.String()+"/webhooks/subscriptions", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	subs, err := subRepo.ListByWorkspace(ctx, ws.ID)
	if err != nil || len(subs) != 1 {
		t.Fatalf("expected 1 sub in DB, got err=%v, count=%d", err, len(subs))
	}

	sub := subs[0]
	if len(sub.EventTypes) != len(allDomainEvents) {
		t.Errorf("expected %d event types, got %d: %v", len(allDomainEvents), len(sub.EventTypes), sub.EventTypes)
	}
	for _, evt := range allDomainEvents {
		found := false
		for _, subEvt := range sub.EventTypes {
			if subEvt == evt {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected event %q to be present in subscription event types", evt)
		}
	}
}
