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

// ErrChatwootMappingNotFound is returned when a chatwoot mapping cannot be found.
var ErrChatwootMappingNotFound = errors.New("chatwoot mapping not found")

// ChatwootMapping represents a link between local database identities and external Chatwoot details.
type ChatwootMapping struct {
	WorkspaceID            uuid.UUID `json:"workspace_id"`
	ContactID              uuid.UUID `json:"contact_id"`
	ConnectionID           uuid.UUID `json:"connection_id"`
	ChatwootContactID      int64     `json:"chatwoot_contact_id"`
	ChatwootConversationID int64     `json:"chatwoot_conversation_id"`
	Channel                string    `json:"channel"`
	SenderIdentity         string    `json:"sender_identity"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

// ChatwootMappingRepository provides operations for managing Chatwoot contact/conversation mappings.
type ChatwootMappingRepository struct {
	pool *pgxpool.Pool
}

// NewChatwootMappingRepository creates a new ChatwootMappingRepository.
func NewChatwootMappingRepository(pool *pgxpool.Pool) *ChatwootMappingRepository {
	return &ChatwootMappingRepository{pool: pool}
}

// Upsert inserts or updates a Chatwoot mapping profile.
func (r *ChatwootMappingRepository) Upsert(ctx context.Context, m *ChatwootMapping) error {
	query := `
		INSERT INTO chatwoot_mappings (
			workspace_id, contact_id, connection_id, 
			chatwoot_contact_id, chatwoot_conversation_id, channel, sender_identity, 
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
		ON CONFLICT (workspace_id, contact_id, connection_id) DO UPDATE SET
			chatwoot_contact_id = EXCLUDED.chatwoot_contact_id,
			chatwoot_conversation_id = EXCLUDED.chatwoot_conversation_id,
			channel = EXCLUDED.channel,
			sender_identity = EXCLUDED.sender_identity,
			updated_at = NOW()
	`
	_, err := r.pool.Exec(ctx, query,
		m.WorkspaceID, m.ContactID, m.ConnectionID,
		m.ChatwootContactID, m.ChatwootConversationID, m.Channel, m.SenderIdentity,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert chatwoot mapping: %w", err)
	}
	return nil
}

// GetByContactAndConnection retrieves a mapping by workspace ID, contact ID, and connection ID.
func (r *ChatwootMappingRepository) GetByContactAndConnection(ctx context.Context, workspaceID, contactID, connectionID uuid.UUID) (*ChatwootMapping, error) {
	query := `
		SELECT workspace_id, contact_id, connection_id, chatwoot_contact_id, chatwoot_conversation_id, channel, sender_identity, created_at, updated_at
		FROM chatwoot_mappings
		WHERE workspace_id = $1 AND contact_id = $2 AND connection_id = $3
	`
	var m ChatwootMapping
	err := r.pool.QueryRow(ctx, query, workspaceID, contactID, connectionID).Scan(
		&m.WorkspaceID, &m.ContactID, &m.ConnectionID,
		&m.ChatwootContactID, &m.ChatwootConversationID,
		&m.Channel, &m.SenderIdentity,
		&m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrChatwootMappingNotFound
		}
		return nil, err
	}
	return &m, nil
}

// GetByConversationID retrieves a mapping by workspace ID and Chatwoot conversation ID.
func (r *ChatwootMappingRepository) GetByConversationID(ctx context.Context, workspaceID uuid.UUID, chatwootConversationID int64) (*ChatwootMapping, error) {
	query := `
		SELECT workspace_id, contact_id, connection_id, chatwoot_contact_id, chatwoot_conversation_id, channel, sender_identity, created_at, updated_at
		FROM chatwoot_mappings
		WHERE workspace_id = $1 AND chatwoot_conversation_id = $2
	`
	var m ChatwootMapping
	err := r.pool.QueryRow(ctx, query, workspaceID, chatwootConversationID).Scan(
		&m.WorkspaceID, &m.ContactID, &m.ConnectionID,
		&m.ChatwootContactID, &m.ChatwootConversationID,
		&m.Channel, &m.SenderIdentity,
		&m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrChatwootMappingNotFound
		}
		return nil, err
	}
	return &m, nil
}

// Delete removes a Chatwoot mapping.
func (r *ChatwootMappingRepository) Delete(ctx context.Context, workspaceID, contactID, connectionID uuid.UUID) error {
	query := `
		DELETE FROM chatwoot_mappings
		WHERE workspace_id = $1 AND contact_id = $2 AND connection_id = $3
	`
	_, err := r.pool.Exec(ctx, query, workspaceID, contactID, connectionID)
	if err != nil {
		return fmt.Errorf("failed to delete chatwoot mapping: %w", err)
	}
	return nil
}
