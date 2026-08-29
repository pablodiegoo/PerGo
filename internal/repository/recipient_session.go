package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pablojhp.pergo/internal/domain"
)

// SessionKey aliases domain.SessionKey for convenient repository usage.
type SessionKey = domain.SessionKey

// ErrSessionNotFound is returned when a recipient session cannot be found.
var ErrSessionNotFound = errors.New("recipient session not found")

// RecipientSession represents a communication session with a recipient on a channel.
type RecipientSession struct {
	SessionKey
	LastInboundAt      time.Time
	LastOutboundAt     *time.Time
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
func (r *RecipientSessionRepository) Upsert(ctx context.Context, key SessionKey, lastInboundAt time.Time, entryPointType string) error {
	if key.WorkspaceID == uuid.Nil {
		return ErrInvalidWorkspaceID
	}
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
		key.WorkspaceID, key.RecipientPhone, key.Channel, key.RecipientIdentity, lastInboundAt, entryPointType,
	)
	return err
}

func normalizeE164Variants(val string) (raw, trimmed, withPlus string) {
	raw = val
	trimmed = strings.TrimPrefix(val, "+")
	withPlus = "+" + trimmed
	return raw, trimmed, withPlus
}

// RecordOutbound updates last_outbound_at for a recipient session, inserting a new session record if none exists.
// It supports E.164 normalization for matching existing sessions.
func (r *RecipientSessionRepository) RecordOutbound(ctx context.Context, key SessionKey, outboundAt time.Time) error {
	if key.WorkspaceID == uuid.Nil {
		return ErrInvalidWorkspaceID
	}
	_, trimmedPhone, withPlusPhone := normalizeE164Variants(key.RecipientPhone)
	_, trimmedIdentity, withPlusIdentity := normalizeE164Variants(key.RecipientIdentity)

	// 1. Try updating matching row first (handling phone variations)
	res, err := r.pool.Exec(ctx,
		`UPDATE recipient_sessions
		 SET last_outbound_at = $5
		 WHERE workspace_id = $1
		   AND (recipient_phone = $2 OR recipient_phone = $3 OR recipient_phone = $4)
		   AND channel = $6
		   AND (recipient_identity = $7 OR recipient_identity = $8 OR recipient_identity = $9 OR recipient_identity = '')`,
		key.WorkspaceID, key.RecipientPhone, trimmedPhone, withPlusPhone, outboundAt, key.Channel, key.RecipientIdentity, trimmedIdentity, withPlusIdentity,
	)
	if err == nil && res.RowsAffected() > 0 {
		return nil
	}

	// 2. Insert new record if no existing row matched
	_, err = r.pool.Exec(ctx,
		`INSERT INTO recipient_sessions (workspace_id, recipient_phone, channel, recipient_identity, last_inbound_at, last_outbound_at, entry_point_type, notified_expiring_at)
		 VALUES ($1, $2, $3, $4, $5, $5, 'standard', NULL)
		 ON CONFLICT (workspace_id, recipient_phone, channel, recipient_identity)
		 DO UPDATE SET last_outbound_at = EXCLUDED.last_outbound_at`,
		key.WorkspaceID, key.RecipientPhone, key.Channel, key.RecipientIdentity, outboundAt,
	)
	return err
}

