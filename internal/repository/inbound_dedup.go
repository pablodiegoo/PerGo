package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InboundDedupRepository handles database-level deduplication for inbound messages.
type InboundDedupRepository struct {
	pool *pgxpool.Pool
}

// NewInboundDedupRepository creates a new InboundDedupRepository.
func NewInboundDedupRepository(pool *pgxpool.Pool) *InboundDedupRepository {
	return &InboundDedupRepository{
		pool: pool,
	}
}

// InsertAndCheck atomically inserts the provider message ID.
// Returns true if the message was successfully inserted (unique), or false if it already existed.
func (r *InboundDedupRepository) InsertAndCheck(ctx context.Context, workspaceID uuid.UUID, channel, providerMessageID string) (bool, error) {
	query := `
		INSERT INTO inbound_dedups (workspace_id, channel, provider_message_id, created_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (workspace_id, channel, provider_message_id) DO NOTHING
	`
	res, err := r.pool.Exec(ctx, query, workspaceID, channel, providerMessageID)
	if err != nil {
		return false, fmt.Errorf("inbound dedup insert: %w", err)
	}

	return res.RowsAffected() == 1, nil
}
