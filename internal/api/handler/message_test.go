package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.omnigo/internal/api/middleware"
	"github.com/pablojhp.omnigo/internal/domain"
	"github.com/pablojhp.omnigo/internal/platform/postgres/tenant"
)

// testContext returns a context with trace_id and workspace_id injected.
func testContext(traceID string, workspaceID uuid.UUID) context.Context {
	ctx := context.Background()
	ctx = middleware.WithContext(ctx, traceID)
	ctx = tenant.WithWorkspaceID(ctx, workspaceID)
	return ctx
}

func TestCreateMessageValid(t *testing.T) {
	e := echo.New()
	h := &MessageHandler{}
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
	h := &MessageHandler{}
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
	h := &MessageHandler{}
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
	h := &MessageHandler{}
	h.RegisterRoutes(e)

	traceID := uuid.New().String()
	wsID := uuid.New()

	body := `{"to":"+1234567890","channel":"sms","body":"Hello"}`
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
		if d.Field == "channel" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected field error for 'channel', got %+v", resp.Details)
	}
}

func TestCreateMessageZeroTTL(t *testing.T) {
	e := echo.New()
	h := &MessageHandler{}
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
	h := &MessageHandler{}
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
	h := &MessageHandler{}
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
	h := &MessageHandler{QueueDepth: qdt}
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
	h := &MessageHandler{QueueDepth: qdt}
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
	h := &MessageHandler{QueueDepth: qdt}
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
	h := &MessageHandler{QueueDepth: qdt}
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
	h := &MessageHandler{Publisher: pub}
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
	h := &MessageHandler{}
	h.RegisterRoutes(e)

	tests := []struct {
		name          string
		body          string
		expectedField string
	}{
		{
			name:          "unsupported fallback channel",
			body:          `{"to":"+1234567890","channel":"whatsapp","body":"Hello","fallback_channels":["sms"]}`,
			expectedField: "fallback_channels[0]",
		},
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
