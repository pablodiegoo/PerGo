package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pablojhp.pergo/internal/platform/crypto"
)

var (
	ErrWebhookConfigNotFound = errors.New("webhook config not found")
	ErrWebhookDLQNotFound    = errors.New("webhook DLQ item not found")
)

// WebhookConfig represents a workspace's webhook configuration.
type WebhookConfig struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	URL         string
	Secret      []byte // Plaintext secret
	KeyID       string
	KeyVersion  int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// WebhookDLQ represents a dead-lettered webhook payload.
type WebhookDLQ struct {
	ID            uuid.UUID
	WorkspaceID   uuid.UUID
	TraceID       string
	MessageID     string
	EventType     string
	Payload       []byte // JSON payload
	WebhookURL    string
	LastAttemptAt time.Time
	FailureReason *string
	Attempts      int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type WebhookDLQRepository struct {
	pool      *pgxpool.Pool
	encryptor *crypto.Encryptor
}

func NewWebhookDLQRepository(pool *pgxpool.Pool, encryptor *crypto.Encryptor) *WebhookDLQRepository {
	return &WebhookDLQRepository{
		pool:      pool,
		encryptor: encryptor,
	}
}

// SaveConfig saves or updates the webhook configuration for a workspace.
func (r *WebhookDLQRepository) SaveConfig(ctx context.Context, workspaceID uuid.UUID, url string, secretPlaintext []byte) error {
	if url == "" {
		return errors.New("webhook URL cannot be empty")
	}
	if len(secretPlaintext) == 0 {
		return errors.New("webhook secret cannot be empty")
	}

	ciphertext, keyID, keyVersion, err := r.encryptor.Encrypt(secretPlaintext)
	if err != nil {
		return err
	}

	_, err = r.pool.Exec(ctx,
		`INSERT INTO webhook_configs (workspace_id, url, secret, key_id, key_version, updated_at)
		 VALUES ($1, $2, $3, $4, $5, now())
		 ON CONFLICT (workspace_id)
		 DO UPDATE SET url = EXCLUDED.url, secret = EXCLUDED.secret, key_id = EXCLUDED.key_id, key_version = EXCLUDED.key_version, updated_at = now()`,
		workspaceID, url, ciphertext, keyID, keyVersion,
	)
	return err
}

// GetConfig retrieves the webhook configuration for a workspace.
func (r *WebhookDLQRepository) GetConfig(ctx context.Context, workspaceID uuid.UUID) (*WebhookConfig, error) {
	var c WebhookConfig
	var ciphertext []byte

	err := r.pool.QueryRow(ctx,
		`SELECT id, workspace_id, url, secret, key_id, key_version, created_at, updated_at 
		 FROM webhook_configs WHERE workspace_id = $1`,
		workspaceID,
	).Scan(&c.ID, &c.WorkspaceID, &c.URL, &ciphertext, &c.KeyID, &c.KeyVersion, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWebhookConfigNotFound
		}
		return nil, err
	}

	secret, err := r.encryptor.Decrypt(ciphertext)
	if err != nil {
		return nil, err
	}
	c.Secret = secret

	return &c, nil
}

// DeleteConfig removes the webhook configuration for a workspace.
func (r *WebhookDLQRepository) DeleteConfig(ctx context.Context, workspaceID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM webhook_configs WHERE workspace_id = $1`,
		workspaceID,
	)
	return err
}

// InsertDLQ inserts a new webhook DLQ item.
func (r *WebhookDLQRepository) InsertDLQ(ctx context.Context, workspaceID uuid.UUID, traceID, messageID, eventType string, payload []byte, url string, attempts int, failureReason *string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO webhook_dlqs (workspace_id, trace_id, message_id, event_type, payload, webhook_url, attempts, failure_reason, last_attempt_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now(), now())`,
		workspaceID, traceID, messageID, eventType, payload, url, attempts, failureReason,
	)
	return err
}

// ListDLQ lists DLQ items for a workspace with pagination.
func (r *WebhookDLQRepository) ListDLQ(ctx context.Context, workspaceID uuid.UUID, limit, offset int) ([]*WebhookDLQ, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, workspace_id, trace_id, message_id, event_type, payload, webhook_url, last_attempt_at, failure_reason, attempts, created_at, updated_at
		 FROM webhook_dlqs 
		 WHERE workspace_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2 OFFSET $3`,
		workspaceID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*WebhookDLQ
	for rows.Next() {
		var item WebhookDLQ
		err := rows.Scan(
			&item.ID, &item.WorkspaceID, &item.TraceID, &item.MessageID, &item.EventType,
			&item.Payload, &item.WebhookURL, &item.LastAttemptAt, &item.FailureReason,
			&item.Attempts, &item.CreatedAt, &item.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		items = append(items, &item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

// GetDLQByID retrieves a specific DLQ item by ID.
func (r *WebhookDLQRepository) GetDLQByID(ctx context.Context, id uuid.UUID) (*WebhookDLQ, error) {
	var item WebhookDLQ
	err := r.pool.QueryRow(ctx,
		`SELECT id, workspace_id, trace_id, message_id, event_type, payload, webhook_url, last_attempt_at, failure_reason, attempts, created_at, updated_at
		 FROM webhook_dlqs WHERE id = $1`,
		id,
	).Scan(
		&item.ID, &item.WorkspaceID, &item.TraceID, &item.MessageID, &item.EventType,
		&item.Payload, &item.WebhookURL, &item.LastAttemptAt, &item.FailureReason,
		&item.Attempts, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWebhookDLQNotFound
		}
		return nil, err
	}
	return &item, nil
}

// DeleteDLQ deletes a specific DLQ item.
func (r *WebhookDLQRepository) DeleteDLQ(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM webhook_dlqs WHERE id = $1`,
		id,
	)
	return err
}

// GetDLQBadgeCount returns the number of unresolved DLQ items for a workspace.
func (r *WebhookDLQRepository) GetDLQBadgeCount(ctx context.Context, workspaceID uuid.UUID) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM webhook_dlqs WHERE workspace_id = $1`,
		workspaceID,
	).Scan(&count)
	return count, err
}

// ListAllDLQ lists all DLQ items across all workspaces.
func (r *WebhookDLQRepository) ListAllDLQ(ctx context.Context, limit, offset int) ([]*WebhookDLQ, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, workspace_id, trace_id, message_id, event_type, payload, webhook_url, last_attempt_at, failure_reason, attempts, created_at, updated_at
		 FROM webhook_dlqs 
		 ORDER BY created_at DESC
		 LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*WebhookDLQ
	for rows.Next() {
		var item WebhookDLQ
		err := rows.Scan(
			&item.ID, &item.WorkspaceID, &item.TraceID, &item.MessageID, &item.EventType,
			&item.Payload, &item.WebhookURL, &item.LastAttemptAt, &item.FailureReason,
			&item.Attempts, &item.CreatedAt, &item.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		items = append(items, &item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}
