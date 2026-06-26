package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.omnigo/internal/domain"
	"github.com/pablojhp.omnigo/internal/platform/postgres/tenant"
)

// --- RateLimiter tests ---

func TestRateLimiterAllow(t *testing.T) {
	// Create limiter with 2 req/s and burst of 2
	rl := NewRateLimiter(2, 2)
	wsID := uuid.New()

	// First two requests should be allowed (burst = 2)
	if !rl.Allow(wsID) {
		t.Error("first request should be allowed")
	}
	if !rl.Allow(wsID) {
		t.Error("second request should be allowed")
	}
	// Third should be denied (burst exhausted)
	if rl.Allow(wsID) {
		t.Error("third request should be denied after burst exhausted")
	}
}

func TestRateLimiterPerWorkspace(t *testing.T) {
	rl := NewRateLimiter(2, 2)
	ws1 := uuid.New()
	ws2 := uuid.New()

	// Exhaust ws1 burst
	rl.Allow(ws1)
	rl.Allow(ws1)

	// ws1 denied
	if rl.Allow(ws1) {
		t.Error("ws1 should be denied after burst")
	}

	// ws2 still has burst available
	if !rl.Allow(ws2) {
		t.Error("ws2 should have independent limiter")
	}
}

func TestRateLimiterReplenish(t *testing.T) {
	// Use a high rate so tokens replenish quickly
	rl := NewRateLimiter(100, 1)
	wsID := uuid.New()

	// Use the one token
	if !rl.Allow(wsID) {
		t.Error("first request should be allowed")
	}
	// Denied immediately
	if rl.Allow(wsID) {
		t.Error("second request should be denied")
	}

	// Wait for replenishment (1/100 = 10ms per token)
	time.Sleep(20 * time.Millisecond)

	// Should be allowed again
	if !rl.Allow(wsID) {
		t.Error("request should be allowed after replenishment")
	}
}

func TestRateLimiterMiddleware(t *testing.T) {
	e := echo.New()
	rl := NewRateLimiter(2, 1) // 2 req/s, burst 1

	h := func(c *echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"ok": "true"})
	}

	// Apply rate limiter middleware
	e.POST("/test", h, RateLimiterMiddleware(rl))

	traceID := uuid.New().String()
	wsID := uuid.New()

	// First request — should pass
	body := `{"test":"data"}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := tenant.WithWorkspaceID(req.Context(), wsID)
	req = req.WithContext(ctx)
	_ = traceID
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("first request: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Second request — burst exhausted, should get 429
	req2 := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	ctx2 := tenant.WithWorkspaceID(req2.Context(), wsID)
	req2 = req2.WithContext(ctx2)
	rec2 := httptest.NewRecorder()

	e.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("second request: expected 429, got %d: %s", rec2.Code, rec2.Body.String())
	}

	// Verify error response format
	var errResp domain.ErrorResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if errResp.Code != "rate_limited" {
		t.Errorf("error code = %q, want %q", errResp.Code, "rate_limited")
	}

	// Verify Retry-After header
	retryAfter := rec2.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Error("expected Retry-After header on 429")
	}
}

func TestRateLimiterMiddlewareNoWorkspace(t *testing.T) {
	e := echo.New()
	rl := NewRateLimiter(2, 1)

	h := func(c *echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"ok": "true"})
	}

	e.POST("/test", h, RateLimiterMiddleware(rl))

	// Request without workspace_id in context — should pass through
	body := `{"test":"data"}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// No workspace_id injected
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 when no workspace_id, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- QueueDepthTracker tests ---

func TestQueueDepthTrackerIncrementDecrement(t *testing.T) {
	qdt := NewQueueDepthTracker()
	wsID := uuid.New()

	// Increment 5 times
	for i := 0; i < 5; i++ {
		qdt.Increment(wsID)
	}

	if d := qdt.Depth(wsID); d != 5 {
		t.Errorf("depth after 5 increments = %d, want 5", d)
	}

	// Decrement 2 times
	qdt.Decrement(wsID)
	qdt.Decrement(wsID)

	if d := qdt.Depth(wsID); d != 3 {
		t.Errorf("depth after 5 inc + 2 dec = %d, want 3", d)
	}
}

func TestQueueDepthTrackerDecrementFloor(t *testing.T) {
	qdt := NewQueueDepthTracker()
	wsID := uuid.New()

	// Decrement from 0 should not go negative
	qdt.Decrement(wsID)
	qdt.Decrement(wsID)

	if d := qdt.Depth(wsID); d != 0 {
		t.Errorf("depth after decrementing from 0 = %d, want 0", d)
	}
}

func TestQueueDepthTrackerExceeds(t *testing.T) {
	qdt := NewQueueDepthTracker()
	wsID := uuid.New()

	// Increment to 1000
	for i := 0; i < 1000; i++ {
		qdt.Increment(wsID)
	}

	if !qdt.Exceeds(wsID, 1000) {
		t.Error("Exceeds(1000) should be true when depth = 1000")
	}
	if qdt.Exceeds(wsID, 999) {
		t.Error("Exceeds(999) should be false when depth = 1000")
	}
}

func TestQueueDepthTrackerPerWorkspace(t *testing.T) {
	qdt := NewQueueDepthTracker()
	ws1 := uuid.New()
	ws2 := uuid.New()

	for i := 0; i < 500; i++ {
		qdt.Increment(ws1)
	}
	for i := 0; i < 300; i++ {
		qdt.Increment(ws2)
	}

	if d := qdt.Depth(ws1); d != 500 {
		t.Errorf("ws1 depth = %d, want 500", d)
	}
	if d := qdt.Depth(ws2); d != 300 {
		t.Errorf("ws2 depth = %d, want 300", d)
	}
}
