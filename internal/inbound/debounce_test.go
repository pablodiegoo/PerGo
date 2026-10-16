package inbound

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func getTestRedis(t *testing.T) redis.Cmdable {
	redisAddr := os.Getenv("PERGO_TEST_REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "127.0.0.1:6379"
	}
	rdb := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("skipping: Redis not available at %s: %v", redisAddr, err)
	}
	return rdb
}

type testCapturePublisher struct {
	mu       sync.Mutex
	subjects []string
	events   [][]byte
}

func (p *testCapturePublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.subjects = append(p.subjects, subject)
	p.events = append(p.events, data)
	return nil
}

func TestDebouncer_RapidMessagesCoalesce(t *testing.T) {
	rdb := getTestRedis(t)
	ctx := context.Background()

	wsID := uuid.New()
	contactID := uuid.New()
	chatID := uuid.New()

	// Clear any old keys
	rdb.Del(ctx, fmt.Sprintf("debounce:%s:%s", wsID, contactID))
	rdb.Del(ctx, fmt.Sprintf("debounce:timer:%s:%s", wsID, contactID))

	coalescedChan := make(chan *CoalescedTurnEvent, 5)
	pub := &testCapturePublisher{}

	deb := NewDebouncer(
		rdb,
		WithDebounceTTL(150*time.Millisecond),
		WithDebouncePublisher(pub),
		WithDebounceHandler(func(ctx context.Context, ev *CoalescedTurnEvent) error {
			coalescedChan <- ev
			return nil
		}),
	)
	defer deb.Close()

	// Enqueue 3 rapid messages
	now := time.Now().UTC()
	m1 := &DebouncedMessage{
		MessageID:   "m-1",
		TraceID:     "t-1",
		WorkspaceID: wsID,
		ContactID:   contactID,
		ChatID:      chatID,
		Channel:     "whatsapp",
		From:        "+5511999990001",
		To:          "+5511000000000",
		SenderName:  "Peter Parker",
		Body:        "Hey there!",
		OccurredAt:  now,
	}
	m2 := &DebouncedMessage{
		MessageID:   "m-2",
		TraceID:     "t-2",
		WorkspaceID: wsID,
		ContactID:   contactID,
		ChatID:      chatID,
		Channel:     "whatsapp",
		From:        "+5511999990001",
		To:          "+5511000000000",
		SenderName:  "Peter Parker",
		Body:        "Do you have a minute?",
		OccurredAt:  now.Add(20 * time.Millisecond),
	}
	m3 := &DebouncedMessage{
		MessageID:   "m-3",
		TraceID:     "t-3",
		WorkspaceID: wsID,
		ContactID:   contactID,
		ChatID:      chatID,
		Channel:     "whatsapp",
		From:        "+5511999990001",
		To:          "+5511000000000",
		SenderName:  "Peter Parker",
		Body:        "I need help with my suit.",
		OccurredAt:  now.Add(40 * time.Millisecond),
	}

	if err := deb.Enqueue(ctx, m1); err != nil {
		t.Fatalf("enqueue m1: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	if err := deb.Enqueue(ctx, m2); err != nil {
		t.Fatalf("enqueue m2: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	if err := deb.Enqueue(ctx, m3); err != nil {
		t.Fatalf("enqueue m3: %v", err)
	}

	// Wait for window to expire
	select {
	case ev := <-coalescedChan:
		if ev.MessageCount != 3 {
			t.Errorf("expected 3 coalesced messages, got %d", ev.MessageCount)
		}
		expectedBody := "Hey there!\n\nDo you have a minute?\n\nI need help with my suit."
		if ev.Body != expectedBody {
			t.Errorf("expected body %q, got %q", expectedBody, ev.Body)
		}
		if len(ev.MessageIDs) != 3 || ev.MessageIDs[0] != "m-1" || ev.MessageIDs[2] != "m-3" {
			t.Errorf("unexpected message IDs: %+v", ev.MessageIDs)
		}
		if ev.SenderName != "Peter Parker" {
			t.Errorf("expected sender name Peter Parker, got %s", ev.SenderName)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for debouncer coalesced event")
	}

	// Verify no duplicate events emitted
	time.Sleep(200 * time.Millisecond)
	if len(coalescedChan) != 0 {
		t.Errorf("unexpected extra coalesced events in channel: %d", len(coalescedChan))
	}
}

func TestDebouncer_ManualFlush(t *testing.T) {
	rdb := getTestRedis(t)
	ctx := context.Background()

	wsID := uuid.New()
	contactID := uuid.New()
	chatID := uuid.New()

	rdb.Del(ctx, fmt.Sprintf("debounce:%s:%s", wsID, contactID))
	rdb.Del(ctx, fmt.Sprintf("debounce:timer:%s:%s", wsID, contactID))

	deb := NewDebouncer(rdb, WithDebounceTTL(10*time.Second))
	defer deb.Close()

	msg := &DebouncedMessage{
		MessageID:   "m-manual",
		TraceID:     "t-manual",
		WorkspaceID: wsID,
		ContactID:   contactID,
		ChatID:      chatID,
		Channel:     "telegram",
		From:        "123456",
		Body:        "Instant query",
		OccurredAt:  time.Now().UTC(),
	}

	if err := deb.Enqueue(ctx, msg); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	ev, err := deb.Flush(ctx, wsID, contactID)
	if err != nil {
		t.Fatalf("manual flush failed: %v", err)
	}
	if ev == nil {
		t.Fatal("expected non-nil coalesced event")
	}
	if ev.Body != "Instant query" {
		t.Errorf("expected body 'Instant query', got %s", ev.Body)
	}

	// Second flush returns nil (idempotent)
	ev2, err := deb.Flush(ctx, wsID, contactID)
	if err != nil {
		t.Fatalf("second flush error: %v", err)
	}
	if ev2 != nil {
		t.Errorf("expected nil on second flush, got %+v", ev2)
	}
}

func TestDebouncer_SetPublisherAndProcessorAutoBinding(t *testing.T) {
	rdb := getTestRedis(t)
	ctx := context.Background()

	wsID := uuid.New()
	contactID := uuid.New()
	chatID := uuid.New()

	rdb.Del(ctx, fmt.Sprintf("debounce:%s:%s", wsID, contactID))
	rdb.Del(ctx, fmt.Sprintf("debounce:timer:%s:%s", wsID, contactID))

	procPub := &testCapturePublisher{}
	proc := NewInboundProcessor(nil, nil, nil, procPub, nil, nil, nil, nil, nil)

	// Create Debouncer WITHOUT WithDebouncePublisher
	deb := NewDebouncer(rdb, WithDebounceTTL(80*time.Millisecond))
	defer deb.Close()

	// Attaching to InboundProcessor should automatically bind procPub onto deb
	proc.SetDebouncer(deb)

	msg := &DebouncedMessage{
		MessageID:   "m-autobind",
		TraceID:     "t-autobind",
		WorkspaceID: wsID,
		ContactID:   contactID,
		ChatID:      chatID,
		Channel:     "whatsapp",
		From:        "+5511999990001",
		To:          "+5511000000000",
		SenderName:  "Mary Jane",
		Body:        "Checking publisher auto-binding",
		OccurredAt:  time.Now().UTC(),
	}

	if err := deb.Enqueue(ctx, msg); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	// Wait for debounce window to flush automatically
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		procPub.mu.Lock()
		n := len(procPub.subjects)
		procPub.mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	procPub.mu.Lock()
	defer procPub.mu.Unlock()
	if len(procPub.subjects) != 2 {
		t.Fatalf("expected 2 published subjects on flush (global and workspace-scoped), got %d: %v", len(procPub.subjects), procPub.subjects)
	}
	if procPub.subjects[0] != EventChatInboundCoalesced {
		t.Errorf("expected first subject %q, got %q", EventChatInboundCoalesced, procPub.subjects[0])
	}
	expectedWsSubject := fmt.Sprintf("%s.%s", EventChatInboundCoalesced, wsID.String())
	if procPub.subjects[1] != expectedWsSubject {
		t.Errorf("expected second subject %q, got %q", expectedWsSubject, procPub.subjects[1])
	}

	// Also verify explicit SetPublisher works and SetDebouncer does not overwrite an already-configured publisher
	explicitPub := &testCapturePublisher{}
	debExplicit := NewDebouncer(rdb, WithDebounceTTL(10*time.Second))
	defer debExplicit.Close()
	debExplicit.SetPublisher(explicitPub)
	proc.SetDebouncer(debExplicit)

	wsID2 := uuid.New()
	contactID2 := uuid.New()
	if err := debExplicit.Enqueue(ctx, &DebouncedMessage{
		MessageID:   "m-explicit",
		TraceID:     "t-explicit",
		WorkspaceID: wsID2,
		ContactID:   contactID2,
		ChatID:      chatID,
		Channel:     "whatsapp",
		From:        "+5511999990002",
		To:          "+5511000000000",
		Body:        "Explicit publisher test",
		OccurredAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("enqueue explicit failed: %v", err)
	}
	if _, err := debExplicit.Flush(ctx, wsID2, contactID2); err != nil {
		t.Fatalf("flush explicit failed: %v", err)
	}
	explicitPub.mu.Lock()
	defer explicitPub.mu.Unlock()
	if len(explicitPub.subjects) != 2 {
		t.Fatalf("expected explicit publisher to receive 2 events, got %d", len(explicitPub.subjects))
	}
}

func TestDebouncer_Cancel_ClearsBufferAndStopsTimer(t *testing.T) {
	rdb := getTestRedis(t)
	ctx := context.Background()

	wsID := uuid.New()
	contactID := uuid.New()
	chatID := uuid.New()

	bufferKey := fmt.Sprintf("debounce:%s:%s", wsID, contactID)
	msgsKey := fmt.Sprintf("debounce:msgs:%s:%s", wsID, contactID)
	timerKey := fmt.Sprintf("debounce:timer:%s:%s", wsID, contactID)

	rdb.Del(ctx, bufferKey, msgsKey, timerKey)

	coalescedChan := make(chan *CoalescedTurnEvent, 5)
	pub := &testCapturePublisher{}

	deb := NewDebouncer(
		rdb,
		WithDebounceTTL(120*time.Millisecond),
		WithDebouncePublisher(pub),
		WithDebounceHandler(func(ctx context.Context, ev *CoalescedTurnEvent) error {
			coalescedChan <- ev
			return nil
		}),
	)
	defer deb.Close()

	now := time.Now().UTC()
	for i := 1; i <= 2; i++ {
		if err := deb.Enqueue(ctx, &DebouncedMessage{
			MessageID:   fmt.Sprintf("m-cancel-%d", i),
			TraceID:     fmt.Sprintf("t-cancel-%d", i),
			WorkspaceID: wsID,
			ContactID:   contactID,
			ChatID:      chatID,
			Channel:     "whatsapp",
			From:        "+5511999990001",
			To:          "+5511000000000",
			Body:        fmt.Sprintf("Message before handoff %d", i),
			OccurredAt:  now.Add(time.Duration(i*10) * time.Millisecond),
		}); err != nil {
			t.Fatalf("enqueue %d failed: %v", i, err)
		}
	}
	// Also populate debounce:msgs:<ws>:<contact> to verify Cancel clears both buffer naming conventions
	_ = rdb.RPush(ctx, msgsKey, "legacy").Err()

	// Verify keys exist in Redis prior to Cancel
	if n, err := rdb.LLen(ctx, bufferKey).Result(); err != nil || n != 2 {
		t.Fatalf("expected 2 buffered messages in Redis before Cancel, got %d (err=%v)", n, err)
	}
	if exists, err := rdb.Exists(ctx, timerKey).Result(); err != nil || exists != 1 {
		t.Fatalf("expected timer key to exist in Redis before Cancel, got %d (err=%v)", exists, err)
	}

	// Cancel mid-window (simulating Human Handoff Trigger)
	if err := deb.Cancel(ctx, wsID, contactID); err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}

	// Verify Redis keys are deleted immediately
	if exists, err := rdb.Exists(ctx, bufferKey, msgsKey, timerKey).Result(); err != nil || exists != 0 {
		t.Fatalf("expected all Redis debounce keys to be deleted on Cancel, got exists=%d (err=%v)", exists, err)
	}

	// Wait past the original 120ms debounce window
	time.Sleep(220 * time.Millisecond)

	if len(coalescedChan) != 0 {
		t.Fatalf("expected 0 coalesced events after Cancel, got %d", len(coalescedChan))
	}
	pub.mu.Lock()
	pubCount := len(pub.subjects)
	pub.mu.Unlock()
	if pubCount != 0 {
		t.Fatalf("expected 0 published events after Cancel, got %d", pubCount)
	}

	ev, err := deb.Flush(ctx, wsID, contactID)
	if err != nil {
		t.Fatalf("Flush after Cancel failed: %v", err)
	}
	if ev != nil {
		t.Fatalf("expected nil event on Flush after Cancel, got %+v", ev)
	}
}


