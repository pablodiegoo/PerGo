package repository

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres"
)

type ConnectionSpike struct {
	ID             uuid.UUID
	WorkspaceID    uuid.UUID
	Name           string
	Channel        string
	SenderIdentity string
	Status         string
	IsDefault      bool
	Credentials    []byte
	KeyID          string
	KeyVersion     int
	JID            *string
	ConnectedSince *time.Time
}

func TestConnectionsSpike(t *testing.T) {
	// 1. Get database connection pool
	dsn := os.Getenv("PERGO_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5433/pergo?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("PostgreSQL ping failed: %v", err)
	}

	// Run migrations to ensure dependencies like workspaces exist
	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		t.Fatalf("failed to wrap pool as sql.DB: %v", err)
	}
	defer db.Close()

	if err := postgres.RunMigrations(db); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// 2. Setup temporary connections_spike table
	_, err = pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS connections_spike (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			channel TEXT NOT NULL,
			sender_identity TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			is_default BOOLEAN NOT NULL DEFAULT FALSE,
			credentials BYTEA,
			key_id TEXT,
			key_version INT NOT NULL DEFAULT 1,
			jid TEXT,
			connected_since TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE (sender_identity)
		)
	`)
	if err != nil {
		t.Fatalf("failed to create temporary spike table: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, "DROP TABLE IF EXISTS connections_spike")
	}()

	// 3. Create a test workspace
	wsRepo := NewWorkspaceRepository(pool)
	ws, err := wsRepo.Create(ctx, "spike_workspace_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	// 4. Setup Encryptor
	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(i)
	}
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	// 5. Helper function to encrypt and save connections
	saveConnection := func(name, channel, senderIdentity string, credsMap map[string]string, isDefault bool) {
		credsBytes, _ := json.Marshal(credsMap)
		ciphertext, keyID, keyVersion, err := enc.Encrypt(credsBytes)
		if err != nil {
			t.Fatalf("failed to encrypt credentials: %v", err)
		}

		_, err = pool.Exec(ctx, `
			INSERT INTO connections_spike (workspace_id, name, channel, sender_identity, is_default, credentials, key_id, key_version)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, ws.ID, name, channel, senderIdentity, isDefault, ciphertext, keyID, keyVersion)
		if err != nil {
			t.Fatalf("failed to insert connection: %v", err)
		}
	}

	// 6. Insert multiple connections of same channel types (2 WABA, 2 Telegram)
	saveConnection("WABA Primary", "whatsapp_cloud", "+5511999990001", map[string]string{"token": "meta-token-1"}, true)
	saveConnection("WABA Secondary", "whatsapp_cloud", "+5511999990002", map[string]string{"token": "meta-token-2"}, false)
	saveConnection("Telegram Support", "telegram", "@pergo_support_bot", map[string]string{"token": "tele-token-1"}, true)
	saveConnection("Telegram Alerts", "telegram", "@pergo_alerts_bot", map[string]string{"token": "tele-token-2"}, false)

	// 7. Verify global unique constraint on sender_identity (cannot add same identity twice)
	_, err = pool.Exec(ctx, `
		INSERT INTO connections_spike (workspace_id, name, channel, sender_identity, credentials, key_id)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, ws.ID, "Duplicate WABA", "whatsapp_cloud", "+5511999990001", []byte("xyz"), "default")
	if err == nil {
		t.Error("expected unique constraint violation for duplicate sender_identity, got nil error")
	}

	// 8. Simulate routing and credential retrieval
	getConnectionCredentials := func(senderIdentity string) (map[string]string, error) {
		var ciphertext []byte
		var keyID string
		var keyVersion int

		err := pool.QueryRow(ctx, `
			SELECT credentials, key_id, key_version 
			FROM connections_spike 
			WHERE workspace_id = $1 AND sender_identity = $2
		`, ws.ID, senderIdentity).Scan(&ciphertext, &keyID, &keyVersion)
		if err != nil {
			return nil, err
		}

		plaintext, err := enc.Decrypt(ciphertext)
		if err != nil {
			return nil, err
		}

		var creds map[string]string
		if err := json.Unmarshal(plaintext, &creds); err != nil {
			return nil, err
		}
		return creds, nil
	}

	// 9. Assert correct routing and decryption for all 4 instances
	testCases := []struct {
		identity      string
		expectedToken string
	}{
		{"+5511999990001", "meta-token-1"},
		{"+5511999990002", "meta-token-2"},
		{"@pergo_support_bot", "tele-token-1"},
		{"@pergo_alerts_bot", "tele-token-2"},
	}

	for _, tc := range testCases {
		creds, err := getConnectionCredentials(tc.identity)
		if err != nil {
			t.Errorf("failed to retrieve credentials for %s: %v", tc.identity, err)
			continue
		}
		if creds["token"] != tc.expectedToken {
			t.Errorf("for %s, got token = %q, want %q", tc.identity, creds["token"], tc.expectedToken)
		}
	}
}
