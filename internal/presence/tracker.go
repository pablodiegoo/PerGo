package presence

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Viewer represents an active team member viewing or typing in a chat.
type Viewer struct {
	UserID   string    `json:"user_id"`
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Status   string    `json:"status"` // "viewing", "typing"
	LastSeen time.Time `json:"last_seen"`
}

// Tracker provides thread-safe real-time presence and collision tracking.
type Tracker struct {
	mu          sync.RWMutex
	viewers     map[string]map[string]*Viewer
	subscribers map[string]map[chan []Viewer]struct{}
	ttl         time.Duration
}

// NewTracker creates a presence tracker with the given heartbeat timeout TTL.
func NewTracker(ttl time.Duration) *Tracker {
	if ttl <= 0 {
		ttl = 15 * time.Second
	}
	return &Tracker{
		viewers:     make(map[string]map[string]*Viewer),
		subscribers: make(map[string]map[chan []Viewer]struct{}),
		ttl:         ttl,
	}
}

func scopeKey(wsID uuid.UUID, chatID string) string {
	return fmt.Sprintf("%s:%s", wsID.String(), chatID)
}

// Heartbeat records or refreshes a viewer's presence for a chat thread.
func (t *Tracker) Heartbeat(wsID uuid.UUID, chatID string, v Viewer) []Viewer {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := scopeKey(wsID, chatID)
	if t.viewers[key] == nil {
		t.viewers[key] = make(map[string]*Viewer)
	}

	if v.Status == "" {
		v.Status = "viewing"
	}
	v.LastSeen = time.Now().UTC()
	t.viewers[key][v.UserID] = &v

	// Clean up stale entries
	now := time.Now().UTC()
	for uid, entry := range t.viewers[key] {
		if now.Sub(entry.LastSeen) > t.ttl {
			delete(t.viewers[key], uid)
		}
	}

	active := t.buildActiveList(key)
	t.notifySubscribers(key, active)
	return active
}

// Leave removes a viewer from a chat presence list.
func (t *Tracker) Leave(wsID uuid.UUID, chatID, userID string) []Viewer {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := scopeKey(wsID, chatID)
	if t.viewers[key] != nil {
		delete(t.viewers[key], userID)
	}

	active := t.buildActiveList(key)
	t.notifySubscribers(key, active)
	return active
}

// GetActiveViewers returns the current non-expired viewers for a chat.
func (t *Tracker) GetActiveViewers(wsID uuid.UUID, chatID string) []Viewer {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := scopeKey(wsID, chatID)
	now := time.Now().UTC()
	if t.viewers[key] != nil {
		for uid, entry := range t.viewers[key] {
			if now.Sub(entry.LastSeen) > t.ttl {
				delete(t.viewers[key], uid)
			}
		}
	}

	return t.buildActiveList(key)
}

func (t *Tracker) buildActiveList(key string) []Viewer {
	room := t.viewers[key]
	list := make([]Viewer, 0, len(room))
	for _, v := range room {
		list = append(list, *v)
	}
	return list
}

func (t *Tracker) notifySubscribers(key string, viewers []Viewer) {
	subs := t.subscribers[key]
	for ch := range subs {
		select {
		case ch <- viewers:
		default:
			// avoid blocking slow listeners
		}
	}
}

// Subscribe listens to presence updates for a given workspace chat.
func (t *Tracker) Subscribe(ctx context.Context, wsID uuid.UUID, chatID string) (<-chan []Viewer, func()) {
	t.mu.Lock()
	key := scopeKey(wsID, chatID)
	if t.subscribers[key] == nil {
		t.subscribers[key] = make(map[chan []Viewer]struct{})
	}
	ch := make(chan []Viewer, 16)
	t.subscribers[key][ch] = struct{}{}
	t.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			t.mu.Lock()
			defer t.mu.Unlock()
			if subs, ok := t.subscribers[key]; ok {
				delete(subs, ch)
				if len(subs) == 0 {
					delete(t.subscribers, key)
				}
			}
			close(ch)
		})
	}

	go func() {
		<-ctx.Done()
		cancel()
	}()

	return ch, cancel
}
