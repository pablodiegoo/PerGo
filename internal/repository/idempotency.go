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

var ErrIdempotencyKeyNotFound = errors.New("idempotency key not found")

type IdempotencyEntry struct {
	ID                uuid.UUID 
	WorkspaceID       uuid.UUID 
	KeyHash           string    
	TraceID           string    
	StatusCode        *int      
	ResponseBody      []byte    
	ProviderMessageID *string   
	CreatedAt         time.Time 
	ExpiresAt         time.Time 
}

type IngressLedgerEntry struct {
	ID             uuid.UUID 
	WorkspaceID    uuid.UUID 
	TraceID        string    
	IdempotencyKey string    
	Channel        string    
	Recipient      string    
	Status         string     // "accepted" | "enqueued" | "delivered" | "failed"
	ErrorReason    *string   
	CreatedAt      time.Time 
	UpdatedAt      time.Time 
}

type IdempotencyRepository struct {
	pool *pgxpool.Pool
}

func NewIdempotencyRepository(pool *pgxpool.Pool) *IdempotencyRepository {
	return &IdempotencyRepository{pool: pool}
}

func (r *IdempotencyRepository) CheckAndStore(ctx context.Context, workspaceID uuid.UUID, keyHash, traceID string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	expiresAt := time.Now().Add(ttl)

	query := `INSERT INTO message_idempotency (workspace_id, key_hash, trace_id, expires_at) VALUES (, , , ) ON CONFLICT (workspace_id, key_hash) DO NOTHING`
	res, err := r.pool.Exec(ctx, query, workspaceID, keyHash, traceID, expiresAt)
	if err != nil {
		return false, fmt.Errorf("failed to check and store idempotency key: %w", err)
	}

	return res.RowsAffected() > 0, nil
}

func (r *IdempotencyRepository) GetByIdempotencyKey(ctx context.Context, workspaceID uuid.UUID, keyHash string) (*IdempotencyEntry, error) {
	var entry IdempotencyEntry
	query := `SELECT id, workspace_id, key_hash, trace_id, status_code, response_body, provider_message_id, created_at, expires_at FROM message_idempotency WHERE workspace_id =  AND key_hash =  AND expires_at > NOW()`
	err := r.pool.QueryRow(ctx, query, workspaceID, keyHash).Scan(
		&entry.ID, &entry.WorkspaceID, &entry.KeyHash, &entry.TraceID,
		&entry.StatusCode, &entry.ResponseBody, &entry.ProviderMessageID,
		&entry.CreatedAt, &entry.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrIdempotencyKeyNotFound
		}
		return nil, fmt.Errorf("failed to get idempotency entry: %w", err)
	}
	return &entry, nil
}

func (r *IdempotencyRepository) UpdateResponse(ctx context.Context, workspaceID uuid.UUID, keyHash string, statusCode int, responseBody []byte, providerMsgID *string) error {
	query := `UPDATE message_idempotency SET status_code = , response_body = , provider_message_id =  WHERE workspace_id =  AND key_hash = `
	_, err := r.pool.Exec(ctx, query, workspaceID, keyHash, statusCode, responseBody, providerMsgID)
	return err
}

func (r *IdempotencyRepository) RecordLedger(ctx context.Context, entry *IngressLedgerEntry) error {
	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}
	if entry.Status == "" {
		entry.Status = "accepted"
	}

	query := `INSERT INTO message_ingress_ledger (id, workspace_id, trace_id, idempotency_key, channel, recipient, status, error_reason, created_at, updated_at) VALUES (, , , , , , , , NOW(), NOW())`
	_, err := r.pool.Exec(ctx, query, entry.ID, entry.WorkspaceID, entry.TraceID, entry.IdempotencyKey, entry.Channel, entry.Recipient, entry.Status, entry.ErrorReason)
	return err
}

func (r *IdempotencyRepository) UpdateLedgerStatus(ctx context.Context, workspaceID uuid.UUID, traceID, status string, errReason *string) error {
	query := `UPDATE message_ingress_ledger SET status = , error_reason = , updated_at = NOW() WHERE workspace_id =  AND trace_id = `
	_, err := r.pool.Exec(ctx, query, workspaceID, traceID, status, errReason)
	return err
}

func (r *IdempotencyRepository) CleanupExpired(ctx context.Context) (int64, error) {
	query := `DELETE FROM message_idempotency WHERE expires_at <= NOW()`
	res, err := r.pool.Exec(ctx, query)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected(), nil
}
