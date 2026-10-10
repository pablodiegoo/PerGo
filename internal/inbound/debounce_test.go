package inbound

import (
	"context"
	"fmt"
	"os"
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
	events [][]byte
}

func (p *testCapturePublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
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
