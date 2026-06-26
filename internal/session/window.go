package session

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.omnigo/internal/repository"
)

// RecipientSessionReader defines the interface for retrieving recipient sessions,
// facilitating unit testing of WindowChecker without a database connection.
type RecipientSessionReader interface {
	Get(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string) (*repository.RecipientSession, error)
}

// WindowChecker checks if the 24-hour customer service window is open.
type WindowChecker struct {
	repo RecipientSessionReader
}

// NewWindowChecker creates a new WindowChecker.
func NewWindowChecker(repo RecipientSessionReader) *WindowChecker {
	return &WindowChecker{repo: repo}
}

// IsWindowOpen checks if a message can be sent to the recipient on the given channel
// under the 24-hour customer service window rule.
func (w *WindowChecker) IsWindowOpen(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string) (bool, error) {
	sess, err := w.repo.Get(ctx, workspaceID, recipientPhone, channel)
	if err != nil {
		if err == repository.ErrSessionNotFound {
			return false, nil
		}
		return false, err
	}

	return time.Since(sess.LastInboundAt) <= 24*time.Hour, nil
}
