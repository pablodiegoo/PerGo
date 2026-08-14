package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/api/handler/admin"
	"github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/webhook"
)

type mockWebhookDeliveryHTTPClient struct {
	lastRequest *http.Request
	lastBody    []byte
}

func (m *mockWebhookDeliveryHTTPClient) Do(req *http.Request) (*http.Response, error) {
	m.lastRequest = req
	if req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		m.lastBody = body
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(bytes.NewReader([]byte("{}"))),
	}, nil
}

type mockSubscriptionStore struct {
	sub *repository.WebhookSubscription
}

func (m *mockSubscriptionStore) Get(ctx context.Context, id uuid.UUID) (*repository.WebhookSubscription, error) {
	if m.sub != nil {
		return m.sub, nil
	}
	return nil, repository.ErrWebhookSubscriptionNotFound
}

func setupWebhookSecretAPIRoutes(t *testing.T) (*echo.Echo, *repository.WorkspaceRepository, *repository.APIKeyRepository) {
	t.Helper()
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

	e := echo.New()
	wsRepo := repository.NewWorkspaceRepository(pool)
	apiKeyRepo := repository.NewAPIKeyRepository(pool)

	workspaceHandler := &admin.WorkspaceHandler{
		Repo:    wsRepo,
		APIKeys: apiKeyRepo,
	}

	// Middleware
	e.Use(middleware.TraceMiddleware())
	e.Use(middleware.AuthMiddleware(apiKeyRepo))

	v1Group := e.Group("/api/v1")
	v1Group.POST("/workspaces/webhook-secret", workspaceHandler.GenerateWebhookSecret)
	v1Group.POST("/workspaces/:workspace_id/webhook-secret", workspaceHandler.GenerateWebhookSecret)
	v1Group.GET("/workspaces/webhook-secret", workspaceHandler.GetWebhookSecret)
	v1Group.GET("/workspaces/:workspace_id/webhook-secret", workspaceHandler.GetWebhookSecret)

	return e, wsRepo, apiKeyRepo
}

func TestWebhookSecret_APIRotationAndImmediateCutover(t *testing.T) {
	e, wsRepo, apiKeyRepo := setupWebhookSecretAPIRoutes(t)
	ctx := context.Background()

	// 1. Create test workspace
	ws, err := wsRepo.Create(ctx, "wh_secret_ws_"+uuid.New().String()[:8])
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	// 2. Create API key for the workspace
	_, apiKeyPlain, err := apiKeyRepo.Create(ctx, ws.ID, "test-wh-key")
	if err != nil {
		t.Fatalf("failed to create API key: %v", err)
	}

	// 3. Test initial GET /api/v1/workspaces/webhook-secret (empty secret)
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/webhook-secret", nil)
		req.Header.Set("Authorization", "Bearer "+apiKeyPlain)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse JSON response: %v", err)
		}
		if resp["webhook_secret"] != "" {
			t.Errorf("expected empty initial webhook_secret, got %q", resp["webhook_secret"])
		}
		if resp["workspace_id"] != ws.ID.String() {
			t.Errorf("expected workspace_id %q, got %q", ws.ID.String(), resp["workspace_id"])
		}
	}

	// 4. Test POST /api/v1/workspaces/webhook-secret generates 64-character hex secret
	var generatedSecret string
	{
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/webhook-secret", nil)
		req.Header.Set("Authorization", "Bearer "+apiKeyPlain)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse JSON response: %v", err)
		}
		generatedSecret = resp["webhook_secret"]
		if len(generatedSecret) != 64 {
			t.Errorf("expected 64-character hex secret (32 bytes), got length %d (%q)", len(generatedSecret), generatedSecret)
		}
	}

	// 5. Verify Dispatcher delivers with valid X-PerGo-Signature verifiable by docs/WEBHOOK_SIGNATURES.md
	httpClient := &mockWebhookDeliveryHTTPClient{}
	subStore := &mockSubscriptionStore{
		sub: &repository.WebhookSubscription{
			ID:          uuid.New(),
			WorkspaceID: ws.ID,
			URL:         "https://subscriber.example.com/webhooks",
			Active:      true,
			Secret:      nil, // Fallback to workspace secret
		},
	}
	dispatcher := webhook.NewDefaultDispatcher(subStore, nil, wsRepo, httpClient, nil)

	payload := []byte(`{"event":"message.sent","message_id":"msg-999","content":"Hello world"}`)
	deliveryTask := webhook.WebhookDeliveryTask{
		ID:             uuid.New(),
		SubscriptionID: subStore.sub.ID,
		WorkspaceID:    ws.ID,
		Event:          "message.sent",
		MessageID:      "msg-999",
		Payload:        payload,
		Mode:           "outbound",
	}

	if err := dispatcher.Dispatch(ctx, deliveryTask); err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}

	if httpClient.lastRequest == nil {
		t.Fatalf("expected webhook request to be made")
	}
	sigHeader := httpClient.lastRequest.Header.Get("X-PerGo-Signature")
	if sigHeader == "" {
		t.Fatalf("expected X-PerGo-Signature header to be present")
	}

	// Verify against Go snippet logic from docs/WEBHOOK_SIGNATURES.md
	if !webhook.VerifyPerGoSignature(payload, sigHeader, generatedSecret) {
		t.Errorf("expected signature %q to verify against secret %q", sigHeader, generatedSecret)
	}

	// 6. Test Instant Secret Rotation via POST /api/v1/workspaces/:workspace_id/webhook-secret
	var rotatedSecret string
	{
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/workspaces/%s/webhook-secret", ws.ID), nil)
		req.Header.Set("Authorization", "Bearer "+apiKeyPlain)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse JSON response: %v", err)
		}
		rotatedSecret = resp["webhook_secret"]
		if rotatedSecret == generatedSecret {
			t.Errorf("expected rotated secret to differ from previous secret")
		}
		if len(rotatedSecret) != 64 {
			t.Errorf("expected 64-char hex secret, got length %d", len(rotatedSecret))
		}
	}

	// 7. Verify Immediate Cutover: Next dispatch signs with rotated secret, old secret fails
	newPayload := []byte(`{"event":"message.delivered","message_id":"msg-999"}`)
	deliveryTask.Payload = newPayload
	deliveryTask.Event = "message.delivered"

	if err := dispatcher.Dispatch(ctx, deliveryTask); err != nil {
		t.Fatalf("dispatch after rotation failed: %v", err)
	}

	newSigHeader := httpClient.lastRequest.Header.Get("X-PerGo-Signature")
	if newSigHeader == "" {
		t.Fatalf("expected X-PerGo-Signature header on new dispatch")
	}

	// Verifies with rotatedSecret
	if !webhook.VerifyPerGoSignature(newPayload, newSigHeader, rotatedSecret) {
		t.Errorf("expected signature to verify with rotated secret")
	}
	// Fails with old generatedSecret
	if webhook.VerifyPerGoSignature(newPayload, newSigHeader, generatedSecret) {
		t.Errorf("expected signature to FAIL with old secret after cutover")
	}

	// 8. Test rotating to a custom secret
	customSecret := "custom-chosen-webhook-secret-string-value"
	{
		bodyJSON := fmt.Sprintf(`{"webhook_secret":%q}`, customSecret)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/webhook-secret", strings.NewReader(bodyJSON))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKeyPlain)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse JSON response: %v", err)
		}
		if resp["webhook_secret"] != customSecret {
			t.Errorf("expected custom secret %q, got %q", customSecret, resp["webhook_secret"])
		}
	}

	// 9. Unauthorized request without API key returns 401
	{
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/webhook-secret", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized without API key, got %d", rec.Code)
		}
	}
}
