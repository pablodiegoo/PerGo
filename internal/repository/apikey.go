package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pablojhp.pergo/internal/platform/crypto"
)

// APIKey represents an API key entity.
type APIKey struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	KeyPrefix   string
	KeyHash     []byte
	Name        string
	RevokedAt   *time.Time
	KeyID       string
	KeyVersion  int
	CreatedAt   time.Time
}

type cacheEntry struct {
	value   *APIKey
	expires time.Time
}

// APIKeyRepository provides CRUD operations for API keys with an in-memory cache.
type APIKeyRepository struct {
	pool  *pgxpool.Pool
	cache map[string]*cacheEntry
	mu    sync.RWMutex
}

// NewAPIKeyRepository creates a new APIKeyRepository.
func NewAPIKeyRepository(pool *pgxpool.Pool) *APIKeyRepository {
	return &APIKeyRepository{
		pool:  pool,
		cache: make(map[string]*cacheEntry),
	}
}

// Create generates a new API key, stores the hash and prefix, and returns the API key and plaintext key.
func (r *APIKeyRepository) Create(ctx context.Context, workspaceID uuid.UUID, name string) (*APIKey, string, error) {
	// Generate random 32-byte key, hex-encoded for safe UTF-8 storage of the prefix
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return nil, "", err
	}
	plaintext := hex.EncodeToString(keyBytes)

	hash, prefix := crypto.HashAPIKey(plaintext)

	keyID := uuid.New().String()

	var apiKey APIKey
	err := r.pool.QueryRow(ctx,
		`INSERT INTO api_keys (workspace_id, key_prefix, key_hash, name, key_id, key_version)
		 VALUES ($1, $2, $3, $4, $5, 1)
		 RETURNING id, workspace_id, key_prefix, key_hash, name, revoked_at, key_id, key_version, created_at`,
		workspaceID, prefix, hash, name, keyID,
	).Scan(&apiKey.ID, &apiKey.WorkspaceID, &apiKey.KeyPrefix, &apiKey.KeyHash,
		&apiKey.Name, &apiKey.RevokedAt, &apiKey.KeyID, &apiKey.KeyVersion, &apiKey.CreatedAt)
	if err != nil {
		return nil, "", err
	}

	return &apiKey, plaintext, nil
}

// GetByPrefix looks up an API key by its prefix, checking the in-memory cache first.
func (r *APIKeyRepository) GetByPrefix(ctx context.Context, prefix string) (*APIKey, error) {
	// Check cache
	r.mu.RLock()
	if entry, ok := r.cache[prefix]; ok && time.Now().Before(entry.expires) {
		r.mu.RUnlock()
		return entry.value, nil
	}
	r.mu.RUnlock()

	// Query DB
	var apiKey APIKey
	err := r.pool.QueryRow(ctx,
		`SELECT id, workspace_id, key_prefix, key_hash, name, revoked_at, key_id, key_version, created_at
		 FROM api_keys WHERE key_prefix = $1 AND revoked_at IS NULL`,
		prefix,
	).Scan(&apiKey.ID, &apiKey.WorkspaceID, &apiKey.KeyPrefix, &apiKey.KeyHash,
		&apiKey.Name, &apiKey.RevokedAt, &apiKey.KeyID, &apiKey.KeyVersion, &apiKey.CreatedAt)
	if err != nil {
		return nil, err
	}

	// Store in cache with 5-minute TTL
	r.mu.Lock()
	r.cache[prefix] = &cacheEntry{
		value:   &apiKey,
		expires: time.Now().Add(5 * time.Minute),
	}
	r.mu.Unlock()

	return &apiKey, nil
}

// Revoke marks an API key as revoked and invalidates the cache entry.
func (r *APIKeyRepository) Revoke(ctx context.Context, id uuid.UUID) error {
	var prefix string
	err := r.pool.QueryRow(ctx,
		`UPDATE api_keys SET revoked_at = now() WHERE id = $1 RETURNING key_prefix`,
		id,
	).Scan(&prefix)
	if err != nil {
		return err
	}

	// Invalidate cache
	r.mu.Lock()
	delete(r.cache, prefix)
	r.mu.Unlock()

	return nil
}

// ListByWorkspace returns all API keys for a workspace (including revoked), ordered by created_at descending.
func (r *APIKeyRepository) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]APIKey, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, workspace_id, key_prefix, key_hash, name, revoked_at, key_id, key_version, created_at
		 FROM api_keys WHERE workspace_id = $1 ORDER BY created_at DESC`,
		workspaceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []APIKey
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.WorkspaceID, &k.KeyPrefix, &k.KeyHash,
			&k.Name, &k.RevokedAt, &k.KeyID, &k.KeyVersion, &k.CreatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// GetByID retrieves an API key by ID.
func (r *APIKeyRepository) GetByID(ctx context.Context, id uuid.UUID) (*APIKey, error) {
	var apiKey APIKey
	err := r.pool.QueryRow(ctx,
		`SELECT id, workspace_id, key_prefix, key_hash, name, revoked_at, key_id, key_version, created_at
		 FROM api_keys WHERE id = $1`,
		id,
	).Scan(&apiKey.ID, &apiKey.WorkspaceID, &apiKey.KeyPrefix, &apiKey.KeyHash,
		&apiKey.Name, &apiKey.RevokedAt, &apiKey.KeyID, &apiKey.KeyVersion, &apiKey.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &apiKey, nil
}

// CountActive returns the count of non-revoked API keys for the workspace.
func (r *APIKeyRepository) CountActive(ctx context.Context, workspaceID uuid.UUID) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM api_keys WHERE workspace_id = $1 AND revoked_at IS NULL`,
		workspaceID,
	).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}
