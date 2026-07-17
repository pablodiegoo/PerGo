package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrTypebotSessionNotFound = errors.New("typebot session not found")

// TypebotSession represents an active session between a contact and a bot on a specific connection.
type TypebotSession struct {
	WorkspaceID      uuid.UUID `json:"workspace_id"`
	ContactID        uuid.UUID `json:"contact_id"`
	ConnectionID     uuid.UUID `json:"connection_id"`
	TypebotSessionID string    `json:"typebot_session_id"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// TypebotSessionRepository manages CRUD operations for active Typebot sessions.
type TypebotSessionRepository struct {
	pool *pgxpool.Pool
}

func NewTypebotSessionRepository(pool *pgxpool.Pool) *TypebotSessionRepository {
	return &TypebotSessionRepository{pool: pool}
}

func (r *TypebotSessionRepository) GetSession(ctx context.Context, workspaceID, contactID, connectionID uuid.UUID) (*TypebotSession, error) {
	query := `
		SELECT workspace_id, contact_id, connection_id, typebot_session_id, created_at, updated_at
		FROM typebot_sessions
		WHERE workspace_id = $1 AND contact_id = $2 AND connection_id = $3
	`
	row := r.pool.QueryRow(ctx, query, workspaceID, contactID, connectionID)

	var s TypebotSession
	err := row.Scan(&s.WorkspaceID, &s.ContactID, &s.ConnectionID, &s.TypebotSessionID, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTypebotSessionNotFound
		}
		return nil, fmt.Errorf("failed to get typebot session: %w", err)
	}

	return &s, nil
}

func (r *TypebotSessionRepository) UpsertSession(ctx context.Context, s *TypebotSession) error {
	query := `
		INSERT INTO typebot_sessions (
			workspace_id, contact_id, connection_id, typebot_session_id, created_at, updated_at
		) VALUES ($1, $2, $3, $4, NOW(), NOW())
		ON CONFLICT (workspace_id, contact_id, connection_id) DO UPDATE SET
			typebot_session_id = EXCLUDED.typebot_session_id,
			updated_at = NOW()
	`

	_, err := r.pool.Exec(ctx, query, s.WorkspaceID, s.ContactID, s.ConnectionID, s.TypebotSessionID)
	if err != nil {
		return fmt.Errorf("failed to upsert typebot session: %w", err)
	}
	return nil
}

func (r *TypebotSessionRepository) DeleteSession(ctx context.Context, workspaceID, contactID, connectionID uuid.UUID) error {
	query := `
		DELETE FROM typebot_sessions
		WHERE workspace_id = $1 AND contact_id = $2 AND connection_id = $3
	`
	_, err := r.pool.Exec(ctx, query, workspaceID, contactID, connectionID)
	if err != nil {
		return fmt.Errorf("failed to delete typebot session: %w", err)
	}
	return nil
}
