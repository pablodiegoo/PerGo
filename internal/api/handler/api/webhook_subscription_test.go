package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/handler/api"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
)

type mockWebhookSubscriptionRepo struct {
	mu            sync.RWMutex
	subscriptions map[uuid.UUID]*repository.WebhookSubscription
}

func newMockWebhookSubscriptionRepo() *mockWebhookSubscriptionRepo {
	return &mockWebhookSubscriptionRepo{
		subscriptions: make(map[uuid.UUID]*repository.WebhookSubscription),
	}
}

func (m *mockWebhookSubscriptionRepo) Create(ctx context.Context, wsID uuid.UUID, url string, eventTypes []string, secretPlaintext []byte) (*repository.WebhookSubscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := uuid.New()
	sub := &repository.WebhookSubscription{
		ID:          id,
		WorkspaceID: wsID,
		URL:         url,
		Secret:      secretPlaintext,
		EventTypes:  eventTypes,
		Active:      true,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	m.subscriptions[id] = sub
	return sub, nil
}

func (m *mockWebhookSubscriptionRepo) Get(ctx context.Context, id uuid.UUID) (*repository.WebhookSubscription, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sub, ok := m.subscriptions[id]
	if !ok {
		return nil, repository.ErrWebhookSubscriptionNotFound
	}
	return sub, nil
}

func (m *mockWebhookSubscriptionRepo) ListByWorkspace(ctx context.Context, wsID uuid.UUID) ([]*repository.WebhookSubscription, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var list []*repository.WebhookSubscription
	for _, s := range m.subscriptions {
		if s.WorkspaceID == wsID {
			list = append(list, s)
		}
	}
	return list, nil
}

func (m *mockWebhookSubscriptionRepo) Update(ctx context.Context, id uuid.UUID, url string, eventTypes []string, active bool, secretPlaintext []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sub, ok := m.subscriptions[id]
	if !ok {
		return repository.ErrWebhookSubscriptionNotFound
	}
	sub.URL = url
	sub.EventTypes = eventTypes
	sub.Active = active
	if len(secretPlaintext) > 0 {
		sub.Secret = secretPlaintext
	}
	sub.UpdatedAt = time.Now().UTC()
	return nil
}

func (m *mockWebhookSubscriptionRepo) Delete(ctx context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.subscriptions[id]; !ok {
		return repository.ErrWebhookSubscriptionNotFound
	}
	delete(m.subscriptions, id)
	return nil
}

func setupWebhookSubscriptionTestRouter(repo *mockWebhookSubscriptionRepo) (*echo.Echo, *api.WebhookSubscriptionAPIHandler) {
	e := echo.New()
	handler := api.NewWebhookSubscriptionAPIHandler(repo)
	handler.RegisterRoutes(e)
	return e, handler
}

func TestWebhookSubscriptionAPI_Create(t *testing.T) {
	repo := newMockWebhookSubscriptionRepo()
	e, _ := setupWebhookSubscriptionTestRouter(repo)
	wsID := uuid.New()

	t.Run("successful creation with default events and generated secret", func(t *testing.T) {
		body := map[string]interface{}{
			"url": "https://api.example.com/webhook",
		}
		jsonBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/subscriptions", bytes.NewReader(jsonBytes))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		ctx := tenant.WithWorkspaceID(req.Context(), wsID)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected status 201 Created, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp api.WebhookSubscriptionResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.Subscription.URL != "https://api.example.com/webhook" {
			t.Errorf("expected URL https://api.example.com/webhook, got %s", resp.Subscription.URL)
		}
		if resp.Subscription.WorkspaceID != wsID {
			t.Errorf("expected WorkspaceID %s, got %s", wsID, resp.Subscription.WorkspaceID)
		}
		if len(resp.Subscription.Events) != 1 || resp.Subscription.Events[0] != "*" {
			t.Errorf("expected default events [*], got %v", resp.Subscription.Events)
		}
		if resp.Subscription.Secret == "" {
			t.Errorf("expected non-empty generated secret")
		}
		if !resp.Subscription.IsActive {
			t.Errorf("expected is_active to be true")
		}
	})

	t.Run("successful creation with custom events and explicit secret", func(t *testing.T) {
		body := map[string]interface{}{
			"url":       "https://api.example.com/events",
			"events":    []string{"message.received", "connection.status"},
			"secret":    "my-custom-secret-key-123",
			"is_active": false,
		}
		jsonBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/subscriptions", bytes.NewReader(jsonBytes))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		ctx := tenant.WithWorkspaceID(req.Context(), wsID)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected status 201 Created, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp api.WebhookSubscriptionResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.Subscription.Secret != "my-custom-secret-key-123" {
			t.Errorf("expected secret my-custom-secret-key-123, got %s", resp.Subscription.Secret)
		}
		if len(resp.Subscription.Events) != 2 {
			t.Errorf("expected 2 events, got %v", resp.Subscription.Events)
		}
		if resp.Subscription.IsActive {
			t.Errorf("expected is_active to be false")
		}
	})

	t.Run("reject SSRF loopback URL", func(t *testing.T) {
		body := map[string]interface{}{
			"url": "http://127.0.0.1:8080/webhook",
		}
		jsonBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/subscriptions", bytes.NewReader(jsonBytes))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		ctx := tenant.WithWorkspaceID(req.Context(), wsID)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected status 422 UnprocessableEntity for SSRF loopback URL, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("reject SSRF private IP URL", func(t *testing.T) {
		body := map[string]interface{}{
			"url": "http://10.0.0.5/webhook",
		}
		jsonBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/subscriptions", bytes.NewReader(jsonBytes))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		ctx := tenant.WithWorkspaceID(req.Context(), wsID)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected status 422 UnprocessableEntity for SSRF private IP, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("successful creation with allowlisted local URL", func(t *testing.T) {
		allowlistHandler := api.NewWebhookSubscriptionAPIHandler(repo, api.WithSubscriptionAllowlist("localhost", "127.0.0.1"))
		eLocal := echo.New()
		allowlistHandler.RegisterRoutes(eLocal)

		body := map[string]interface{}{
			"url": "http://localhost:8080/api/pergo/webhook",
		}
		jsonBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/subscriptions", bytes.NewReader(jsonBytes))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		ctx := tenant.WithWorkspaceID(req.Context(), wsID)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		eLocal.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected status 201 Created for allowlisted local URL, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("reject missing workspace context", func(t *testing.T) {
		body := map[string]interface{}{
			"url": "https://api.example.com/webhook",
		}
		jsonBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/subscriptions", bytes.NewReader(jsonBytes))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected status 401 Unauthorized, got %d", rec.Code)
		}
	})

	t.Run("reject empty URL", func(t *testing.T) {
		body := map[string]interface{}{
			"url": "",
		}
		jsonBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/subscriptions", bytes.NewReader(jsonBytes))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		ctx := tenant.WithWorkspaceID(req.Context(), wsID)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400 Bad Request, got %d", rec.Code)
		}
	})
}

