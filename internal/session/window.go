package session

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/repository"
)

// RecipientSessionReader defines the interface for retrieving recipient sessions,
// facilitating unit testing of WindowChecker without a database connection.
type RecipientSessionReader interface {
	Get(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string, recipientIdentity string) (*repository.RecipientSession, error)
}

// WindowStatus represents the detailed status of a recipient's customer service window.
type WindowStatus struct {
	Open           bool          `json:"open"`
	ExpiresAt      time.Time     `json:"expires_at"`
	EntryPointType string        `json:"entry_point_type"`
	WindowDuration time.Duration `json:"window_duration"`
}

// SessionWindowError represents a failed customer service window check.
type SessionWindowError struct {
	Status *WindowStatus
	Source string // "ingestion" or "dispatch"
}

func (e *SessionWindowError) Error() string {
	return "customer service window expired for recipient"
}

// WindowChecker checks if the customer service window (24h standard, 72h CTWA) is open.
type WindowChecker struct {
	repo RecipientSessionReader
}

// NewWindowChecker creates a new WindowChecker.
func NewWindowChecker(repo RecipientSessionReader) *WindowChecker {
	return &WindowChecker{repo: repo}
}

// IsWindowOpen checks if a message can be sent to the recipient on the given channel
// under customer service window rules, applying an optional safetyBuffer.
func (w *WindowChecker) IsWindowOpen(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string, recipientIdentity string, safetyBuffer time.Duration) (*WindowStatus, error) {
	sess, err := w.repo.Get(ctx, workspaceID, recipientPhone, channel, recipientIdentity)
	if err != nil {
		if errors.Is(err, repository.ErrSessionNotFound) {
			return &WindowStatus{Open: false}, nil
		}
		return nil, err
	}

	entryPoint := sess.EntryPointType
	if entryPoint == "" {
		entryPoint = "standard"
	}

	duration := 24 * time.Hour
	if entryPoint == "ctwa" {
		duration = 72 * time.Hour
	}

	expiresAt := sess.LastInboundAt.Add(duration)
	cutoff := expiresAt.Add(-safetyBuffer)
	open := time.Now().UTC().Before(cutoff)

	return &WindowStatus{
		Open:           open,
		ExpiresAt:      expiresAt,
		EntryPointType: entryPoint,
		WindowDuration: duration,
	}, nil
}
