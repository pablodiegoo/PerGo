package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrWebhookSubscriptionNotFound = errors.New("webhook subscription not found")
)

type WebhookSubscription struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	URL         string
	Secret      []byte // Plaintext secret decrypted by CredentialProvider
	KeyID       string
	KeyVersion  int
	EventTypes  []string
	Active      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type WebhookSubscriptionRepository struct {
	pool      *pgxpool.Pool
	encryptor CredentialProvider
}

func NewWebhookSubscriptionRepository(pool *pgxpool.Pool, encryptor CredentialProvider) *WebhookSubscriptionRepository {
	return &WebhookSubscriptionRepository{
		pool:      pool,
		encryptor: encryptor,
	}
}

// Create inserts a new subscription
func (r *WebhookSubscriptionRepository) Create(ctx context.Context, wsID uuid.UUID, url string, eventTypes []string, secretPlaintext []byte) (*WebhookSubscription, error) {
	ciphertext, keyID, keyVersion, err := r.encryptor.Encrypt(secretPlaintext)
	if err != nil {
		return nil, err
	}

	var sub WebhookSubscription
	err = r.pool.QueryRow(ctx,
		`INSERT INTO webhook_subscriptions (workspace_id, url, secret, key_id, key_version, event_types, active, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, TRUE, now())
		 RETURNING id, workspace_id, url, key_id, key_version, event_types, active, created_at, updated_at`,
		wsID, url, ciphertext, keyID, keyVersion, eventTypes,
	).Scan(&sub.ID, &sub.WorkspaceID, &sub.URL, &sub.KeyID, &sub.KeyVersion, &sub.EventTypes, &sub.Active, &sub.CreatedAt, &sub.UpdatedAt)
	if err != nil {
		return nil, err
	}
	sub.Secret = secretPlaintext
	return &sub, nil
}

// Get retrieves a subscription by ID
func (r *WebhookSubscriptionRepository) Get(ctx context.Context, id uuid.UUID) (*WebhookSubscription, error) {
	var sub WebhookSubscription
	var ciphertext []byte

	err := r.pool.QueryRow(ctx,
		`SELECT id, workspace_id, url, secret, key_id, key_version, event_types, active, created_at, updated_at
		 FROM webhook_subscriptions WHERE id = $1`,
		id,
	).Scan(&sub.ID, &sub.WorkspaceID, &sub.URL, &ciphertext, &sub.KeyID, &sub.KeyVersion, &sub.EventTypes, &sub.Active, &sub.CreatedAt, &sub.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWebhookSubscriptionNotFound
		}
		return nil, err
	}

	secret, err := r.encryptor.Decrypt(ciphertext)
	if err != nil {
		return nil, err
	}
	sub.Secret = secret
	return &sub, nil
}

// ListByWorkspace returns all subscriptions belonging to a workspace
func (r *WebhookSubscriptionRepository) ListByWorkspace(ctx context.Context, wsID uuid.UUID) ([]*WebhookSubscription, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, workspace_id, url, secret, key_id, key_version, event_types, active, created_at, updated_at
		 FROM webhook_subscriptions WHERE workspace_id = $1 ORDER BY created_at DESC`,
		wsID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []*WebhookSubscription
	for rows.Next() {
		var sub WebhookSubscription
		var ciphertext []byte
		err := rows.Scan(&sub.ID, &sub.WorkspaceID, &sub.URL, &ciphertext, &sub.KeyID, &sub.KeyVersion, &sub.EventTypes, &sub.Active, &sub.CreatedAt, &sub.UpdatedAt)
		if err != nil {
			return nil, err
		}
		secret, err := r.encryptor.Decrypt(ciphertext)
		if err != nil {
			return nil, err
		}
		sub.Secret = secret
		subs = append(subs, &sub)
	}
	return subs, rows.Err()
}

// Update modifies a subscription
func (r *WebhookSubscriptionRepository) Update(ctx context.Context, id uuid.UUID, url string, eventTypes []string, active bool, secretPlaintext []byte) error {
	var err error
	if len(secretPlaintext) > 0 {
		ciphertext, keyID, keyVersion, err := r.encryptor.Encrypt(secretPlaintext)
		if err != nil {
			return err
		}
		_, err = r.pool.Exec(ctx,
			`UPDATE webhook_subscriptions 
			 SET url = $1, event_types = $2, active = $3, secret = $4, key_id = $5, key_version = $6, updated_at = now()
			 WHERE id = $7`,
			url, eventTypes, active, ciphertext, keyID, keyVersion, id,
		)
	} else {
		_, err = r.pool.Exec(ctx,
			`UPDATE webhook_subscriptions 
			 SET url = $1, event_types = $2, active = $3, updated_at = now()
			 WHERE id = $4`,
			url, eventTypes, active, id,
		)
	}
	return err
}

// Delete removes a subscription
func (r *WebhookSubscriptionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, "DELETE FROM webhook_subscriptions WHERE id = $1", id)
	return err
}
