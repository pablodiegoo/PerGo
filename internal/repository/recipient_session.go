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
	WorkspaceID        uuid.UUID
	RecipientPhone     string
	Channel            string
	RecipientIdentity  string
	LastInboundAt      time.Time
	LastReadAt         *time.Time
	EntryPointType     string
	NotifiedExpiringAt *time.Time
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
// It also sets entry_point_type (defaulting to "standard") and resets notified_expiring_at to NULL.
func (r *RecipientSessionRepository) Upsert(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string, recipientIdentity string, lastInboundAt time.Time, entryPointType string) error {
	if entryPointType == "" {
		entryPointType = "standard"
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO recipient_sessions (workspace_id, recipient_phone, channel, recipient_identity, last_inbound_at, entry_point_type, notified_expiring_at)
		 VALUES ($1, $2, $3, $4, $5, $6, NULL)
		 ON CONFLICT (workspace_id, recipient_phone, channel, recipient_identity)
		 DO UPDATE SET 
		 	last_inbound_at = EXCLUDED.last_inbound_at,
		 	entry_point_type = EXCLUDED.entry_point_type,
		 	notified_expiring_at = NULL`,
		workspaceID, recipientPhone, channel, recipientIdentity, lastInboundAt, entryPointType,
	)
	return err
}

// Get retrieves a recipient session by workspace ID, recipient phone/ID, channel, and recipient identity.
func (r *RecipientSessionRepository) Get(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string, recipientIdentity string) (*RecipientSession, error) {
	var s RecipientSession
	err := r.pool.QueryRow(ctx,
		`SELECT workspace_id, recipient_phone, channel, recipient_identity, last_inbound_at, last_read_at, entry_point_type, notified_expiring_at
		 FROM recipient_sessions 
		 WHERE workspace_id = $1 AND recipient_phone = $2 AND channel = $3 AND recipient_identity = $4`,
		workspaceID, recipientPhone, channel, recipientIdentity,
	).Scan(&s.WorkspaceID, &s.RecipientPhone, &s.Channel, &s.RecipientIdentity, &s.LastInboundAt, &s.LastReadAt, &s.EntryPointType, &s.NotifiedExpiringAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	return &s, nil
}

// GetExpiringSessions retrieves sessions with last_inbound_at between start and end and notified_expiring_at IS NULL.
func (r *RecipientSessionRepository) GetExpiringSessions(ctx context.Context, start time.Time, end time.Time) ([]RecipientSession, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT workspace_id, recipient_phone, channel, recipient_identity, last_inbound_at, last_read_at, entry_point_type, notified_expiring_at
		 FROM recipient_sessions
		 WHERE last_inbound_at BETWEEN $1 AND $2
		   AND notified_expiring_at IS NULL`,
		start, end,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []RecipientSession
	for rows.Next() {
		var s RecipientSession
		err := rows.Scan(&s.WorkspaceID, &s.RecipientPhone, &s.Channel, &s.RecipientIdentity, &s.LastInboundAt, &s.LastReadAt, &s.EntryPointType, &s.NotifiedExpiringAt)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// MarkNotifiedExpiring sets notified_expiring_at to the given time for a specific recipient session.
func (r *RecipientSessionRepository) MarkNotifiedExpiring(ctx context.Context, workspaceID uuid.UUID, recipientPhone, channel, recipientIdentity string, notifiedAt time.Time) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE recipient_sessions
		 SET notified_expiring_at = $5
		 WHERE workspace_id = $1 AND recipient_phone = $2 AND channel = $3 AND recipient_identity = $4`,
		workspaceID, recipientPhone, channel, recipientIdentity, notifiedAt,
	)
	return err
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

// UpdateLastReadAtByContact updates the last_read_at timestamp for all recipient sessions belonging to a contact.
func (r *RecipientSessionRepository) UpdateLastReadAtByContact(ctx context.Context, workspaceID uuid.UUID, contactID uuid.UUID, lastReadAt time.Time) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE recipient_sessions rs
		 SET last_read_at = $3
		 FROM contact_identities ci
		 WHERE rs.workspace_id = $1
		   AND ci.workspace_id = $1
		   AND ci.contact_id = $2
		   AND rs.recipient_phone = ci.sender_identity
		   AND rs.channel = ci.channel`,
		workspaceID, contactID, lastReadAt,
	)
	return err
}
