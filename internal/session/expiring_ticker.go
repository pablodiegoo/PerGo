package session

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/chat/activity"
)

// Publisher defines the port for publishing events to NATS.
type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte, traceID string) error
}

// ExpiringSessionEvent defines the event payload for session.expiring_soon.
type ExpiringSessionEvent struct {
	Event          string `json:"event"`
	TraceID        string `json:"trace_id"`
	WorkspaceID    string `json:"workspace_id"`
	ChatID         string `json:"chat_id,omitempty"`
	ContactID      string `json:"contact_id,omitempty"`
	ConnectionID   string `json:"connection_id,omitempty"`
	RecipientPhone string `json:"recipient_phone"`
	Channel        string `json:"channel"`
	EntryPointType string `json:"entry_point_type"`
	ExpiresAt      string `json:"expires_at"`
	Timestamp      string `json:"timestamp"`
}

// SessionTicker periodically claims chats approaching service window expiration and emits session.expiring_soon webhook events.
type SessionTicker struct {
	activity  *activity.Recorder
	publisher Publisher
	interval  time.Duration
}

// NewSessionTicker creates a new SessionTicker instance.
func NewSessionTicker(act *activity.Recorder, publisher Publisher) *SessionTicker {
	return &SessionTicker{
		activity:  act,
		publisher: publisher,
		interval:  5 * time.Minute,
	}
}

// SetInterval overrides the tick interval (useful for testing).
func (st *SessionTicker) SetInterval(interval time.Duration) {
	st.interval = interval
}

// Run starts the background ticker loop. It blocks until ctx is cancelled.
func (st *SessionTicker) Run(ctx context.Context) {
	slog.Info("session ticker started", "interval", st.interval)
	ticker := time.NewTicker(st.interval)
	defer ticker.Stop()

	// Perform initial check on startup
	st.checkExpiringSessions(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.Info("session ticker stopping")
			return
		case <-ticker.C:
			st.checkExpiringSessions(ctx)
		}
	}
}

func (st *SessionTicker) checkExpiringSessions(ctx context.Context) {
	st.CheckExpiringSessionsAt(ctx, time.Now().UTC())
}

// CheckExpiringSessionsAt claims chats whose service window expires within 1 hour of now and publishes session.expiring_soon events.
func (st *SessionTicker) CheckExpiringSessionsAt(ctx context.Context, now time.Time) {
	if st == nil || st.activity == nil || st.publisher == nil {
		return
	}

	now = now.UTC()
	claimed, err := st.activity.ClaimExpiring(ctx, now, 1*time.Hour, 50)
	if err != nil {
		slog.Error("session ticker: failed to claim expiring chats", "error", err)
		return
	}

	for _, c := range claimed {
		traceID := uuid.New().String()
		var connIDStr string
		if c.ConnectionID != uuid.Nil {
			connIDStr = c.ConnectionID.String()
		}

		evt := ExpiringSessionEvent{
			Event:          "session.expiring_soon",
			TraceID:        traceID,
			WorkspaceID:    c.WorkspaceID.String(),
			ChatID:         c.ChatID.String(),
			ContactID:      c.ContactID.String(),
			ConnectionID:   connIDStr,
			RecipientPhone: c.RecipientPhone,
			Channel:        c.Channel,
			EntryPointType: string(c.EntryPointType),
			ExpiresAt:      c.ExpiresAt.UTC().Format(time.RFC3339),
			Timestamp:      now.Format(time.RFC3339),
		}

		payload, err := json.Marshal(evt)
		if err != nil {
			slog.Error("session ticker: failed to marshal expiring event", "error", err, "trace_id", traceID)
			continue
		}

		if err := st.publisher.Publish(ctx, "webhooks.events", payload, traceID); err != nil {
			slog.Error("session ticker: failed to publish expiring event", "error", err, "trace_id", traceID)
			continue
		}
	}
}
