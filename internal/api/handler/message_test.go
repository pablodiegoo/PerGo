package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/outbound"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/session"
)

type mockConnectionRepo struct {
	GetBySlugFunc                   func(ctx context.Context, workspaceID uuid.UUID, slug string) (*repository.Connection, error)
	GetBySenderIdentityFunc         func(ctx context.Context, workspaceID uuid.UUID, senderIdentity string) (*repository.Connection, error)
	GetDefaultChannelConnectionFunc func(ctx context.Context, workspaceID uuid.UUID, channel string) (*repository.Connection, error)
}

func (m *mockConnectionRepo) GetBySlug(ctx context.Context, workspaceID uuid.UUID, slug string) (*repository.Connection, error) {
	if m.GetBySlugFunc != nil {
		return m.GetBySlugFunc(ctx, workspaceID, slug)
	}
	return nil, repository.ErrConnectionNotFound
}

func (m *mockConnectionRepo) GetBySenderIdentity(ctx context.Context, workspaceID uuid.UUID, senderIdentity string) (*repository.Connection, error) {
	if m.GetBySenderIdentityFunc != nil {
		return m.GetBySenderIdentityFunc(ctx, workspaceID, senderIdentity)
	}
	return nil, repository.ErrConnectionNotFound
}

func (m *mockConnectionRepo) GetDefaultChannelConnection(ctx context.Context, workspaceID uuid.UUID, channel string) (*repository.Connection, error) {
	if m.GetDefaultChannelConnectionFunc != nil {
		return m.GetDefaultChannelConnectionFunc(ctx, workspaceID, channel)
	}
	return nil, repository.ErrConnectionNotFound
}

func defaultMockConnectionRepo() *mockConnectionRepo {
	return &mockConnectionRepo{
		GetDefaultChannelConnectionFunc: func(ctx context.Context, workspaceID uuid.UUID, channel string) (*repository.Connection, error) {
			return &repository.Connection{
				ID:             uuid.New(),
				WorkspaceID:    workspaceID,
				Name:           "default " + channel,
				Channel:        channel,
				SenderIdentity: "+1234567890",
				Status:         "active",
				IsDefault:      true,
			}, nil
		},
		GetBySenderIdentityFunc: func(ctx context.Context, workspaceID uuid.UUID, senderIdentity string) (*repository.Connection, error) {
			return &repository.Connection{
				ID:             uuid.New(),
				WorkspaceID:    workspaceID,
				Name:           "custom conn",
				Channel:        "whatsapp",
				SenderIdentity: senderIdentity,
				Status:         "active",
			}, nil
		},
	}
}

func newTestMessageHandler() *MessageHandler {
	return &MessageHandler{
		ConnectionRepo: defaultMockConnectionRepo(),
	}
}

// testContext returns a context with trace_id and workspace_id injected.
func testContext(traceID string, workspaceID uuid.UUID) context.Context {
	ctx := context.Background()
	ctx = middleware.WithContext(ctx, traceID)
	ctx = tenant.WithWorkspaceID(ctx, workspaceID)
	return ctx
}

