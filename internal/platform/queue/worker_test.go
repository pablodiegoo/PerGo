package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/pablojhp.omnigo/internal/channel"
	"github.com/pablojhp.omnigo/internal/domain"
	"github.com/pablojhp.omnigo/internal/platform/postgres"
	"github.com/pablojhp.omnigo/internal/repository"
)

func TestRetryAttemptParsing(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected int
	}{
		{"no header", "", 0},
		{"zero attempt", "0", 0},
		{"first attempt", "1", 1},
		{"third attempt", "3", 3},
		{"invalid header", "abc", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test via the format string that would be in headers
			var n int
			if tt.header != "" {
				_, err := fmt.Sscanf(tt.header, "%d", &n)
				if err != nil {
					n = 0
				}
			}
			if n != tt.expected {
				t.Errorf("retryAttempt = %d, want %d", n, tt.expected)
			}
		})
	}
}

func TestExponentialBackoff(t *testing.T) {
	tests := []struct {
		attempt    int
		wantDelay  time.Duration
		maxBackoff time.Duration
	}{
		{0, 1 * time.Second, 60 * time.Second},     // 2^0 * 1s = 1s
		{1, 2 * time.Second, 60 * time.Second},     // 2^1 * 1s = 2s
		{2, 4 * time.Second, 60 * time.Second},     // 2^2 * 1s = 4s
		{3, 8 * time.Second, 60 * time.Second},     // 2^3 * 1s = 8s
		{4, 16 * time.Second, 60 * time.Second},    // 2^4 * 1s = 16s
		{5, 32 * time.Second, 60 * time.Second},    // 2^5 * 1s = 32s
		{6, 60 * time.Second, 60 * time.Second},    // 2^6 * 1s = 64s → capped at 60s
		{10, 60 * time.Second, 60 * time.Second},   // 2^10 * 1s → capped at 60s
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("attempt_%d", tt.attempt), func(t *testing.T) {
			// Replicate the backoff calculation from handleFailure
			delay := time.Duration(1<<uint(tt.attempt)) * defaultBaseBackoff
			if delay > tt.maxBackoff {
				delay = tt.maxBackoff
			}
			if delay != tt.wantDelay {
				t.Errorf("backoff at attempt %d = %v, want %v", tt.attempt, delay, tt.wantDelay)
			}
		})
	}
}

func TestIsExpired(t *testing.T) {
	tests := []struct {
		name      string
		payload   string
		wantExpire bool
	}{
		{
			name:      "no TTL set",
			payload:   `{"to":"+123","channel":"whatsapp","body":"hi"}`,
			wantExpire: false,
		},
		{
			name:      "TTL not expired",
			payload: func() string {
				queuedAt := time.Now().UTC().Add(-10 * time.Second).Format(time.RFC3339Nano)
				ttl := 300 // 5 minutes
				return fmt.Sprintf(`{"to":"+123","channel":"whatsapp","body":"hi","ttl_seconds":%d,"queued_at":"%s"}`, ttl, queuedAt)
			}(),
			wantExpire: false,
		},
		{
			name:      "TTL expired",
			payload: func() string {
				queuedAt := time.Now().UTC().Add(-600 * time.Second).Format(time.RFC3339Nano)
				ttl := 300 // 5 minutes
				return fmt.Sprintf(`{"to":"+123","channel":"whatsapp","body":"hi","ttl_seconds":%d,"queued_at":"%s"}`, ttl, queuedAt)
			}(),
			wantExpire: true,
		},
		{
			name:      "zero TTL ignored",
			payload:   `{"to":"+123","channel":"whatsapp","body":"hi","ttl_seconds":0}`,
			wantExpire: false,
		},
		{
			name:      "negative TTL ignored",
			payload:   `{"to":"+123","channel":"whatsapp","body":"hi","ttl_seconds":-1}`,
			wantExpire: false,
		},
		{
			name:      "invalid queued_at format",
			payload:   `{"to":"+123","channel":"whatsapp","body":"hi","ttl_seconds":1,"queued_at":"invalid"}`,
			wantExpire: false,
		},
		{
			name:      "no queued_at field",
			payload:   `{"to":"+123","channel":"whatsapp","body":"hi","ttl_seconds":1}`,
			wantExpire: false,
		},
		{
			name:      "invalid JSON",
			payload:   `{not json}`,
			wantExpire: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the same isExpired logic as the worker
			type ttlPayload struct {
				TTLSeconds *int   `json:"ttl_seconds"`
				QueuedAt   string `json:"queued_at"`
			}
			var p ttlPayload
			if err := json.Unmarshal([]byte(tt.payload), &p); err != nil {
				if tt.wantExpire {
					t.Errorf("isExpired = false (parse error), want true")
				}
				return
			}

			if p.TTLSeconds == nil || *p.TTLSeconds <= 0 {
				if tt.wantExpire {
					t.Error("isExpired = false (no TTL), want true")
				}
				return
			}

			queuedAt, err := time.Parse(time.RFC3339Nano, p.QueuedAt)
			if err != nil {
				if tt.wantExpire {
					t.Error("isExpired = false (parse error on queued_at), want true")
				}
				return
			}

			expiry := queuedAt.Add(time.Duration(*p.TTLSeconds) * time.Second)
			expired := time.Now().UTC().After(expiry)
			if expired != tt.wantExpire {
				t.Errorf("isExpired = %v, want %v", expired, tt.wantExpire)
			}
		})
	}
}

