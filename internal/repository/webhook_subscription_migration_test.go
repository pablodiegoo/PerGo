package repository_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/pressly/goose/v3"

	"github.com/pablojhp.pergo/internal/platform/postgres"
)

func TestWebhookSubscriptionMigration(t *testing.T) {
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

	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("failed to set goose dialect: %v", err)
	}
	goose.SetBaseFS(nil)

	// Clean up tables
	_, _ = db.ExecContext(ctx, "DELETE FROM webhook_dlqs")
	_, _ = db.ExecContext(ctx, "DELETE FROM workspaces WHERE name LIKE 'migration-test-workspace-%'")

	// 1. Migrate Down to 22
	if err := goose.DownTo(db, dir, 22); err != nil {
		t.Fatalf("failed to migrate Down to 22: %v", err)
	}

	// 2. Setup mock data at 22 (legacy webhook_configs)
	wsID := uuid.New()
	_, err = db.ExecContext(ctx, "INSERT INTO workspaces (id, name, created_at, updated_at) VALUES ($1, $2, now(), now())", wsID, "migration-test-workspace-"+wsID.String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM workspaces WHERE id = $1", wsID)
	}()

	configID := uuid.New()
	_, err = db.ExecContext(ctx, `
		INSERT INTO webhook_configs (id, workspace_id, url, secret, key_id, key_version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now(), now())
	`, configID, wsID, "https://old.url", []byte("old-secret"), "key-1", 1)
	if err != nil {
		t.Fatalf("failed to insert legacy webhook config: %v", err)
	}

	dlqID := uuid.New()
	_, err = db.ExecContext(ctx, `
		INSERT INTO webhook_dlqs (id, workspace_id, trace_id, message_id, event_type, payload, webhook_url, last_attempt_at, failure_reason, attempts)
		VALUES ($1, $2, 't1', 'm1', 'e1', '{}', 'https://old.url', now(), 'reason', 1)
	`, dlqID, wsID)
	if err != nil {
		t.Fatalf("failed to insert legacy webhook dlq: %v", err)
	}

	// 3. Migrate Up to 23
	if err := goose.UpTo(db, dir, 23); err != nil {
		t.Fatalf("failed to migrate Up to 23: %v", err)
	}

	// Verify subscription was created
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM webhook_subscriptions").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query webhook_subscriptions: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 webhook subscription, got %d", count)
	}

	// Verify DLQ subscription_id was set
	var subID uuid.UUID
	err = db.QueryRowContext(ctx, "SELECT subscription_id FROM webhook_dlqs WHERE id = $1", dlqID).Scan(&subID)
	if err != nil {
		t.Fatalf("failed to query webhook_dlqs subscription_id: %v", err)
	}
	if subID == uuid.Nil {
		t.Errorf("expected non-nil subscription_id in DLQ item")
	}

	// 4. Migrate Down to 22 (restore legacy webhook_configs)
	if err := goose.DownTo(db, dir, 22); err != nil {
		t.Fatalf("failed to migrate Down to 22: %v", err)
	}

	// Verify webhook_configs has the restored config
	var restoredURL string
	err = db.QueryRowContext(ctx, "SELECT url FROM webhook_configs WHERE workspace_id = $1", wsID).Scan(&restoredURL)
	if err != nil {
		t.Fatalf("failed to query restored legacy webhook config: %v", err)
	}
	if restoredURL != "https://old.url" {
		t.Errorf("expected URL 'https://old.url', got '%s'", restoredURL)
	}

	// 5. Migrate back Up to 23 to leave the database clean
	if err := goose.UpTo(db, dir, 23); err != nil {
		t.Fatalf("failed to migrate back Up to 23: %v", err)
	}
}
