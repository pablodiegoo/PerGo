package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrCredentialsNotFound is returned when credentials cannot be found.
var ErrCredentialsNotFound = errors.New("credentials not found")

// CredentialsRepository provides CRUD operations for channel credentials, shimmed on top of connections table.
type CredentialsRepository struct {
	pool      *pgxpool.Pool
	provider CredentialProvider
}

// NewCredentialsRepository creates a new CredentialsRepository.
func NewCredentialsRepository(pool *pgxpool.Pool, provider CredentialProvider) *CredentialsRepository {
	return &CredentialsRepository{
		pool:     pool,
		provider: provider,
	}
}

// Save encrypts the credentials payload and saves or updates it in the connections table.
func (r *CredentialsRepository) Save(ctx context.Context, workspaceID uuid.UUID, channel string, plaintext []byte) error {
	if len(plaintext) == 0 {
		return errors.New("credentials payload cannot be empty")
	}

	ciphertext, keyID, keyVersion, err := r.provider.Encrypt(plaintext)
	if err != nil {
		return err
	}

	var senderIdentity string
	if channel == "telegram" {
		var tgCfg struct {
			BotUsername string `json:"bot_username"`
		}
		if err := json.Unmarshal(plaintext, &tgCfg); err == nil && tgCfg.BotUsername != "" {
			senderIdentity = tgCfg.BotUsername
		}
	}

	// Check if a connection already exists for this workspace and channel (legacy uniqueness)
	var connID uuid.UUID
	err = r.pool.QueryRow(ctx,
		`SELECT id FROM connections WHERE workspace_id = $1 AND channel = $2 LIMIT 1`,
		workspaceID, channel,
	).Scan(&connID)

	if err == nil {
		// Update existing connection's credentials
		_, err = r.pool.Exec(ctx,
			`UPDATE connections 
			 SET credentials = $2, key_id = $3, key_version = $4, sender_identity = COALESCE(NULLIF($5, ''), sender_identity), updated_at = now() 
			 WHERE id = $1`,
			connID, ciphertext, keyID, keyVersion, senderIdentity,
		)
		return err
	}

	if errors.Is(err, pgx.ErrNoRows) {
		// Insert new connection
		name := "WhatsApp WABA"
		if channel == "telegram" {
			name = "Telegram Bot"
		}
		newID := uuid.New()
		if senderIdentity == "" {
			senderIdentity = fmt.Sprintf("legacy_%s_%s", channel, newID.String())
		}
		_, err = r.pool.Exec(ctx,
			`INSERT INTO connections (
				id, workspace_id, name, channel, sender_identity, status, is_default, 
				credentials, key_id, key_version, created_at, updated_at
			 ) VALUES ($1, $2, $3, $4, $5, 'active', TRUE, $6, $7, $8, now(), now())`,
			newID, workspaceID, name, channel, senderIdentity, ciphertext, keyID, keyVersion,
		)
		return err
	}

	return err
}

// Get retrieves the credentials from the connections table and decrypts them.
func (r *CredentialsRepository) Get(ctx context.Context, workspaceID uuid.UUID, channel string) ([]byte, error) {
	var ciphertext []byte
	var keyID string
	var keyVersion int

	err := r.pool.QueryRow(ctx,
		`SELECT credentials, key_id, key_version FROM connections WHERE workspace_id = $1 AND channel = $2 LIMIT 1`,
		workspaceID, channel,
	).Scan(&ciphertext, &keyID, &keyVersion)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCredentialsNotFound
		}
		return nil, err
	}

	if len(ciphertext) == 0 {
		return nil, ErrCredentialsNotFound
	}

	return r.provider.Decrypt(ciphertext)
}

// Delete removes credentials for a workspace and channel.
func (r *CredentialsRepository) Delete(ctx context.Context, workspaceID uuid.UUID, channel string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM connections WHERE workspace_id = $1 AND channel = $2`,
		workspaceID, channel,
	)
	return err
}