func TestWebhookSubscriptionAPI_List(t *testing.T) {
	repo := newMockWebhookSubscriptionRepo()
	e, _ := setupWebhookSubscriptionTestRouter(repo)

	ws1 := uuid.New()
	ws2 := uuid.New()

	sub1, _ := repo.Create(context.Background(), ws1, "https://ws1.example.com/wh1", []string{"*"}, []byte("sec1"))
	sub2, _ := repo.Create(context.Background(), ws1, "https://ws1.example.com/wh2", []string{"message.received"}, []byte("sec2"))
	_, _ = repo.Create(context.Background(), ws2, "https://ws2.example.com/wh", []string{"*"}, []byte("sec3"))

	t.Run("list only returns subscriptions belonging to authenticated workspace", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/subscriptions", nil)
		ctx := tenant.WithWorkspaceID(req.Context(), ws1)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp api.WebhookSubscriptionListResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode list response: %v", err)
		}

		if len(resp.Subscriptions) != 2 {
			t.Fatalf("expected 2 subscriptions for ws1, got %d", len(resp.Subscriptions))
		}

		ids := map[uuid.UUID]bool{resp.Subscriptions[0].ID: true, resp.Subscriptions[1].ID: true}
		if !ids[sub1.ID] || !ids[sub2.ID] {
			t.Errorf("expected subscriptions %s and %s in results", sub1.ID, sub2.ID)
		}
	})
}