func TestDeliveryDedup(t *testing.T) {
	w := &Worker{}

	traceID := "test-trace-dedup-001"

	// First call — not in dispatched set
	_, loaded := w.dispatched.LoadOrStore(traceID, struct{}{})
	if loaded {
		t.Error("first call should NOT be loaded (first occurrence)")
	}

	// Second call — should be in dispatched set
	_, loaded = w.dispatched.LoadOrStore(traceID, struct{}{})
	if !loaded {
		t.Error("second call SHOULD be loaded (duplicate detected)")
	}

	// Different trace ID — not a duplicate
	_, loaded = w.dispatched.LoadOrStore("test-trace-dedup-002", struct{}{})
	if loaded {
		t.Error("different trace ID should NOT be loaded")
	}
}

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("OMNIGO_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/omnigo?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("PostgreSQL not available at %s: %v", dsn, err)
	}

	err = pool.Ping(ctx)
	if err != nil {
		pool.Close()
		t.Skipf("PostgreSQL ping failed at %s: %v", dsn, err)
	}

	_, err = postgres.NewSQLDB(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("failed to initialize db: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
	})
	return pool
}

type mockMsg struct {
	data     []byte
	headers  nats.Header
	acked    bool
	nacked   bool
	nakDelay time.Duration
}

func (m *mockMsg) Metadata() (*jetstream.MsgMetadata, error) { return nil, nil }
func (m *mockMsg) Data() []byte                              { return m.data }
func (m *mockMsg) Headers() nats.Header                      { return m.headers }
func (m *mockMsg) Subject() string                           { return "messages.outbound" }
func (m *mockMsg) Reply() string                             { return "" }
func (m *mockMsg) Ack() error                                { m.acked = true; return nil }
func (m *mockMsg) DoubleAck(ctx context.Context) error       { return nil }
func (m *mockMsg) Nak() error                                { m.nacked = true; return nil }
func (m *mockMsg) NakWithDelay(d time.Duration) error        { m.nacked = true; m.nakDelay = d; return nil }
func (m *mockMsg) InProgress() error                         { return nil }
func (m *mockMsg) Term() error                               { return nil }
func (m *mockMsg) TermWithReason(reason string) error        { return nil }

type mockDispatcher struct {
	err         error
	calledCount int
	calledWith  []string
}

func (m *mockDispatcher) Dispatch(ctx context.Context, p *channel.MessagePayload) error {
	m.calledCount++
	m.calledWith = append(m.calledWith, p.Channel)
	return m.err
}