func TestCreateMessageValid(t *testing.T) {
	e := echo.New()
	h := newTestMessageHandler()
	h.RegisterRoutes(e)

	traceID := uuid.New().String()
	wsID := uuid.New()

	body := `{"to":"+1234567890","channel":"whatsapp","body":"Hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(testContext(traceID, wsID))
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp domain.CreateMessageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp.MessageID == uuid.Nil {
		t.Error("expected non-nil message_id")
	}
	if resp.Status != domain.StatusQueued {
		t.Errorf("status = %q, want %q", resp.Status, domain.StatusQueued)
	}
	if resp.QueuedAt.IsZero() {
		t.Error("expected non-zero queued_at")
	}

	// Check X-Trace-Id header
	traceHeader := rec.Header().Get("X-Trace-Id")
	if traceHeader != traceID {
		t.Errorf("X-Trace-Id = %q, want %q", traceHeader, traceID)
	}
}

func TestCreateMessageInvalidJSON(t *testing.T) {
	e := echo.New()
	h := newTestMessageHandler()
	h.RegisterRoutes(e)

	traceID := uuid.New().String()
	wsID := uuid.New()

	body := `{invalid json`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(testContext(traceID, wsID))
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp domain.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp.Code != "invalid_payload" {
		t.Errorf("code = %q, want %q", resp.Code, "invalid_payload")
	}
}

func TestCreateMessageMissingTo(t *testing.T) {
	e := echo.New()
	h := newTestMessageHandler()
	h.RegisterRoutes(e)

	traceID := uuid.New().String()
	wsID := uuid.New()

	body := `{"channel":"whatsapp","body":"Hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(testContext(traceID, wsID))
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp domain.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp.Code != "invalid_payload" {
		t.Errorf("code = %q, want %q", resp.Code, "invalid_payload")
	}

	found := false
	for _, d := range resp.Details {
		if d.Field == "to" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected field error for 'to', got %+v", resp.Details)
	}
}

func TestCreateMessageInvalidChannel(t *testing.T) {
	e := echo.New()
	mockRepo := &mockConnectionRepo{
		GetBySlugFunc: func(ctx context.Context, workspaceID uuid.UUID, slug string) (*repository.Connection, error) {
			return nil, repository.ErrConnectionNotFound
		},
		GetDefaultChannelConnectionFunc: func(ctx context.Context, workspaceID uuid.UUID, channel string) (*repository.Connection, error) {
			return nil, repository.ErrConnectionNotFound
		},
	}
	h := &MessageHandler{
		Ingestor: outbound.NewProcessor(nil, nil, mockRepo, nil),
	}
	h.RegisterRoutes(e)

	traceID := uuid.New().String()
	wsID := uuid.New()

	body := `{"to":"+1234567890","channel":"unknown-slug","body":"Hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(testContext(traceID, wsID))
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateMessageZeroTTL(t *testing.T) {
	e := echo.New()
	h := newTestMessageHandler()
	h.RegisterRoutes(e)

	traceID := uuid.New().String()
	wsID := uuid.New()

	body := `{"to":"+1234567890","channel":"whatsapp","ttl_seconds":0}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(testContext(traceID, wsID))
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp domain.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	found := false
	for _, d := range resp.Details {
		if d.Field == "ttl_seconds" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected field error for 'ttl_seconds', got %+v", resp.Details)
	}
}

func TestCreateMessageTraceHeader(t *testing.T) {
	e := echo.New()
	h := newTestMessageHandler()
	h.RegisterRoutes(e)

	traceID := "custom-trace-id-12345"
	wsID := uuid.New()

	body := `{"to":"+1234567890","channel":"whatsapp","body":"Hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(testContext(traceID, wsID))
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	traceHeader := rec.Header().Get("X-Trace-Id")
	if traceHeader != traceID {
		t.Errorf("X-Trace-Id = %q, want %q", traceHeader, traceID)
	}
}

func TestCreateMessageMissingAuth(t *testing.T) {
	// Test without auth middleware — handler still works (auth is separate)
	e := echo.New()
	h := newTestMessageHandler()
	h.RegisterRoutes(e)

	traceID := uuid.New().String()
	wsID := uuid.New()

	body := `{"to":"+1234567890","channel":"whatsapp","body":"Hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(testContext(traceID, wsID))
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	// Without auth middleware, the handler processes the request normally
	// (auth is applied at the router level, not the handler level)
	if rec.Code != http.StatusAccepted {
		t.Errorf("expected 202 without auth middleware, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- Queue depth tests ---

func TestCreateMessageQueueFull(t *testing.T) {
	e := echo.New()
	qdt := middleware.NewQueueDepthTracker()
	h := newTestMessageHandler()
	h.QueueDepth = qdt
	h.RegisterRoutes(e)

	wsID := uuid.New()

	// Fill queue to 1000
	for i := 0; i < 1000; i++ {
		qdt.Increment(wsID)
	}

	traceID := uuid.New().String()
	body := `{"to":"+1234567890","channel":"whatsapp","body":"Hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(testContext(traceID, wsID))
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d: %s", rec.Code, rec.Body.String())
	}

	var errResp domain.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if errResp.Code != "queue_full" {
		t.Errorf("error code = %q, want %q", errResp.Code, "queue_full")
	}

	retryAfter := rec.Header().Get("Retry-After")
	if retryAfter != "5" {
		t.Errorf("Retry-After = %q, want %q", retryAfter, "5")
	}
}

func TestCreateMessageQueueNotFull(t *testing.T) {
	e := echo.New()
	qdt := middleware.NewQueueDepthTracker()
	h := newTestMessageHandler()
	h.QueueDepth = qdt
	h.RegisterRoutes(e)

	wsID := uuid.New()
	// Only 999 messages — should be allowed
	for i := 0; i < 999; i++ {
		qdt.Increment(wsID)
	}

	traceID := uuid.New().String()
	body := `{"to":"+1234567890","channel":"whatsapp","body":"Hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(testContext(traceID, wsID))
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("expected 202 when queue not full, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateMessageQueueDepthIncremented(t *testing.T) {
	e := echo.New()
	qdt := middleware.NewQueueDepthTracker()
	h := newTestMessageHandler()
	h.QueueDepth = qdt
	h.RegisterRoutes(e)

	wsID := uuid.New()

	traceID := uuid.New().String()
	body := `{"to":"+1234567890","channel":"whatsapp","body":"Hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(testContext(traceID, wsID))
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec.Code)
	}

	// Queue depth should be incremented after successful publish
	if d := qdt.Depth(wsID); d != 1 {
		t.Errorf("queue depth = %d, want 1 after successful publish", d)
	}
}

func TestCreateMessageRateLimited(t *testing.T) {
	e := echo.New()
	rl := middleware.NewRateLimiter(2, 1) // 2 req/s, burst 1
	qdt := middleware.NewQueueDepthTracker()
	h := newTestMessageHandler()
	h.QueueDepth = qdt
	h.RegisterRoutes(e, middleware.RateLimiterMiddleware(rl))

	wsID := uuid.New()

	body := `{"to":"+1234567890","channel":"whatsapp","body":"Hello"}`

	// First request — allowed
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	req1 = req1.WithContext(testContext(uuid.New().String(), wsID))
	rec1 := httptest.NewRecorder()
	e.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusAccepted {
		t.Errorf("first request: expected 202, got %d", rec1.Code)
	}

	// Second request — burst exhausted
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2 = req2.WithContext(testContext(uuid.New().String(), wsID))
	rec2 := httptest.NewRecorder()
	e.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("second request: expected 429, got %d: %s", rec2.Code, rec2.Body.String())
	}

	var errResp domain.ErrorResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if errResp.Code != "rate_limited" {
		t.Errorf("error code = %q, want %q", errResp.Code, "rate_limited")
	}
}

type mockPublisher struct {
	subject string
	data    []byte
	traceID string
	err     error
}

func (m *mockPublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	m.subject = subject
	m.data = data
	m.traceID = traceID
	return m.err
}

func TestCreateMessageWithFallbackChannels(t *testing.T) {
	e := echo.New()
	pub := &mockPublisher{}
	h := newTestMessageHandler()
	h.Publisher = pub
	h.RegisterRoutes(e)

	traceID := uuid.New().String()
	wsID := uuid.New()

	body := `{"to":"+1234567890","channel":"whatsapp","body":"Hello","fallback_channels":["whatsapp_cloud","telegram"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(testContext(traceID, wsID))
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the published payload is QueueMessage and contains all fields
	var qMsg domain.QueueMessage
	if err := json.Unmarshal(pub.data, &qMsg); err != nil {
		t.Fatalf("failed to unmarshal published data: %v", err)
	}

	if qMsg.WorkspaceID != wsID {
		t.Errorf("expected WorkspaceID %s, got %s", wsID, qMsg.WorkspaceID)
	}
	if qMsg.TraceID != traceID {
		t.Errorf("expected TraceID %s, got %s", traceID, qMsg.TraceID)
	}
	if qMsg.Channel != "whatsapp" {
		t.Errorf("expected Channel whatsapp, got %s", qMsg.Channel)
	}
	if len(qMsg.FallbackChannels) != 2 || qMsg.FallbackChannels[0] != "whatsapp_cloud" || qMsg.FallbackChannels[1] != "telegram" {
		t.Errorf("expected FallbackChannels [whatsapp_cloud, telegram], got %v", qMsg.FallbackChannels)
	}
}

func TestCreateMessageInvalidFallbackChannels(t *testing.T) {
	e := echo.New()
	h := newTestMessageHandler()
	h.RegisterRoutes(e)

	tests := []struct {
		name          string
		body          string
		expectedField string
	}{
		{
			name:          "duplicate fallback channel",
			body:          `{"to":"+1234567890","channel":"whatsapp","body":"Hello","fallback_channels":["telegram","telegram"]}`,
			expectedField: "fallback_channels[1]",
		},
		{
			name:          "fallback channel same as primary",
			body:          `{"to":"+1234567890","channel":"whatsapp","body":"Hello","fallback_channels":["whatsapp"]}`,
			expectedField: "fallback_channels[0]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(testContext(uuid.New().String(), uuid.New()))
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}

			var resp domain.ErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}

			found := false
			for _, d := range resp.Details {
				if d.Field == tc.expectedField {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected field error for %q, got details: %+v", tc.expectedField, resp.Details)
			}
		})
	}
}

func TestCreateMessageWithFrom(t *testing.T) {
	e := echo.New()
	pub := &mockPublisher{}
	h := newTestMessageHandler()
	h.Publisher = pub
	h.RegisterRoutes(e)

	traceID := uuid.New().String()
	wsID := uuid.New()

	body := `{"to":"+1234567890","channel":"whatsapp","body":"Hello","from":"+1234567890"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(testContext(traceID, wsID))
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	var qMsg domain.QueueMessage
	if err := json.Unmarshal(pub.data, &qMsg); err != nil {
		t.Fatalf("failed to unmarshal published data: %v", err)
	}

	if qMsg.SenderIdentity != "+1234567890" {
		t.Errorf("expected SenderIdentity '+1234567890', got %q", qMsg.SenderIdentity)
	}
	if qMsg.ConnectionID == uuid.Nil {
		t.Error("expected non-nil resolved ConnectionID")
	}
}

func TestCreateMessageRouteNotFound(t *testing.T) {
	e := echo.New()
	h := newTestMessageHandler()
	// Override with a mock repo that returns errors for lookups
	h.ConnectionRepo = &mockConnectionRepo{
		GetBySenderIdentityFunc: func(ctx context.Context, workspaceID uuid.UUID, senderIdentity string) (*repository.Connection, error) {
			return nil, repository.ErrConnectionNotFound
		},
		GetDefaultChannelConnectionFunc: func(ctx context.Context, workspaceID uuid.UUID, channel string) (*repository.Connection, error) {
			return nil, repository.ErrConnectionNotFound
		},
	}
	h.RegisterRoutes(e)

	traceID := uuid.New().String()
	wsID := uuid.New()

	body := `{"to":"+1234567890","channel":"whatsapp","body":"Hello","from":"+9999999"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(testContext(traceID, wsID))
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp domain.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp.Code != "route_not_found" {
		t.Errorf("expected code 'route_not_found', got %q", resp.Code)
	}
}

func TestCreateMessageChannelMismatch(t *testing.T) {
	e := echo.New()
	h := newTestMessageHandler()
	// Override mock repo to return a connection with a channel that doesn't match requested
	h.ConnectionRepo = &mockConnectionRepo{
		GetBySenderIdentityFunc: func(ctx context.Context, workspaceID uuid.UUID, senderIdentity string) (*repository.Connection, error) {
			return &repository.Connection{
				ID:             uuid.New(),
				WorkspaceID:    workspaceID,
				Channel:        "telegram", // mismatches requested 'whatsapp'
				SenderIdentity: senderIdentity,
			}, nil
		},
	}
	h.RegisterRoutes(e)

	traceID := uuid.New().String()
	wsID := uuid.New()

	body := `{"to":"+1234567890","channel":"whatsapp","body":"Hello","from":"@some_bot"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(testContext(traceID, wsID))
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp domain.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp.Code != "route_not_found" {
		t.Errorf("expected code 'route_not_found', got %q", resp.Code)
	}
}

func TestMessageHandler_ProductValidation(t *testing.T) {
	t.Run("Missing catalog_id returns HTTP 422 missing_catalog_id", func(t *testing.T) {
		e := echo.New()
		h := newTestMessageHandler()
		h.RegisterRoutes(e)

		traceID := uuid.New().String()
		wsID := uuid.New()

		body := `{"to":"+1234567890","channel":"whatsapp","type":"product","product":{"product_retailer_id":"sku_123"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(testContext(traceID, wsID))
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp domain.ErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}

		if resp.Code != "missing_catalog_id" {
			t.Errorf("expected code 'missing_catalog_id', got %q", resp.Code)
		}
	})

	t.Run("Valid product message with default_catalog_id returns HTTP 202", func(t *testing.T) {
		e := echo.New()
		creds, _ := json.Marshal(map[string]string{
			"default_catalog_id": "cat_default_789",
		})
		repo := &mockConnectionRepo{
			GetDefaultChannelConnectionFunc: func(ctx context.Context, workspaceID uuid.UUID, channel string) (*repository.Connection, error) {
				return &repository.Connection{
					ID:             uuid.New(),
					WorkspaceID:    workspaceID,
					Channel:        channel,
					SenderIdentity: "+1234567890",
					Status:         "active",
					Credentials:    creds,
				}, nil
			},
		}
		h := &MessageHandler{ConnectionRepo: repo}
		h.RegisterRoutes(e)

		traceID := uuid.New().String()
		wsID := uuid.New()

		body := `{"to":"+1234567890","channel":"whatsapp","type":"product","product":{"product_retailer_id":"sku_123"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(testContext(traceID, wsID))
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Invalid product payload returns HTTP 422 invalid_product_payload", func(t *testing.T) {
		e := echo.New()
		h := newTestMessageHandler()
		h.RegisterRoutes(e)

		traceID := uuid.New().String()
		wsID := uuid.New()

		body := `{"to":"+1234567890","channel":"whatsapp","type":"product_list","product":{"catalog_id":"cat_123","sections":[{"title":"This title is far too long for a product list section title","product_items":[{"product_retailer_id":"sku_1"}]}]}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(testContext(traceID, wsID))
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp domain.ErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}

		if resp.Code != "invalid_product_payload" {
			t.Errorf("expected code 'invalid_product_payload', got %q", resp.Code)
		}
		if len(resp.Details) == 0 {
			t.Errorf("expected validation details in response")
		}
	})
}

type mockSessionReaderForHandler struct {
	sess *repository.RecipientSession
	err  error
}

func (m *mockSessionReaderForHandler) Get(ctx context.Context, key domain.SessionKey) (*repository.RecipientSession, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.sess != nil {
		return m.sess, nil
	}
	return nil, repository.ErrSessionNotFound
}

func TestCreateMessage_CustomerServiceWindow(t *testing.T) {
	traceID := uuid.New().String()
	wsID := uuid.New()
	senderIdentity := "+5511888880000"
	contactPhone := "+5511999990000"

	wabaConn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    wsID,
		Name:           "WABA Main",
		Channel:        "whatsapp_cloud",
		SenderIdentity: senderIdentity,
		Status:         "active",
		IsDefault:      true,
	}

	waWebConn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    wsID,
		Name:           "WhatsApp Web Main",
		Channel:        "whatsapp",
		SenderIdentity: "+5511777770000",
		Status:         "active",
		IsDefault:      true,
	}

	telegramConn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    wsID,
		Name:           "Telegram Bot",
		Channel:        "telegram",
		SenderIdentity: "@my_bot",
		Status:         "active",
		IsDefault:      true,
	}

	mockRepo := &mockConnectionRepo{
		GetDefaultChannelConnectionFunc: func(ctx context.Context, workspaceID uuid.UUID, channel string) (*repository.Connection, error) {
			switch channel {
			case "whatsapp_cloud":
				return wabaConn, nil
			case "whatsapp", "whatsapp_web":
				return waWebConn, nil
			case "telegram":
				return telegramConn, nil
			default:
				return nil, repository.ErrConnectionNotFound
			}
		},
		GetBySenderIdentityFunc: func(ctx context.Context, workspaceID uuid.UUID, senderIdentity string) (*repository.Connection, error) {
			if senderIdentity == wabaConn.SenderIdentity {
				return wabaConn, nil
			}
			if senderIdentity == waWebConn.SenderIdentity {
				return waWebConn, nil
			}
			if senderIdentity == telegramConn.SenderIdentity {
				return telegramConn, nil
			}
			return nil, repository.ErrConnectionNotFound
		},
	}

	t.Run("WABA freeform within 24h standard session returns 202", func(t *testing.T) {
		e := echo.New()
		sessReader := &mockSessionReaderForHandler{
			sess: &repository.RecipientSession{
				SessionKey: domain.SessionKey{
					WorkspaceID:       wsID,
					RecipientPhone:    contactPhone,
					Channel:           "whatsapp_cloud",
					RecipientIdentity: senderIdentity,
				},
				LastInboundAt:  time.Now().Add(-2 * time.Hour),
				EntryPointType: "standard",
			},
		}
		h := &MessageHandler{
			ConnectionRepo: mockRepo,
			WindowChecker:  session.NewWindowChecker(sessReader),
		}
		h.RegisterRoutes(e)

		body := `{"to":"` + contactPhone + `","channel":"whatsapp_cloud","body":"Valid freeform reply"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(testContext(traceID, wsID))
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("WABA freeform within 72h CTWA session returns 202", func(t *testing.T) {
		e := echo.New()
		sessReader := &mockSessionReaderForHandler{
			sess: &repository.RecipientSession{
				SessionKey: domain.SessionKey{
					WorkspaceID:       wsID,
					RecipientPhone:    contactPhone,
					Channel:           "whatsapp_cloud",
					RecipientIdentity: senderIdentity,
				},
				LastInboundAt:  time.Now().Add(-48 * time.Hour),
				EntryPointType: "ctwa",
			},
		}
		h := &MessageHandler{
			ConnectionRepo: mockRepo,
			WindowChecker:  session.NewWindowChecker(sessReader),
		}
		h.RegisterRoutes(e)

		body := `{"to":"` + contactPhone + `","channel":"whatsapp_cloud","body":"Valid CTWA reply within 72h"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(testContext(traceID, wsID))
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("WABA freeform outside window returns 422 SESSION_WINDOW_EXPIRED with schema compliance", func(t *testing.T) {
		e := echo.New()
		lastInbound := time.Now().Add(-26 * time.Hour)
		sessReader := &mockSessionReaderForHandler{
			sess: &repository.RecipientSession{
				SessionKey: domain.SessionKey{
					WorkspaceID:       wsID,
					RecipientPhone:    contactPhone,
					Channel:           "whatsapp_cloud",
					RecipientIdentity: senderIdentity,
				},
				LastInboundAt:  lastInbound,
				EntryPointType: "standard",
			},
		}
		h := &MessageHandler{
			ConnectionRepo: mockRepo,
			WindowChecker:  session.NewWindowChecker(sessReader),
		}
		h.RegisterRoutes(e)

		body := `{"to":"` + contactPhone + `","channel":"whatsapp_cloud","body":"Blocked outside window"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(testContext(traceID, wsID))
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp struct {
			Code    string            `json:"code"`
			Message string            `json:"message"`
			Details map[string]string `json:"details"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse SESSION_WINDOW_EXPIRED response: %v", err)
		}

		if resp.Code != "SESSION_WINDOW_EXPIRED" {
			t.Errorf("code = %q, want %q", resp.Code, "SESSION_WINDOW_EXPIRED")
		}
		if resp.Message != "Customer service window expired for recipient" {
			t.Errorf("message = %q, want %q", resp.Message, "Customer service window expired for recipient")
		}
		if resp.Details == nil {
			t.Fatalf("details map is nil")
		}
		if resp.Details["hint"] != "Use type: template to reach this contact" {
			t.Errorf("details.hint = %q, want %q", resp.Details["hint"], "Use type: template to reach this contact")
		}
		if resp.Details["source"] != "ingestion" {
			t.Errorf("details.source = %q, want %q", resp.Details["source"], "ingestion")
		}
		if resp.Details["window_duration"] != "24h0m0s" {
			t.Errorf("details.window_duration = %q, want %q", resp.Details["window_duration"], "24h0m0s")
		}
		expectedExpiredAt := lastInbound.Add(24 * time.Hour).Format(time.RFC3339)
		if resp.Details["window_expired_at"] != expectedExpiredAt {
			t.Errorf("details.window_expired_at = %q, want %q", resp.Details["window_expired_at"], expectedExpiredAt)
		}
	})

	t.Run("WhatsApp Web (whatsmeow) ignores window restrictions and returns 202", func(t *testing.T) {
		e := echo.New()
		sessReader := &mockSessionReaderForHandler{
			sess: nil, // no session at all
		}
		h := &MessageHandler{
			ConnectionRepo: mockRepo,
			WindowChecker:  session.NewWindowChecker(sessReader),
		}
		h.RegisterRoutes(e)

		body := `{"to":"` + contactPhone + `","channel":"whatsapp","body":"WhatsApp Web bypasses window"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(testContext(traceID, wsID))
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202 for WhatsApp Web, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Telegram ignores window restrictions and returns 202", func(t *testing.T) {
		e := echo.New()
		sessReader := &mockSessionReaderForHandler{
			sess: nil, // no session at all
		}
		h := &MessageHandler{
			ConnectionRepo: mockRepo,
			WindowChecker:  session.NewWindowChecker(sessReader),
		}
		h.RegisterRoutes(e)

		body := `{"to":"123456789","channel":"telegram","body":"Telegram bypasses window"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(testContext(traceID, wsID))
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202 for Telegram, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}