func TestWebhookSubscriptionAPI_Get(t *testing.T) {
	repo := newMockWebhookSubscriptionRepo()
	e, _ := setupWebhookSubscriptionTestRouter(repo)

	ws1 := uuid.New()
	ws2 := uuid.New()

	sub1, _ := repo.Create(context.Background(), ws1, "https://ws1.example.com/hook", []string{"message.received"}, []byte("sec1"))

	t.Run("get existing subscription in own workspace", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/subscriptions/"+sub1.ID.String(), nil)
		ctx := tenant.WithWorkspaceID(req.Context(), ws1)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp api.WebhookSubscriptionResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.Subscription.ID != sub1.ID {
			t.Errorf("expected ID %s, got %s", sub1.ID, resp.Subscription.ID)
		}
	})

	t.Run("get subscription from other workspace returns 404 (tenant isolation)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/subscriptions/"+sub1.ID.String(), nil)
		ctx := tenant.WithWorkspaceID(req.Context(), ws2)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected status 404 Not Found for cross-workspace access, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("get non-existent subscription returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/subscriptions/"+uuid.New().String(), nil)
		ctx := tenant.WithWorkspaceID(req.Context(), ws1)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected status 404 Not Found, got %d", rec.Code)
		}
	})
}

func TestWebhookSubscriptionAPI_Update(t *testing.T) {
	repo := newMockWebhookSubscriptionRepo()
	e, _ := setupWebhookSubscriptionTestRouter(repo)

	ws1 := uuid.New()
	ws2 := uuid.New()

	sub1, _ := repo.Create(context.Background(), ws1, "https://ws1.example.com/initial", []string{"*"}, []byte("sec1"))

	t.Run("successful update of URL, events, and active state", func(t *testing.T) {
		newURL := "https://ws1.example.com/updated"
		newActive := false
		newEvents := []string{"connection.status", "message.delivered"}

		body := api.UpdateWebhookSubscriptionRequest{
			URL:      &newURL,
			Events:   newEvents,
			IsActive: &newActive,
		}
		jsonBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/webhooks/subscriptions/"+sub1.ID.String(), bytes.NewReader(jsonBytes))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		ctx := tenant.WithWorkspaceID(req.Context(), ws1)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp api.WebhookSubscriptionResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.Subscription.URL != newURL {
			t.Errorf("expected URL %s, got %s", newURL, resp.Subscription.URL)
		}
		if resp.Subscription.IsActive != false {
			t.Errorf("expected is_active false, got true")
		}
		if len(resp.Subscription.Events) != 2 {
			t.Errorf("expected 2 events, got %v", resp.Subscription.Events)
		}
	})

	t.Run("reject update with SSRF target URL", func(t *testing.T) {
		ssrfURL := "http://127.0.0.1:9000/bad"
		body := api.UpdateWebhookSubscriptionRequest{
			URL: &ssrfURL,
		}
		jsonBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/webhooks/subscriptions/"+sub1.ID.String(), bytes.NewReader(jsonBytes))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		ctx := tenant.WithWorkspaceID(req.Context(), ws1)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected status 422 UnprocessableEntity, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("reject update on subscription from different workspace", func(t *testing.T) {
		newURL := "https://ws2.example.com/hack"
		body := api.UpdateWebhookSubscriptionRequest{
			URL: &newURL,
		}
		jsonBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/webhooks/subscriptions/"+sub1.ID.String(), bytes.NewReader(jsonBytes))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		ctx := tenant.WithWorkspaceID(req.Context(), ws2)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected status 404 Not Found for cross-workspace update, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestWebhookSubscriptionAPI_Delete(t *testing.T) {
	repo := newMockWebhookSubscriptionRepo()
	e, _ := setupWebhookSubscriptionTestRouter(repo)

	ws1 := uuid.New()
	ws2 := uuid.New()

	sub1, _ := repo.Create(context.Background(), ws1, "https://ws1.example.com/to-delete", []string{"*"}, []byte("sec1"))

	t.Run("reject delete on subscription from different workspace", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/webhooks/subscriptions/"+sub1.ID.String(), nil)
		ctx := tenant.WithWorkspaceID(req.Context(), ws2)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected status 404 Not Found for cross-workspace delete, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("successful delete of subscription in own workspace", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/webhooks/subscriptions/"+sub1.ID.String(), nil)
		ctx := tenant.WithWorkspaceID(req.Context(), ws1)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp["status"] != "deleted" || resp["id"] != sub1.ID.String() {
			t.Errorf("unexpected delete response: %v", resp)
		}

		// Verify deletion
		_, err := repo.Get(context.Background(), sub1.ID)
		if err != repository.ErrWebhookSubscriptionNotFound {
			t.Errorf("expected ErrWebhookSubscriptionNotFound after delete, got %v", err)
		}
	})
}
