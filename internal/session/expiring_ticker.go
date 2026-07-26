package session

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/repository"
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
	RecipientPhone string `json:"recipient_phone"`
	Channel        string `json:"channel"`
	EntryPointType string `json:"entry_point_type"`
	ExpiresAt      string `json:"expires_at"`
	Timestamp      string `json:"timestamp"`
}

// SessionTicker periodically queries for sessions approaching window expiration and emits session.expiring_soon webhook events.
type SessionTicker struct {
	repo      *repository.RecipientSessionRepository
	publisher Publisher
	interval  time.Duration
}

// NewSessionTicker creates a new SessionTicker instance.
func NewSessionTicker(repo *repository.RecipientSessionRepository, publisher Publisher) *SessionTicker {
	return &SessionTicker{
		repo:      repo,
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
	if st.repo == nil || st.publisher == nil {
		return
	}

	now := time.Now().UTC()

	// Check standard (24h) sessions that are at 23h (23h to 24h window)
	standardStart := now.Add(-24 * time.Hour)
	standardEnd := now.Add(-23 * time.Hour)

	// Check CTWA (72h) sessions that are at 71h (71h to 72h window)
	ctwaStart := now.Add(-72 * time.Hour)
	ctwaEnd := now.Add(-71 * time.Hour)

	// Query standard expiring sessions
	st.processRange(ctx, standardStart, standardEnd, "standard", 24*time.Hour)

	// Query CTWA expiring sessions
	st.processRange(ctx, ctwaStart, ctwaEnd, "ctwa", 72*time.Hour)
}

func (st *SessionTicker) processRange(ctx context.Context, start, end time.Time, expectedType string, duration time.Duration) {
	sessions, err := st.repo.GetExpiringSessions(ctx, start, end)
	if err != nil {
		slog.Error("session ticker: failed to query expiring sessions", "error", err)
		return
	}

	for _, sess := range sessions {
		entryPoint := sess.EntryPointType
		if entryPoint == "" {
			entryPoint = "standard"
		}
		if entryPoint != expectedType {
			continue
		}

		expiresAt := sess.LastInboundAt.Add(duration)
		traceID := uuid.New().String()

		evt := ExpiringSessionEvent{
			Event:          "session.expiring_soon",
			TraceID:        traceID,
			WorkspaceID:    sess.WorkspaceID.String(),
			RecipientPhone: sess.RecipientPhone,
			Channel:        sess.Channel,
			EntryPointType: entryPoint,
			ExpiresAt:      expiresAt.Format(time.RFC3339),
			Timestamp:      time.Now().UTC().Format(time.RFC3339),
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

		if err := st.repo.MarkNotifiedExpiring(ctx, sess.WorkspaceID, sess.RecipientPhone, sess.Channel, sess.RecipientIdentity, time.Now().UTC()); err != nil {
			slog.Error("session ticker: failed to mark notified expiring", "error", err, "recipient", sess.RecipientPhone)
		}
	}
}