// Get retrieves a recipient session by SessionKey (workspace ID, recipient phone/ID, channel, and recipient identity).
// It supports E.164 phone normalization (+ prefix) and falls back to matching by workspace, recipient phone, and channel.
func (r *RecipientSessionRepository) Get(ctx context.Context, key SessionKey) (*RecipientSession, error) {
	if key.WorkspaceID == uuid.Nil {
		return nil, ErrInvalidWorkspaceID
	}
	var s RecipientSession
	_, trimmedPhone, withPlusPhone := normalizeE164Variants(key.RecipientPhone)
	_, trimmedIdentity, withPlusIdentity := normalizeE164Variants(key.RecipientIdentity)

	if key.RecipientIdentity != "" {
		err := r.pool.QueryRow(ctx,
			`SELECT workspace_id, recipient_phone, channel, recipient_identity, last_inbound_at, last_outbound_at, last_read_at, entry_point_type, notified_expiring_at
			 FROM recipient_sessions 
			 WHERE workspace_id = $1 
			   AND (recipient_phone = $2 OR recipient_phone = $3 OR recipient_phone = $4) 
			   AND channel = $5 
			   AND (recipient_identity = $6 OR recipient_identity = $7 OR recipient_identity = $8)
			 ORDER BY last_inbound_at DESC LIMIT 1`,
			key.WorkspaceID, key.RecipientPhone, trimmedPhone, withPlusPhone, key.Channel, key.RecipientIdentity, trimmedIdentity, withPlusIdentity,
		).Scan(&s.WorkspaceID, &s.RecipientPhone, &s.Channel, &s.RecipientIdentity, &s.LastInboundAt, &s.LastOutboundAt, &s.LastReadAt, &s.EntryPointType, &s.NotifiedExpiringAt)
		if err == nil {
			return &s, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}

	// Fallback: match by workspace, recipient phone (normalized), and channel
	err := r.pool.QueryRow(ctx,
		`SELECT workspace_id, recipient_phone, channel, recipient_identity, last_inbound_at, last_outbound_at, last_read_at, entry_point_type, notified_expiring_at
		 FROM recipient_sessions 
		 WHERE workspace_id = $1 
		   AND (recipient_phone = $2 OR recipient_phone = $3 OR recipient_phone = $4) 
		   AND channel = $5
		 ORDER BY last_inbound_at DESC LIMIT 1`,
		key.WorkspaceID, key.RecipientPhone, trimmedPhone, withPlusPhone, key.Channel,
	).Scan(&s.WorkspaceID, &s.RecipientPhone, &s.Channel, &s.RecipientIdentity, &s.LastInboundAt, &s.LastOutboundAt, &s.LastReadAt, &s.EntryPointType, &s.NotifiedExpiringAt)
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
		`SELECT workspace_id, recipient_phone, channel, recipient_identity, last_inbound_at, last_outbound_at, last_read_at, entry_point_type, notified_expiring_at
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
		err := rows.Scan(&s.WorkspaceID, &s.RecipientPhone, &s.Channel, &s.RecipientIdentity, &s.LastInboundAt, &s.LastOutboundAt, &s.LastReadAt, &s.EntryPointType, &s.NotifiedExpiringAt)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// MarkNotifiedExpiring sets notified_expiring_at to the given time for a specific recipient session.
func (r *RecipientSessionRepository) MarkNotifiedExpiring(ctx context.Context, key SessionKey, notifiedAt time.Time) error {
	if key.WorkspaceID == uuid.Nil {
		return ErrInvalidWorkspaceID
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE recipient_sessions
		 SET notified_expiring_at = $5
		 WHERE workspace_id = $1 AND recipient_phone = $2 AND channel = $3 AND recipient_identity = $4`,
		key.WorkspaceID, key.RecipientPhone, key.Channel, key.RecipientIdentity, notifiedAt,
	)
	return err
}

// UpdateLastReadAt updates the last_read_at timestamp for a specific recipient session.
func (r *RecipientSessionRepository) UpdateLastReadAt(ctx context.Context, key SessionKey, lastReadAt time.Time) error {
	if key.WorkspaceID == uuid.Nil {
		return ErrInvalidWorkspaceID
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE recipient_sessions
		 SET last_read_at = $5
		 WHERE workspace_id = $1 AND recipient_phone = $2 AND channel = $3 AND recipient_identity = $4`,
		key.WorkspaceID, key.RecipientPhone, key.Channel, key.RecipientIdentity, lastReadAt,
	)
	return err
}

// UpdateLastReadAtByContact updates the last_read_at timestamp for all recipient sessions belonging to a contact.
func (r *RecipientSessionRepository) UpdateLastReadAtByContact(ctx context.Context, workspaceID uuid.UUID, contactID uuid.UUID, lastReadAt time.Time) error {
	if workspaceID == uuid.Nil {
		return ErrInvalidWorkspaceID
	}
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
