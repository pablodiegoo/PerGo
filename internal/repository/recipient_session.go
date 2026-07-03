package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)


// ErrSessionNotFound is returned when a recipient session cannot be found.
var ErrSessionNotFound = errors.New("recipient session not found")

// RecipientSession represents a communication session with a recipient on a channel.
type RecipientSession struct {
	WorkspaceID       uuid.UUID
	RecipientPhone    string
	Channel           string
	RecipientIdentity string
	LastInboundAt     time.Time
	LastReadAt        *time.Time
}

// RecipientSessionRepository provides operations for managing recipient sessions.
type RecipientSessionRepository struct {
	pool *pgxpool.Pool
}

// NewRecipientSessionRepository creates a new RecipientSessionRepository.
func NewRecipientSessionRepository(pool *pgxpool.Pool) *RecipientSessionRepository {
	return &RecipientSessionRepository{pool: pool}
}

// Upsert inserts or updates a recipient session setting last_inbound_at to the given/current time.
func (r *RecipientSessionRepository) Upsert(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string, recipientIdentity string, lastInboundAt time.Time) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO recipient_sessions (workspace_id, recipient_phone, channel, recipient_identity, last_inbound_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (workspace_id, recipient_phone, channel, recipient_identity)
		 DO UPDATE SET last_inbound_at = EXCLUDED.last_inbound_at`,
		workspaceID, recipientPhone, channel, recipientIdentity, lastInboundAt,
	)
	return err
}

// Get retrieves a recipient session by workspace ID, recipient phone/ID, channel, and recipient identity.
func (r *RecipientSessionRepository) Get(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string, recipientIdentity string) (*RecipientSession, error) {
	var s RecipientSession
	err := r.pool.QueryRow(ctx,
		`SELECT workspace_id, recipient_phone, channel, recipient_identity, last_inbound_at, last_read_at
		 FROM recipient_sessions 
		 WHERE workspace_id = $1 AND recipient_phone = $2 AND channel = $3 AND recipient_identity = $4`,
		workspaceID, recipientPhone, channel, recipientIdentity,
	).Scan(&s.WorkspaceID, &s.RecipientPhone, &s.Channel, &s.RecipientIdentity, &s.LastInboundAt, &s.LastReadAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	return &s, nil
}

// UpdateLastReadAt updates the last_read_at timestamp for a specific recipient session.
func (r *RecipientSessionRepository) UpdateLastReadAt(ctx context.Context, workspaceID uuid.UUID, recipientPhone, channel, recipientIdentity string, lastReadAt time.Time) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE recipient_sessions
		 SET last_read_at = $5
		 WHERE workspace_id = $1 AND recipient_phone = $2 AND channel = $3 AND recipient_identity = $4`,
		workspaceID, recipientPhone, channel, recipientIdentity, lastReadAt,
	)
	return err
}
