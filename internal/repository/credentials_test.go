package repository

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres"
)

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("PERGO_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("PostgreSQL not available at %s: %v", dsn, err)
	}

	// Ping to check if connection actually works
	err = pool.Ping(ctx)
	if err != nil {
		pool.Close()
		t.Skipf("PostgreSQL ping failed at %s: %v", dsn, err)
	}

	// Run migrations to ensure schema is up to date
	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("failed to wrap pool as sql.DB: %v", err)
	}
	defer db.Close()

	if err := postgres.RunMigrations(db); err != nil {
		pool.Close()
		t.Fatalf("failed to run migrations: %v", err)
	}

	return pool
}

func TestCredentialsRepository(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	// 1. Setup Encryptor
	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(i)
	}
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	repo := NewCredentialsRepository(pool, enc)

	// 2. Create a test workspace
	wsRepo := NewWorkspaceRepository(pool)
	wsName := "credentials_test_ws_" + uuid.New().String()
	ws, err := wsRepo.Create(ctx, wsName)
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	// Test data
	channelName := "whatsapp_cloud"
	secretPayload := []byte("meta-secret-api-token-123456")

	// 3. Verify Save
	err = repo.Save(ctx, ws.ID, channelName, secretPayload)
	if err != nil {
		t.Fatalf("failed to save credentials: %v", err)
	}

	// 4. Verify DB content is encrypted (does not match plaintext)
	var dbCredentials []byte
	err = pool.QueryRow(ctx,
		"SELECT credentials FROM connections WHERE workspace_id = $1 AND channel = $2",
		ws.ID, channelName,
	).Scan(&dbCredentials)
	if err != nil {
		t.Fatalf("failed to select raw credentials from DB: %v", err)
	}

	if bytes.Equal(dbCredentials, secretPayload) {
		t.Error("expected credentials in database to be encrypted, but they matched plaintext")
	}

	// 5. Verify Get decrypts correctly
	decrypted, err := repo.Get(ctx, ws.ID, channelName)
	if err != nil {
		t.Fatalf("failed to get and decrypt credentials: %v", err)
	}

	if !bytes.Equal(decrypted, secretPayload) {
		t.Errorf("decrypted credentials = %q, want %q", string(decrypted), string(secretPayload))
	}

	// 6. Test Upsert (Save again on conflict)
	newSecretPayload := []byte("meta-secret-api-token-987654")
	err = repo.Save(ctx, ws.ID, channelName, newSecretPayload)
	if err != nil {
		t.Fatalf("failed to update/upsert credentials: %v", err)
	}

	decryptedNew, err := repo.Get(ctx, ws.ID, channelName)
	if err != nil {
		t.Fatalf("failed to get updated credentials: %v", err)
	}

	if !bytes.Equal(decryptedNew, newSecretPayload) {
		t.Errorf("decrypted updated credentials = %q, want %q", string(decryptedNew), string(newSecretPayload))
	}

	// 7. Verify Delete
	err = repo.Delete(ctx, ws.ID, channelName)
	if err != nil {
		t.Fatalf("failed to delete credentials: %v", err)
	}

	_, err = repo.Get(ctx, ws.ID, channelName)
	if !errors.Is(err, ErrCredentialsNotFound) {
		t.Errorf("expected ErrCredentialsNotFound on get after delete, got: %v", err)
	}
}

func TestCredentialsEmpty(t *testing.T) {
	kek := make([]byte, 32)
	enc, _ := crypto.NewEncryptor(kek)
	repo := NewCredentialsRepository(nil, enc)

	err := repo.Save(context.Background(), uuid.New(), "telegram", nil)
	if err == nil {
		t.Error("expected error saving empty credentials, got nil")
	}
}
