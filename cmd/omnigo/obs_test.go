package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pablojhp.omnigo/internal/platform/obs"
	"github.com/pablojhp.omnigo/internal/platform/shutdown"
)

// TestPprofServer verifies the debug server starts and serves pprof on a random port.
func TestPprofServer(t *testing.T) {
	ds := obs.StartDebugServer("127.0.0.1:0")
	defer ds.Shutdown(context.Background())

	addr := ds.Addr()

	resp, err := http.Get(fmt.Sprintf("http://%s/debug/pprof/", addr))
	if err != nil {
		t.Fatalf("GET /debug/pprof/ failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		t.Error("expected Content-Type header")
	}
}

// TestExpvarHandler verifies expvar is available at /debug/vars with memstats.
func TestExpvarHandler(t *testing.T) {
	ds := obs.StartDebugServer("127.0.0.1:0")
	defer ds.Shutdown(context.Background())

	resp, err := http.Get(fmt.Sprintf("http://%s/debug/vars", ds.Addr()))
	if err != nil {
		t.Fatalf("GET /debug/vars failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var data map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("failed to decode expvar JSON: %v", err)
	}

	if _, ok := data["memstats"]; !ok {
		t.Error("expvar response missing memstats")
	}
}

// TestAuditDropCounter verifies a custom expvar counter can be registered, incremented,
// and queried via /debug/vars.
func TestAuditDropCounter(t *testing.T) {
	counter := obs.RegisterCounter("test_audit_drops_obs")

	ds := obs.StartDebugServer("127.0.0.1:0")
	defer ds.Shutdown(context.Background())

	// Increment the counter
	counter.Add(5)

	resp, err := http.Get(fmt.Sprintf("http://%s/debug/vars", ds.Addr()))
	if err != nil {
		t.Fatalf("GET /debug/vars failed: %v", err)
	}
	defer resp.Body.Close()

	var data map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("failed to decode expvar JSON: %v", err)
	}

	raw, ok := data["test_audit_drops_obs"]
	if !ok {
		t.Fatal("custom counter not found in expvar response")
	}

	var val int64
	if err := json.Unmarshal(raw, &val); err != nil {
		t.Fatalf("failed to unmarshal counter value: %v", err)
	}

	if val != 5 {
		t.Errorf("expected counter value 5, got %d", val)
	}
}

// TestOrchestratorOrder verifies cleanup functions execute in reverse registration order (LIFO).
func TestOrchestratorOrder(t *testing.T) {
	orch := shutdown.NewOrchestrator()

	var order []int
	orch.Register(func() error { order = append(order, 1); return nil })
	orch.Register(func() error { order = append(order, 2); return nil })
	orch.Register(func() error { order = append(order, 3); return nil })

	if err := orch.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	if len(order) != 3 {
		t.Fatalf("expected 3 cleanup calls, got %d", len(order))
	}

	// LIFO order: 3, 2, 1
	expected := []int{3, 2, 1}
	for i, v := range order {
		if v != expected[i] {
			t.Errorf("order[%d] = %d, want %d", i, v, expected[i])
		}
	}
}

// TestOrchestratorTimeout verifies shutdown completes within deadline even with slow cleanup.
func TestOrchestratorTimeout(t *testing.T) {
	orch := shutdown.NewOrchestrator()

	orch.Register(func() error {
		time.Sleep(2 * time.Second)
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	if err := orch.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Errorf("shutdown took %v, expected < 5s", elapsed)
	}
}

// TestOrchestratorSlowCleanup verifies shutdown completes within 30s context
// even when cleanup takes 2s.
func TestOrchestratorSlowCleanup(t *testing.T) {
	orch := shutdown.NewOrchestrator()

	var cleaned atomic.Bool
	orch.Register(func() error {
		time.Sleep(2 * time.Second)
		cleaned.Store(true)
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := orch.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	if !cleaned.Load() {
		t.Error("cleanup function was not called")
	}
}

// TestOrchestratorIdempotent verifies calling Shutdown twice does not panic
// and the second call is a no-op.
func TestOrchestratorIdempotent(t *testing.T) {
	orch := shutdown.NewOrchestrator()

	var count atomic.Int32
	orch.Register(func() error { count.Add(1); return nil })

	if err := orch.Shutdown(context.Background()); err != nil {
		t.Fatalf("first shutdown failed: %v", err)
	}

	if err := orch.Shutdown(context.Background()); err != nil {
		t.Fatalf("second shutdown failed: %v", err)
	}

	if c := count.Load(); c != 1 {
		t.Errorf("expected cleanup called once, got %d", c)
	}
}
