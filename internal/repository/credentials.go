package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pablojhp.pergo/internal/platform/crypto"
)

// ErrCredentialsNotFound is returned when credentials cannot be found.
var ErrCredentialsNotFound = errors.New("credentials not found")

// CredentialsRepository provides CRUD operations for channel credentials.
type CredentialsRepository struct {
	pool      *pgxpool.Pool
	encryptor *crypto.Encryptor
}

// NewCredentialsRepository creates a new CredentialsRepository.
func NewCredentialsRepository(pool *pgxpool.Pool, encryptor *crypto.Encryptor) *CredentialsRepository {
	return &CredentialsRepository{
		pool:      pool,
		encryptor: encryptor,
	}
}

// Save encrypts the credentials payload and saves or updates it in the database.
func (r *CredentialsRepository) Save(ctx context.Context, workspaceID uuid.UUID, channel string, plaintext []byte) error {
	if len(plaintext) == 0 {
		return errors.New("credentials payload cannot be empty")
	}

	ciphertext, keyID, keyVersion, err := r.encryptor.Encrypt(plaintext)
	if err != nil {
		return err
	}

	_, err = r.pool.Exec(ctx,
		`INSERT INTO channel_credentials (workspace_id, channel, credentials, key_id, key_version, updated_at)
		 VALUES ($1, $2, $3, $4, $5, now())
		 ON CONFLICT (workspace_id, channel)
		 DO UPDATE SET credentials = EXCLUDED.credentials, key_id = EXCLUDED.key_id, key_version = EXCLUDED.key_version, updated_at = now()`,
		workspaceID, channel, ciphertext, keyID, keyVersion,
	)
	return err
}

// Get retrieves the credentials from the database and decrypts them.
func (r *CredentialsRepository) Get(ctx context.Context, workspaceID uuid.UUID, channel string) ([]byte, error) {
	var ciphertext []byte
	var keyID string
	var keyVersion int

	err := r.pool.QueryRow(ctx,
		`SELECT credentials, key_id, key_version FROM channel_credentials WHERE workspace_id = $1 AND channel = $2`,
		workspaceID, channel,
	).Scan(&ciphertext, &keyID, &keyVersion)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCredentialsNotFound
		}
		return nil, err
	}

	return r.encryptor.Decrypt(ciphertext)
}

// Delete removes credentials for a workspace and channel.
func (r *CredentialsRepository) Delete(ctx context.Context, workspaceID uuid.UUID, channel string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM channel_credentials WHERE workspace_id = $1 AND channel = $2`,
		workspaceID, channel,
	)
	return err
}
