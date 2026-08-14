package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pablojhp.pergo/internal/channel"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/audit"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestRetryAttemptParsing(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		expected int
	}{
		{"no headers", nil, 0},
		{"no retry header", map[string]string{}, 0},
		{"zero attempt", map[string]string{"X-Retry-Attempt": "0"}, 0},
		{"first attempt", map[string]string{"X-Retry-Attempt": "1"}, 1},
		{"third attempt", map[string]string{"X-Retry-Attempt": "3"}, 3},
		{"invalid", map[string]string{"X-Retry-Attempt": "abc"}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &fakeDispatchMsg{headers: tt.headers}
			got := retryAttempt(msg)
			if got != tt.expected {
				t.Errorf("retryAttempt = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestExponentialBackoff(t *testing.T) {
	tests := []struct {
		attempt    int
		maxBackoff time.Duration
		wantDelay  time.Duration
	}{
		{0, 60 * time.Second, 1 * time.Second},
		{1, 60 * time.Second, 2 * time.Second},
		{2, 60 * time.Second, 4 * time.Second},
		{3, 60 * time.Second, 8 * time.Second},
		{4, 60 * time.Second, 16 * time.Second},
		{5, 60 * time.Second, 32 * time.Second},
		{6, 60 * time.Second, 60 * time.Second},
		{10, 60 * time.Second, 60 * time.Second},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("attempt_%d", tt.attempt), func(t *testing.T) {
			delay := time.Duration(1<<uint(tt.attempt)) * orchDefaultBaseBackoff
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
	orchestrator := &DispatchOrchestrator{}

	tests := []struct {
		name       string
		qMsg       domain.QueueMessage
		wantExpire bool
	}{
		{
			name:       "no TTL set",
			qMsg:       domain.QueueMessage{},
			wantExpire: false,
		},
		{
			name: "TTL not expired",
			qMsg: domain.QueueMessage{
				QueuedAt:   time.Now().UTC().Add(-10 * time.Second),
				TTLSeconds: intPtr(300),
			},
			wantExpire: false,
		},
		{
			name: "TTL expired",
			qMsg: domain.QueueMessage{
				QueuedAt:   time.Now().UTC().Add(-600 * time.Second),
				TTLSeconds: intPtr(300),
			},
			wantExpire: true,
		},
		{
			name: "zero TTL ignored",
			qMsg: domain.QueueMessage{
				QueuedAt:   time.Now().UTC(),
				TTLSeconds: intPtr(0),
			},
			wantExpire: false,
		},
		{
			name: "negative TTL ignored",
			qMsg: domain.QueueMessage{
				QueuedAt:   time.Now().UTC(),
				TTLSeconds: intPtr(-1),
			},
			wantExpire: false,
		},
		{
			name: "zero queued_at with TTL",
			qMsg: domain.QueueMessage{
				TTLSeconds: intPtr(60),
			},
			wantExpire: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := orchestrator.isExpired(&tt.qMsg)
			if got != tt.wantExpire {
				t.Errorf("isExpired = %v, want %v", got, tt.wantExpire)
			}
		})
	}
}

func TestDeliveryDedup(t *testing.T) {
	orchestrator := &DispatchOrchestrator{}

	traceID := "test-trace-dedup-001"

	// First call — not in dispatched set
	_, loaded := orchestrator.dispatched.LoadOrStore(traceID, struct{}{})
	if loaded {
		t.Error("first call should NOT be loaded (first occurrence)")
	}

	// Second call — should be in dispatched set
	_, loaded = orchestrator.dispatched.LoadOrStore(traceID, struct{}{})
	if !loaded {
		t.Error("second call SHOULD be loaded (duplicate detected)")
	}

	// Different trace ID — not a duplicate
	_, loaded = orchestrator.dispatched.LoadOrStore("test-trace-dedup-002", struct{}{})
	if loaded {
		t.Error("different trace ID should NOT be loaded")
	}
}

// --- Fake adapters for orchestrator tests ---

// fakeDispatchMsg implements DispatchMessage for tests.
type fakeDispatchMsg struct {
	data     []byte
	headers  map[string]string
	acked    bool
	nacked   bool
	nakDelay time.Duration
}

func (m *fakeDispatchMsg) Data() []byte                         { return m.data }
func (m *fakeDispatchMsg) Headers() map[string]string           { return m.headers }
func (m *fakeDispatchMsg) Ack() error                           { m.acked = true; return nil }
func (m *fakeDispatchMsg) NakWithDelay(d time.Duration) error   { m.nacked = true; m.nakDelay = d; return nil }

type fakeDispatcher struct {
	err         error
	calledCount int
	calledWith  []string
	lastTo      string
}

func (m *fakeDispatcher) Dispatch(ctx context.Context, p *channel.MessagePayload) (string, error) {
	m.calledCount++
	m.calledWith = append(m.calledWith, p.Channel)
	m.lastTo = p.To
	return "", m.err
}

type fakeQueueDepthTracker struct {
	decrements map[uuid.UUID]int
}

func (m *fakeQueueDepthTracker) Decrement(workspaceID uuid.UUID) {
	if m.decrements == nil {
		m.decrements = make(map[uuid.UUID]int)
	}
	m.decrements[workspaceID]++
}

func newTestOrchestrator(dispatchers *channel.Registry, dispatchRepo *repository.MessageDispatchRepository) *DispatchOrchestrator {
	return NewDispatchOrchestrator(dispatchers, dispatchRepo, nil, nil, nil, nil, 5, 60*time.Second)
}

func TestOrchestrator_FallbackLoop(t *testing.T) {
	pool := getTestPool(t)

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)

	ws, err := wsRepo.Create(ctx, "orch_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	t.Run("Terminal error triggers fallback immediately", func(t *testing.T) {
		traceID := uuid.New().String()
		qMsg := &domain.QueueMessage{
			WorkspaceID:      ws.ID,
			TraceID:          traceID,
			To:               "+123",
			Channel:          "whatsapp",
			Body:             "test terminal",
			FallbackChannels: []string{"whatsapp_cloud", "telegram"},
		}

		msg := &fakeDispatchMsg{}

		registry := channel.NewRegistry(nil)
		disp1 := &fakeDispatcher{err: channel.NewTerminalError(errors.New("banned"))}
		disp2 := &fakeDispatcher{err: nil}
		registry.Register("whatsapp", disp1)
		registry.Register("whatsapp_cloud", disp2)

		orchestrator := newTestOrchestrator(registry, dispatchRepo)

		_ = orchestrator.Process(ctx, msg, qMsg, 0)

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
	})

	t.Run("Transient error triggers NAK, does not advance fallback", func(t *testing.T) {
		traceID := uuid.New().String()
		qMsg := &domain.QueueMessage{
			WorkspaceID:      ws.ID,
			TraceID:          traceID,
			To:               "+123",
			Channel:          "whatsapp",
			Body:             "test transient",
			FallbackChannels: []string{"whatsapp_cloud"},
		}

		msg := &fakeDispatchMsg{}

		registry := channel.NewRegistry(nil)
		disp1 := &fakeDispatcher{err: errors.New("network timeout")}
		registry.Register("whatsapp", disp1)

		orchestrator := newTestOrchestrator(registry, dispatchRepo)

		_ = orchestrator.Process(ctx, msg, qMsg, 0)

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
	})

	t.Run("Redelivery of sent message skips dispatch", func(t *testing.T) {
		traceID := uuid.New().String()
		d, err := dispatchRepo.GetOrCreateDispatch(ctx, ws.ID, traceID, "whatsapp", nil, nil, nil)
		if err != nil {
			t.Fatalf("failed to create dispatch: %v", err)
		}
		err = dispatchRepo.UpdateDispatchStatus(ctx, d.ID, "sent", "whatsapp", 0, nil)
		if err != nil {
			t.Fatalf("failed to update status: %v", err)
		}

		qMsg := &domain.QueueMessage{
			WorkspaceID: ws.ID,
			TraceID:     traceID,
			To:          "+123",
			Channel:     "whatsapp",
			Body:        "test redelivery",
		}
		msg := &fakeDispatchMsg{}

		registry := channel.NewRegistry(nil)
		disp1 := &fakeDispatcher{err: nil}
		registry.Register("whatsapp", disp1)

		orchestrator := newTestOrchestrator(registry, dispatchRepo)

		_ = orchestrator.Process(ctx, msg, qMsg, 0)

		if !msg.acked {
			t.Error("expected message to be acked")
		}
		if disp1.calledCount != 0 {
			t.Errorf("expected dispatcher NOT to be called, got %d", disp1.calledCount)
		}
	})

	t.Run("Exhaustion of all fallback channels marks failed", func(t *testing.T) {
		traceID := uuid.New().String()
		qMsg := &domain.QueueMessage{
			WorkspaceID:      ws.ID,
			TraceID:          traceID,
			To:               "+123",
			Channel:          "whatsapp",
			Body:             "test exhaustion",
			FallbackChannels: []string{"telegram"},
		}
		msg := &fakeDispatchMsg{}

		registry := channel.NewRegistry(nil)
		disp1 := &fakeDispatcher{err: channel.NewTerminalError(errors.New("terminal 1"))}
		disp2 := &fakeDispatcher{err: channel.NewTerminalError(errors.New("terminal 2"))}
		registry.Register("whatsapp", disp1)
		registry.Register("telegram", disp2)

		orchestrator := newTestOrchestrator(registry, dispatchRepo)

		_ = orchestrator.Process(ctx, msg, qMsg, 0)

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
	})

	t.Run("TTL expired message is dropped", func(t *testing.T) {
		traceID := uuid.New().String()
		qMsg := &domain.QueueMessage{
			WorkspaceID: ws.ID,
			TraceID:     traceID,
			To:          "+123",
			Channel:     "whatsapp",
			Body:        "test TTL",
			QueuedAt:    time.Now().UTC().Add(-600 * time.Second),
			TTLSeconds:  intPtr(300),
		}
		msg := &fakeDispatchMsg{}

		registry := channel.NewRegistry(nil)
		disp1 := &fakeDispatcher{err: nil}
		registry.Register("whatsapp", disp1)

		orchestrator := newTestOrchestrator(registry, dispatchRepo)

		_ = orchestrator.Process(ctx, msg, qMsg, 0)

		if !msg.acked {
			t.Error("expected expired message to be acked")
		}
		if disp1.calledCount != 0 {
			t.Errorf("expected dispatcher NOT to be called for expired message, got %d", disp1.calledCount)
		}
	})
}

func TestOrchestrator_QueueDepthDecrement(t *testing.T) {
	tracker := &fakeQueueDepthTracker{}
	orchestrator := &DispatchOrchestrator{
		queueDepth: tracker,
	}

	wsID := uuid.New()

	// ack calls Decrement
	msg := &fakeDispatchMsg{}
	orchestrator.ack(msg, wsID)
	if tracker.decrements[wsID] != 1 {
		t.Errorf("expected 1 decrement, got %d", tracker.decrements[wsID])
	}

	// handleFailure (above max retries) calls ack → decrement
	msg2 := &fakeDispatchMsg{}
	orchestrator.maxRetries = 0
	orchestrator.handleFailure(msg2, wsID, "trace-123", 0)
	if tracker.decrements[wsID] != 2 {
		t.Errorf("expected 2 decrements after terminal failure, got %d", tracker.decrements[wsID])
	}
}

func TestOrchestrator_TelegramContactResolution(t *testing.T) {
	pool := getTestPool(t)

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	contactRepo := repository.NewContactRepository(pool)

	ws, err := wsRepo.Create(ctx, "tg_res_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	// Upsert mapping: username "@my_user" -> "chat_98765"
	_, err = contactRepo.ResolveContact(ctx, ws.ID, "telegram", "chat_98765", "My User", "@my_user", "")
	if err != nil {
		t.Fatalf("failed to upsert contact: %v", err)
	}

	// Setup mock dispatcher
	tgDisp := &fakeDispatcher{err: nil}
	registry := channel.NewRegistry(map[string]channel.Dispatcher{
		"telegram": tgDisp,
	})

	orchestrator := NewDispatchOrchestrator(registry, nil, nil, nil, nil, contactRepo, 5, 60*time.Second)

	qMsg := &domain.QueueMessage{
		WorkspaceID: ws.ID,
		TraceID:     uuid.New().String(),
		To:          "@my_user",
		Channel:     "telegram",
		Body:        "hello world",
	}

	msg := &fakeDispatchMsg{}
	err = orchestrator.Process(ctx, msg, qMsg, 0)
	if err != nil {
		t.Fatalf("process failed: %v", err)
	}

	if tgDisp.lastTo != "chat_98765" {
		t.Errorf("expected dispatched To to be 'chat_98765', got %s", tgDisp.lastTo)
	}
}

func intPtr(i int) *int { return &i }

type fakeOrchAuditWriter struct {
	events []audit.Event
}

func (f *fakeOrchAuditWriter) Write(event audit.Event) error {
	f.events = append(f.events, event)
	return nil
}

func (f *fakeOrchAuditWriter) Flush(ctx context.Context) error            { return nil }
func (f *fakeOrchAuditWriter) Close() error                               { return nil }
func (f *fakeOrchAuditWriter) EnsurePartitions(ctx context.Context) error { return nil }

func TestOrchestrator_MockCascading(t *testing.T) {
	ctx := context.Background()
	wsID := uuid.New()

	t.Run("Cascades through terminal errors and records transition audit events", func(t *testing.T) {
		dispWABA := &fakeDispatcher{err: channel.NewTerminalError(errors.New("waba error"))}
		dispWA := &fakeDispatcher{err: channel.NewTerminalError(errors.New("wa error"))}
		dispTG := &fakeDispatcher{err: channel.NewTerminalError(errors.New("tg error"))}
		dispEmail := &fakeDispatcher{err: nil}

		registry := channel.NewRegistry(map[string]channel.Dispatcher{
			"whatsapp_cloud": dispWABA,
			"whatsapp":       dispWA,
			"telegram":       dispTG,
			"email":          dispEmail,
		})

		auditWriter := &fakeOrchAuditWriter{}
		orchestrator := NewDispatchOrchestrator(registry, nil, nil, nil, auditWriter, nil, 5, 60*time.Second)

		traceID := "mock_cascade_trace_" + uuid.New().String()
		qMsg := &domain.QueueMessage{
			WorkspaceID:      wsID,
			TraceID:          traceID,
			To:               "test@example.com",
			Channel:          "whatsapp_cloud",
			FallbackChannels: []string{"whatsapp", "telegram", "email"},
			Body:             "Mock cascading test",
		}

		msg := &fakeDispatchMsg{}
		err := orchestrator.Process(ctx, msg, qMsg, 0)
		if err != nil {
			t.Fatalf("expected successful delivery on email, got %v", err)
		}
		if !msg.acked {
			t.Errorf("expected message to be acked")
		}

		if dispEmail.calledCount != 1 {
			t.Errorf("expected dispEmail to be called once, got %d", dispEmail.calledCount)
		}

		// Verify audit transition events
		var transitions []audit.Event
		for _, ev := range auditWriter.events {
			if ev.TraceID == traceID && ev.EventType == "outbound_fallback_transition" {
				transitions = append(transitions, ev)
			}
		}
		if len(transitions) == 0 {
			t.Errorf("expected outbound_fallback_transition audit events, got 0")
		}
	})
}

func TestOrchestrator_AutomatedCascadingAndIdentityVerification(t *testing.T) {
	pool := getTestPool(t)

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)
	contactRepo := repository.NewContactRepository(pool)

	ws, err := wsRepo.Create(ctx, "cascade_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	t.Run("Four-channel automated cascading WABA -> WhatsApp -> Telegram -> Email", func(t *testing.T) {
		email := "cascade_user@example.com"
		contact, err := contactRepo.CreateContact(ctx, ws.ID, "Cascade User", &email, nil)
		if err != nil {
			t.Fatalf("failed to create contact: %v", err)
		}

		// Link identities for WhatsApp and Telegram
		_, err = pool.Exec(ctx, `
			INSERT INTO contact_identities (contact_id, workspace_id, channel, sender_identity)
			VALUES ($1, $2, 'whatsapp_cloud', '5511999990001'),
			       ($1, $2, 'whatsapp', '5511999990001'),
			       ($1, $2, 'telegram', 'tg_chat_9999')
		`, contact.ID, ws.ID)
		if err != nil {
			t.Fatalf("failed to link identities: %v", err)
		}

		dispWABA := &fakeDispatcher{err: channel.NewTerminalError(errors.New("waba template rejected"))}
		dispWA := &fakeDispatcher{err: channel.NewTerminalError(errors.New("device disconnected"))}
		dispTG := &fakeDispatcher{err: channel.NewTerminalError(errors.New("bot blocked by user"))}
		dispEmail := &fakeDispatcher{err: nil} // Succeeds!

		registry := channel.NewRegistry(map[string]channel.Dispatcher{
			"whatsapp_cloud": dispWABA,
			"whatsapp":       dispWA,
			"telegram":       dispTG,
			"email":          dispEmail,
		})

		auditWriter := &fakeOrchAuditWriter{}
		orchestrator := NewDispatchOrchestrator(registry, dispatchRepo, nil, nil, auditWriter, contactRepo, 5, 60*time.Second)

		traceID := "cascade_trace_" + uuid.New().String()
		qMsg := &domain.QueueMessage{
			WorkspaceID:      ws.ID,
			TraceID:          traceID,
			To:               "5511999990001",
			Channel:          "whatsapp_cloud",
			FallbackChannels: []string{"whatsapp", "telegram", "email"},
			Body:             "Omnichannel fallback notification",
		}

		msg := &fakeDispatchMsg{}
		err = orchestrator.Process(ctx, msg, qMsg, 0)
		if err != nil {
			t.Fatalf("expected successful cascade to email, got error: %v", err)
		}

		if !msg.acked {
			t.Errorf("expected message to be acked upon email delivery")
		}

		// Verify invocation counts and channel order
		if dispWABA.calledCount != 1 {
			t.Errorf("expected dispWABA called once, got %d", dispWABA.calledCount)
		}
		if dispWA.calledCount != 1 {
			t.Errorf("expected dispWA called once, got %d", dispWA.calledCount)
		}
		if dispTG.calledCount != 1 {
			t.Errorf("expected dispTG called once, got %d", dispTG.calledCount)
		}
		if dispTG.lastTo != "tg_chat_9999" {
			t.Errorf("expected dispTG to receive resolved chat ID 'tg_chat_9999', got %s", dispTG.lastTo)
		}
		if dispEmail.calledCount != 1 {
			t.Errorf("expected dispEmail called once, got %d", dispEmail.calledCount)
		}
		if dispEmail.lastTo != "cascade_user@example.com" {
			t.Errorf("expected dispEmail to receive resolved email 'cascade_user@example.com', got %s", dispEmail.lastTo)
		}

		// Verify database dispatch record
		d, err := dispatchRepo.GetByTraceID(ctx, traceID)
		if err != nil {
			t.Fatalf("failed to get dispatch from DB: %v", err)
		}
		if d.Status != "sent" {
			t.Errorf("expected DB status 'sent', got %s", d.Status)
		}
		if d.CurrentChannel != "email" {
			t.Errorf("expected DB current channel 'email', got %s", d.CurrentChannel)
		}
		if d.FallbackIndex != 3 {
			t.Errorf("expected DB fallback index 3, got %d", d.FallbackIndex)
		}

		// Verify audit logs contain trace-correlated fallback transitions
		var transitionsCount int
		for _, ev := range auditWriter.events {
			if ev.TraceID == traceID && ev.EventType == "outbound_fallback_transition" {
				transitionsCount++
			}
		}
		if transitionsCount < 3 {
			t.Errorf("expected at least 3 fallback transition audit events, got %d", transitionsCount)
		}
	})

	t.Run("Skip intermediate channel when recipient lacks identity", func(t *testing.T) {
		email := "skip_tg_user@example.com"
		contact, err := contactRepo.CreateContact(ctx, ws.ID, "Skip TG User", &email, nil)
		if err != nil {
			t.Fatalf("failed to create contact: %v", err)
		}

		// Link ONLY whatsapp identity (NO Telegram identity)
		_, err = pool.Exec(ctx, `
			INSERT INTO contact_identities (contact_id, workspace_id, channel, sender_identity)
			VALUES ($1, $2, 'whatsapp', '5511988880002')
		`, contact.ID, ws.ID)
		if err != nil {
			t.Fatalf("failed to link identities: %v", err)
		}

		dispWA := &fakeDispatcher{err: channel.NewTerminalError(errors.New("user not found on WhatsApp"))}
		dispTG := &fakeDispatcher{err: nil}    // Should NOT be called
		dispEmail := &fakeDispatcher{err: nil} // Succeeds!

		registry := channel.NewRegistry(map[string]channel.Dispatcher{
			"whatsapp": dispWA,
			"telegram": dispTG,
			"email":    dispEmail,
		})

		auditWriter := &fakeOrchAuditWriter{}
		orchestrator := NewDispatchOrchestrator(registry, dispatchRepo, nil, nil, auditWriter, contactRepo, 5, 60*time.Second)

		traceID := "skip_tg_trace_" + uuid.New().String()
		qMsg := &domain.QueueMessage{
			WorkspaceID:      ws.ID,
			TraceID:          traceID,
			To:               "5511988880002",
			Channel:          "whatsapp",
			FallbackChannels: []string{"telegram", "email"},
			Body:             "Test skip without identity",
		}

		msg := &fakeDispatchMsg{}
		err = orchestrator.Process(ctx, msg, qMsg, 0)
		if err != nil {
			t.Fatalf("expected successful delivery on email, got error: %v", err)
		}

		if dispWA.calledCount != 1 {
			t.Errorf("expected dispWA called once, got %d", dispWA.calledCount)
		}
		if dispTG.calledCount != 0 {
			t.Errorf("expected dispTG to NOT be called (skipped due to no identity), got %d", dispTG.calledCount)
		}
		if dispEmail.calledCount != 1 {
			t.Errorf("expected dispEmail called once, got %d", dispEmail.calledCount)
		}
		if dispEmail.lastTo != "skip_tg_user@example.com" {
			t.Errorf("expected dispEmail to receive 'skip_tg_user@example.com', got %s", dispEmail.lastTo)
		}

		// Verify audit logs recorded skipped_no_identity
		hasSkippedAudit := false
		for _, ev := range auditWriter.events {
			if ev.TraceID == traceID && ev.EventType == "outbound_fallback_transition" {
				var payload map[string]any
				if jsonErr := json.Unmarshal(ev.Payload, &payload); jsonErr == nil {
					if payload["status"] == "skipped_no_identity" && payload["channel"] == "telegram" {
						hasSkippedAudit = true
					}
				}
			}
		}
		if !hasSkippedAudit {
			t.Errorf("expected audit event for skipped_no_identity on telegram channel")
		}
	})
}

