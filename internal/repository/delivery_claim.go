package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DeliveryClaim struct {
	ID               uuid.UUID  `json:"id"`
	WorkspaceID      uuid.UUID  `json:"workspace_id"`
	TraceID          string     `json:"trace_id"`
	ConnectionID     *uuid.UUID `json:"connection_id,omitempty"`
	MessageSubject   string     `json:"message_subject"`
	ClaimedAt        time.Time  `json:"claimed_at"`
	WorkerInstanceID string     `json:"worker_instance_id"`
}

type DeliveryClaimRepository struct {
	pool *pgxpool.Pool
}

func NewDeliveryClaimRepository(pool *pgxpool.Pool) *DeliveryClaimRepository {
	return &DeliveryClaimRepository{pool: pool}
}

// CreateClaim attempts to record an active message delivery claim before worker dispatch.
// Returns inserted=true if claim acquired, inserted=false if claim already exists.
func (r *DeliveryClaimRepository) CreateClaim(ctx context.Context, claim *DeliveryClaim) (bool, error) {
	if claim == nil || claim.WorkspaceID == uuid.Nil {
		return false, ErrInvalidWorkspaceID
	}
	if claim.ID == uuid.Nil {
		claim.ID = uuid.New()
	}
	if claim.ClaimedAt.IsZero() {
		claim.ClaimedAt = time.Now()
	}

	query := `INSERT INTO dispatch_delivery_claim (id, workspace_id, trace_id, connection_id, message_subject, claimed_at, worker_instance_id) VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (workspace_id, trace_id) DO NOTHING`
	res, err := r.pool.Exec(ctx, query, claim.ID, claim.WorkspaceID, claim.TraceID, claim.ConnectionID, claim.MessageSubject, claim.ClaimedAt, claim.WorkerInstanceID)
	if err != nil {
		return false, fmt.Errorf("failed to create delivery claim: %w", err)
	}
	return res.RowsAffected() > 0, nil
}

// ReleaseClaim deletes a delivery claim upon successful message ack or terminal failure.
func (r *DeliveryClaimRepository) ReleaseClaim(ctx context.Context, workspaceID uuid.UUID, traceID string) error {
	if workspaceID == uuid.Nil {
		return ErrInvalidWorkspaceID
	}
	query := `DELETE FROM dispatch_delivery_claim WHERE workspace_id = $1 AND trace_id = $2`
	_, err := r.pool.Exec(ctx, query, workspaceID, traceID)
	return err
}

// RecoverOrphanedClaims finds claims older than the specified duration threshold (e.g. 5 minutes).
func (r *DeliveryClaimRepository) RecoverOrphanedClaims(ctx context.Context, olderThan time.Duration) ([]*DeliveryClaim, error) {
	threshold := time.Now().Add(-olderThan)
	query := `SELECT id, workspace_id, trace_id, connection_id, message_subject, claimed_at, worker_instance_id FROM dispatch_delivery_claim WHERE claimed_at <= $1`

	rows, err := r.pool.Query(ctx, query, threshold)
	if err != nil {
		return nil, fmt.Errorf("failed to query orphaned claims: %w", err)
	}
	defer rows.Close()

	var claims []*DeliveryClaim
	for rows.Next() {
		var c DeliveryClaim
		if err := rows.Scan(&c.ID, &c.WorkspaceID, &c.TraceID, &c.ConnectionID, &c.MessageSubject, &c.ClaimedAt, &c.WorkerInstanceID); err != nil {
			return nil, err
		}
		claims = append(claims, &c)
	}
	return claims, rows.Err()
}
