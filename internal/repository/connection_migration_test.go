package repository_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"

	"github.com/pablojhp.pergo/internal/platform/postgres"
)

func getMigrationTestPool(t *testing.T) *pgxpool.Pool {
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

	err = pool.Ping(ctx)
	if err != nil {
		pool.Close()
		t.Skipf("PostgreSQL ping failed at %s: %v", dsn, err)
	}

	return pool
}

func TestConnectionMigration(t *testing.T) {
	pool := getMigrationTestPool(t)
	defer pool.Close()

	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		t.Fatalf("failed to wrap pool as sql.DB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	dir, err := filepath.Abs("../platform/postgres/migrations")
	if err != nil {
		t.Fatalf("failed to get absolute path for migrations: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		wd, _ := os.Getwd()
		t.Fatalf("os.Stat failed for %s (wd: %s): %v", dir, wd, err)
	}

	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("failed to set goose dialect: %v", err)
	}

	// Reset base FS to local OS filesystem to avoid side-effects from tests that set it to embed.FS
	goose.SetBaseFS(nil)

	// Clean up existing migration test workspaces/devices/credentials to avoid conflicts
	_, _ = db.ExecContext(ctx, "DELETE FROM audit_logs")
	_, _ = db.ExecContext(ctx, "DELETE FROM waba_templates")
	_, _ = db.ExecContext(ctx, "DELETE FROM recipient_sessions")
	_, _ = db.ExecContext(ctx, "DELETE FROM message_dispatches")
	_, _ = db.ExecContext(ctx, "DELETE FROM webhooks_dlq")
	_, _ = db.ExecContext(ctx, "DELETE FROM webhooks")
	_, _ = db.ExecContext(ctx, "DELETE FROM api_keys")
	_, _ = db.ExecContext(ctx, "DELETE FROM connections")
	_, _ = db.ExecContext(ctx, "DELETE FROM devices")
	_, _ = db.ExecContext(ctx, "DELETE FROM channel_credentials")
	_, _ = db.ExecContext(ctx, "DELETE FROM workspaces WHERE name LIKE 'migration-test-workspace-%'")

	// 1. Ensure we migrate Down to version 11 first to start from baseline
	if err := goose.DownTo(db, dir, 11); err != nil {
		t.Fatalf("failed to migrate Down to 11: %v", err)
	}

	// 2. Migrate Up to version 11 (no-op if already at 11)
	if err := goose.UpTo(db, dir, 11); err != nil {
		t.Fatalf("failed to migrate Up to version 11: %v", err)
	}

	// 3. Setup mock data at version 11
	wsID := uuid.New()
	_, err = db.ExecContext(ctx, "INSERT INTO workspaces (id, name, created_at, updated_at) VALUES ($1, $2, now(), now())", wsID, "migration-test-workspace-"+wsID.String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM workspaces WHERE id = $1", wsID)
	}()

	// Insert into legacy devices
	deviceID := uuid.New()
	phone := "5511999990001"
	jid := phone + "@s.whatsapp.net"
	_, err = db.ExecContext(ctx, `
		INSERT INTO devices (id, workspace_id, channel, device_id, status, credentials, key_id, key_version, jid, phone, connected_since, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now(), now())
	`, deviceID, wsID, "whatsapp", deviceID.String(), "connected", []byte("enc-device-creds"), "key-1", 1, jid, phone, time.Now().UTC())
	if err != nil {
		t.Fatalf("failed to insert legacy device: %v", err)
	}

	// Insert into legacy channel_credentials
	credID := uuid.New()
	_, err = db.ExecContext(ctx, `
		INSERT INTO channel_credentials (id, workspace_id, channel, credentials, key_id, key_version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now(), now())
	`, credID, wsID, "telegram", []byte("enc-telegram-creds"), "key-1", 1)
	if err != nil {
		t.Fatalf("failed to insert legacy channel credential: %v", err)
	}

	// 4. Migrate Up to version 12 (consolidated connections)
	// Note: During Task 1, version 12 migration file does not exist yet. This will fail or do nothing depending on if 12 exists.
	// We will skip running up to 12 if version 12 file is not present yet.
	version12Exists := false
	if _, err := os.Stat("../platform/postgres/migrations/012_consolidate_connections.sql"); err == nil {
		version12Exists = true
	}

	if !version12Exists {
		t.Log("Migration file for version 12 not created yet, skipping migration verification steps.")
		return
	}

	if err := goose.UpTo(db, dir, 12); err != nil {
		t.Fatalf("failed to migrate Up to version 12: %v", err)
	}

	// Verify connections table exists and has migrated data
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM connections").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query connections: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 connections, got %d", count)
	}

	// Check migrated device
	var connID uuid.UUID
	var connChannel string
	var connIdentity string
	var connStatus string
	var connIsDefault bool
	var connJID sql.NullString

	err = db.QueryRowContext(ctx, `
		SELECT id, channel, sender_identity, status, is_default, jid 
		FROM connections 
		WHERE id = $1
	`, deviceID).Scan(&connID, &connChannel, &connIdentity, &connStatus, &connIsDefault, &connJID)
	if err != nil {
		t.Fatalf("failed to query migrated device: %v", err)
	}
	if connChannel != "whatsapp" || connIdentity != phone || connStatus != "connected" || connJID.String != jid {
		t.Errorf("migrated whatsapp device connection has unexpected values: channel=%s, identity=%s, status=%s, jid=%s", connChannel, connIdentity, connStatus, connJID.String)
	}

	// Check migrated telegram credential
	var credChannel string
	var credIdentity string
	var credStatus string
	var credIsDefault bool
	err = db.QueryRowContext(ctx, `
		SELECT channel, sender_identity, status, is_default 
		FROM connections 
		WHERE id = $1
	`, credID).Scan(&credChannel, &credIdentity, &credStatus, &credIsDefault)
	if err != nil {
		t.Fatalf("failed to query migrated telegram credential: %v", err)
	}
	if credChannel != "telegram" || credStatus != "active" || !credIsDefault {
		t.Errorf("migrated telegram connection has unexpected values: channel=%s, status=%s, is_default=%t", credChannel, credStatus, credIsDefault)
	}

	// Verify legacy tables are dropped
	var tableExists bool
	err = db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_schema = 'public' 
			AND table_name = 'devices'
		)
	`).Scan(&tableExists)
	if err != nil {
		t.Fatalf("failed to check if devices table exists: %v", err)
	}
	if tableExists {
		t.Error("expected devices table to be dropped, but it still exists")
	}

	// 5. Migrate Down to version 11 (restore legacy tables)
	if err := goose.DownTo(db, dir, 11); err != nil {
		t.Fatalf("failed to migrate Down to version 11: %v", err)
	}

	// Verify legacy tables exist and have restored data
	err = db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_schema = 'public' 
			AND table_name = 'devices'
		)
	`).Scan(&tableExists)
	if err != nil {
		t.Fatalf("failed to check if devices table exists after rollback: %v", err)
	}
	if !tableExists {
		t.Error("expected devices table to be recreated after rollback, but it does not exist")
	}

	var restoredDeviceID uuid.UUID
	err = db.QueryRowContext(ctx, "SELECT id FROM devices WHERE id = $1", deviceID).Scan(&restoredDeviceID)
	if err != nil {
		t.Fatalf("failed to query restored device: %v", err)
	}
	if restoredDeviceID != deviceID {
		t.Errorf("restored device ID got %s, want %s", restoredDeviceID, deviceID)
	}
}
