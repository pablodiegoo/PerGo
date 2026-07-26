package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestWABAMetaClient_RateLimiting(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer ts.Close()

	cli := NewWABAMetaClient(ts.Client(), ts.URL)
	connID := uuid.New()
	wsID := uuid.New()

	ctx := context.Background()

	// First call should succeed
	_, err := cli.SyncTemplates(ctx, connID, "waba123", "tok123", wsID, nil, false)
	if err != nil {
		t.Fatalf("first sync failed unexpectedly: %v", err)
	}

	// Second call within 15 mins should fail with ErrSyncRateLimited
	_, err = cli.SyncTemplates(ctx, connID, "waba123", "tok123", wsID, nil, false)
	if err != ErrSyncRateLimited {
		t.Fatalf("expected ErrSyncRateLimited, got %v", err)
	}

	// Call with bypassRateLimit = true should succeed
	_, err = cli.SyncTemplates(ctx, connID, "waba123", "tok123", wsID, nil, true)
	if err != nil {
		t.Fatalf("bypass sync failed: %v", err)
	}

	// Simulate 16 minutes pass
	cli.mu.Lock()
	cli.lastSyncTime[connID] = time.Now().Add(-16 * time.Minute)
	cli.mu.Unlock()

	// Call after 15 mins should succeed
	_, err = cli.SyncTemplates(ctx, connID, "waba123", "tok123", wsID, nil, false)
	if err != nil {
		t.Fatalf("sync after expiry failed: %v", err)
	}
}
