package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrIntegrationNotFound is returned when an integration config cannot be found.
var ErrIntegrationNotFound = errors.New("integration not found")

// Integration represents a unified third-party integration configuration.
type Integration struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	Name        string    `json:"name"`
	Provider    string    `json:"provider"` // e.g. "chatwoot", "typebot"
	Active      bool      `json:"active"`
	Config      []byte    `json:"config,omitempty"` // Holds plaintext config JSON envelope in memory
	KeyID       string    `json:"key_id,omitempty"`
	KeyVersion  int       `json:"key_version,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// IntegrationRepository manages CRUD operations and credentials cryptography for integrations.
type IntegrationRepository struct {
	pool     *pgxpool.Pool
	provider CredentialProvider
}

// NewIntegrationRepository creates a new IntegrationRepository.
func NewIntegrationRepository(pool *pgxpool.Pool, provider CredentialProvider) *IntegrationRepository {
	return &IntegrationRepository{
		pool:     pool,
		provider: provider,
	}
}

// Save inserts or updates an integration configuration, encrypting the config payload.
func (r *IntegrationRepository) Save(ctx context.Context, i *Integration) error {
	if i.ID == uuid.Nil {
		i.ID = uuid.New()
	}

	var ciphertext []byte
	var keyID string
	var keyVersion int = 1

	if len(i.Config) > 0 {
		var err error
		ciphertext, keyID, keyVersion, err = r.provider.Encrypt(i.Config)
		if err != nil {
			return fmt.Errorf("failed to encrypt configuration config: %w", err)
		}
	}

	query := `
		INSERT INTO integrations (
			id, workspace_id, name, provider, active, config, key_id, key_version, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
		ON CONFLICT (workspace_id, provider) DO UPDATE SET
			name = EXCLUDED.name,
			active = EXCLUDED.active,
			config = EXCLUDED.config,
			key_id = EXCLUDED.key_id,
			key_version = EXCLUDED.key_version,
			updated_at = NOW()
	`

	_, err := r.pool.Exec(ctx, query,
		i.ID, i.WorkspaceID, i.Name, i.Provider, i.Active,
		ciphertext, keyID, keyVersion,
	)
	if err != nil {
		return fmt.Errorf("failed to save integration: %w", err)
	}

	return nil
}

// GetByProvider retrieves and decrypts an integration configuration by workspace ID and provider name.
func (r *IntegrationRepository) GetByProvider(ctx context.Context, workspaceID uuid.UUID, provider string) (*Integration, error) {
	query := `
		SELECT id, workspace_id, name, provider, active, config, key_id, key_version, created_at, updated_at
		FROM integrations
		WHERE workspace_id = $1 AND provider = $2
	`
	row := r.pool.QueryRow(ctx, query, workspaceID, provider)
	return r.scanAndDecrypt(row)
}

func (r *IntegrationRepository) scanAndDecrypt(row pgx.Row) (*Integration, error) {
	var i Integration
	var ciphertext []byte
	var keyID sql.NullString
	var keyVersion int

	err := row.Scan(
		&i.ID, &i.WorkspaceID, &i.Name, &i.Provider, &i.Active,
		&ciphertext, &keyID, &keyVersion, &i.CreatedAt, &i.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrIntegrationNotFound
		}
		return nil, err
	}

	i.KeyID = keyID.String
	i.KeyVersion = keyVersion

	if len(ciphertext) > 0 {
		plaintext, err := r.provider.Decrypt(ciphertext)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt integration configuration: %w", err)
		}
		i.Config = plaintext
	}

	return &i, nil
}
