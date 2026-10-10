package presence

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPresenceTracker(t *testing.T) {
	tracker := NewTracker(100 * time.Millisecond)
	wsID := uuid.New()
	chatID := "chat-123"

	v1 := Viewer{
		UserID: "user-1",
		Name:   "Alice",
		Email:  "alice@example.com",
		Status: "viewing",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, unsub := tracker.Subscribe(ctx, wsID, chatID)
	defer unsub()

	// 1. Initial heartbeat
	active := tracker.Heartbeat(wsID, chatID, v1)
	if len(active) != 1 {
		t.Fatalf("expected 1 active viewer, got %d", len(active))
	}

	select {
	case event := <-ch:
		if len(event) != 1 || event[0].UserID != "user-1" {
			t.Errorf("unexpected event payload: %+v", event)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for subscription update")
	}

	// 2. Add second viewer (collision scenario)
	v2 := Viewer{
		UserID: "user-2",
		Name:   "Bob",
		Email:  "bob@example.com",
		Status: "typing",
	}
	active2 := tracker.Heartbeat(wsID, chatID, v2)
	if len(active2) != 2 {
		t.Fatalf("expected 2 active viewers (collision), got %d", len(active2))
	}

	// 3. User 1 leaves
	active3 := tracker.Leave(wsID, chatID, "user-1")
	if len(active3) != 1 || active3[0].UserID != "user-2" {
		t.Fatalf("expected only Bob remaining, got %+v", active3)
	}

	// 4. Expiration check after TTL
	time.Sleep(150 * time.Millisecond)
	expired := tracker.GetActiveViewers(wsID, chatID)
	if len(expired) != 0 {
		t.Fatalf("expected 0 active viewers after TTL expiry, got %d", len(expired))
	}
}