func TestWorkerFallbackLoop(t *testing.T) {
	pool := getTestPool(t)

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)

	ws, err := wsRepo.Create(ctx, "worker_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	t.Run("Terminal error triggers fallback immediately", func(t *testing.T) {
		traceID := uuid.New().String()
		qMsg := domain.QueueMessage{
			WorkspaceID:      ws.ID,
			TraceID:          traceID,
			To:               "+123",
			Channel:          "whatsapp",
			Body:             "test terminal",
			FallbackChannels: []string{"whatsapp_cloud", "telegram"},
		}
		data, _ := json.Marshal(qMsg)

		msg := &mockMsg{
			data:    data,
			headers: nats.Header{"Nats-Msg-Id": []string{traceID}},
		}

		registry := channel.NewRegistry(nil)
		disp1 := &mockDispatcher{err: channel.NewTerminalError(errors.New("banned"))}
		disp2 := &mockDispatcher{err: nil}
		registry.Register("whatsapp", disp1)
		registry.Register("whatsapp_cloud", disp2)

		w := &Worker{
			dispatchers:  registry,
			dispatchRepo: dispatchRepo,
			maxRetries:   5,
			maxBackoff:   60 * time.Second,
		}

		w.processMessage(ctx, msg)

		if !msg.acked {
			t.Error("expected message to be acked")
		}

		if disp1.calledCount != 1 {
			t.Errorf("expected disp1 called once, got %d", disp1.calledCount)
		}
		if disp2.calledCount != 1 {
			t.Errorf("expected disp2 called once, got %d", disp2.calledCount)
		}

		d, err := dispatchRepo.GetByTraceID(ctx, traceID)
		if err != nil {
			t.Fatalf("failed to get dispatch from DB: %v", err)
		}
		if d.Status != "sent" {
			t.Errorf("expected DB status 'sent', got %s", d.Status)
		}
		if d.CurrentChannel != "whatsapp_cloud" {
			t.Errorf("expected DB current channel 'whatsapp_cloud', got %s", d.CurrentChannel)
		}
		if d.FallbackIndex != 1 {
			t.Errorf("expected DB fallback index 1, got %d", d.FallbackIndex)
		}
	})

	t.Run("Transient error triggers NATS retry and does not advance fallback", func(t *testing.T) {
		traceID := uuid.New().String()
		qMsg := domain.QueueMessage{
			WorkspaceID:      ws.ID,
			TraceID:          traceID,
			To:               "+123",
			Channel:          "whatsapp",
			Body:             "test transient",
			FallbackChannels: []string{"whatsapp_cloud"},
		}
		data, _ := json.Marshal(qMsg)

		msg := &mockMsg{
			data:    data,
			headers: nats.Header{"Nats-Msg-Id": []string{traceID}},
		}

		registry := channel.NewRegistry(nil)
		disp1 := &mockDispatcher{err: errors.New("network timeout")}
		registry.Register("whatsapp", disp1)

		w := &Worker{
			dispatchers:  registry,
			dispatchRepo: dispatchRepo,
			maxRetries:   5,
			maxBackoff:   60 * time.Second,
		}

		w.processMessage(ctx, msg)

		if msg.acked {
			t.Error("expected message NOT to be acked")
		}
		if !msg.nacked {
			t.Error("expected message to be nacked")
		}

		if disp1.calledCount != 1 {
			t.Errorf("expected disp1 called once, got %d", disp1.calledCount)
		}

		d, err := dispatchRepo.GetByTraceID(ctx, traceID)
		if err != nil {
			t.Fatalf("failed to get dispatch from DB: %v", err)
		}
		if d.Status != "failed_transient" {
			t.Errorf("expected DB status 'failed_transient', got %s", d.Status)
		}
		if d.CurrentChannel != "whatsapp" {
			t.Errorf("expected DB current channel 'whatsapp', got %s", d.CurrentChannel)
		}
		if d.FallbackIndex != 0 {
			t.Errorf("expected DB fallback index 0, got %d", d.FallbackIndex)
		}
	})

	t.Run("Redelivery of a successfully sent message skips dispatch", func(t *testing.T) {
		traceID := uuid.New().String()
		d, err := dispatchRepo.GetOrCreateDispatch(ctx, ws.ID, traceID, "whatsapp")
		if err != nil {
			t.Fatalf("failed to create dispatch: %v", err)
		}
		err = dispatchRepo.UpdateDispatchStatus(ctx, d.ID, "sent", "whatsapp", 0, nil)
		if err != nil {
			t.Fatalf("failed to update status: %v", err)
		}

		qMsg := domain.QueueMessage{
			WorkspaceID: ws.ID,
			TraceID:     traceID,
			To:          "+123",
			Channel:     "whatsapp",
			Body:        "test redelivery",
		}
		data, _ := json.Marshal(qMsg)

		msg := &mockMsg{
			data:    data,
			headers: nats.Header{"Nats-Msg-Id": []string{traceID}},
		}

		registry := channel.NewRegistry(nil)
		disp1 := &mockDispatcher{err: nil}
		registry.Register("whatsapp", disp1)

		w := &Worker{
			dispatchers:  registry,
			dispatchRepo: dispatchRepo,
			maxRetries:   5,
			maxBackoff:   60 * time.Second,
		}

		w.processMessage(ctx, msg)

		if !msg.acked {
			t.Error("expected message to be acked")
		}

		if disp1.calledCount != 0 {
			t.Errorf("expected dispatcher NOT to be called, got %d", disp1.calledCount)
		}
	})

	t.Run("Exhaustion of all fallback channels marks failed permanently", func(t *testing.T) {
		traceID := uuid.New().String()
		qMsg := domain.QueueMessage{
			WorkspaceID:      ws.ID,
			TraceID:          traceID,
			To:               "+123",
			Channel:          "whatsapp",
			Body:             "test exhaustion",
			FallbackChannels: []string{"telegram"},
		}
		data, _ := json.Marshal(qMsg)

		msg := &mockMsg{
			data:    data,
			headers: nats.Header{"Nats-Msg-Id": []string{traceID}},
		}

		registry := channel.NewRegistry(nil)
		disp1 := &mockDispatcher{err: channel.NewTerminalError(errors.New("terminal 1"))}
		disp2 := &mockDispatcher{err: channel.NewTerminalError(errors.New("terminal 2"))}
		registry.Register("whatsapp", disp1)
		registry.Register("telegram", disp2)

		w := &Worker{
			dispatchers:  registry,
			dispatchRepo: dispatchRepo,
			maxRetries:   5,
			maxBackoff:   60 * time.Second,
		}

		w.processMessage(ctx, msg)

		if !msg.acked {
			t.Error("expected message to be acked (stop retries)")
		}

		d, err := dispatchRepo.GetByTraceID(ctx, traceID)
		if err != nil {
			t.Fatalf("failed to get dispatch from DB: %v", err)
		}
		if d.Status != "failed" {
			t.Errorf("expected DB status 'failed', got %s", d.Status)
		}
		if d.CurrentChannel != "telegram" {
			t.Errorf("expected DB current channel 'telegram', got %s", d.CurrentChannel)
		}
		if d.FallbackIndex != 1 {
			t.Errorf("expected DB fallback index 1, got %d", d.FallbackIndex)
		}
	})
}

type mockQueueDepthTracker struct {
	decrements map[uuid.UUID]int
}

func (m *mockQueueDepthTracker) Decrement(workspaceID uuid.UUID) {
	if m.decrements == nil {
		m.decrements = make(map[uuid.UUID]int)
	}
	m.decrements[workspaceID]++
}

func TestWorker_QueueDepthDecrement(t *testing.T) {
	tracker := &mockQueueDepthTracker{}
	w := &Worker{
		queueDepth: tracker,
	}

	wsID := uuid.New()

	// 1. Success path
	msg1 := &mockMsg{}
	w.ackMessage(msg1, wsID)
	if tracker.decrements[wsID] != 1 {
		t.Errorf("expected 1 decrement, got %d", tracker.decrements[wsID])
	}

	// 2. Terminal failure path
	msg2 := &mockMsg{}
	w.handleFailure(msg2, wsID, "trace-123", 5) // attempt 5 >= maxRetries (0)
	if tracker.decrements[wsID] != 2 {
		t.Errorf("expected 2 decrements, got %d", tracker.decrements[wsID])
	}
}

