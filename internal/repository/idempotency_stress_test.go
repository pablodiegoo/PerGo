package repository_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestIdempotencyRepository_Concurrency(t *testing.T) {
	pool := getTestPoolWithMigrations(t)
	if pool == nil {
		t.Skip("Postgres not available")
	}
	defer pool.Close()

	ctx := context.Background()
	wsID := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO workspaces (id, name) VALUES ($1, $2)`, wsID, "Concurrency WS")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	repo := repository.NewIdempotencyRepository(pool)
	keyHash := "concurrent_key_hash_1234567890abcdef1234567890abcdef"
	traceID := "trace_concurrent_123"

	const goroutines = 50
	var successCount int32
	var errorCount int32
	var wg sync.WaitGroup

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			inserted, err := repo.CheckAndStore(ctx, wsID, keyHash, fmt.Sprintf("%s_%d", traceID, id), 1*time.Hour)
			if err != nil {
				atomic.AddInt32(&errorCount, 1)
				return
			}
			if inserted {
				atomic.AddInt32(&successCount, 1)
			}
		}(i)
	}
	wg.Wait()

	if errorCount > 0 {
		t.Errorf("expected 0 errors, got %d", errorCount)
	}
	if successCount != 1 {
		t.Errorf("expected exactly 1 successful insert under concurrency, got %d", successCount)
	}
}

func TestIdempotencyRepository_UpdateResponse_Concurrently(t *testing.T) {
	pool := getTestPoolWithMigrations(t)
	if pool == nil {
		t.Skip("Postgres not available")
	}
	defer pool.Close()

	ctx := context.Background()
	wsID := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO workspaces (id, name) VALUES ($1, $2)`, wsID, "Update Response WS")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	repo := repository.NewIdempotencyRepository(pool)
	keyHash := "update_resp_key_hash_1234567890abcdef"
	traceID := "trace_update_resp"

	inserted, err := repo.CheckAndStore(ctx, wsID, keyHash, traceID, 1*time.Hour)
	if err != nil || !inserted {
		t.Fatalf("CheckAndStore failed: %v", err)
	}

	const workers = 20
	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func(id int) {
			defer wg.Done()
			body := []byte(fmt.Sprintf(`{"worker":%d}`, id))
			providerID := fmt.Sprintf("provider_msg_%d", id)
			_ = repo.UpdateResponse(ctx, wsID, keyHash, 200+id, body, &providerID)
		}(i)
	}
	wg.Wait()

	entry, err := repo.GetByIdempotencyKey(ctx, wsID, keyHash)
	if err != nil {
		t.Fatalf("GetByIdempotencyKey failed: %v", err)
	}
	if entry.StatusCode == nil || *entry.StatusCode < 200 || *entry.StatusCode >= 220 {
		t.Errorf("unexpected status code after concurrent updates: %v", entry.StatusCode)
	}
}

func TestIdempotencyRepository_ExpiredKeyLifecycle(t *testing.T) {
	pool := getTestPoolWithMigrations(t)
	if pool == nil {
		t.Skip("Postgres not available")
	}
	defer pool.Close()

	ctx := context.Background()
	wsID := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO workspaces (id, name) VALUES ($1, $2)`, wsID, "Expired Key WS")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	repo := repository.NewIdempotencyRepository(pool)
	keyHash := "expired_key_hash_1234567890abcdef"
	traceID := "trace_expired"

	// 1. Insert key with 1ms TTL
	inserted, err := repo.CheckAndStore(ctx, wsID, keyHash, traceID, 1*time.Millisecond)
	if err != nil || !inserted {
		t.Fatalf("failed to insert key with 1ms TTL: %v", err)
	}

	// Sleep 50ms so key expires in Postgres
	time.Sleep(50 * time.Millisecond)

	// 2. GetByIdempotencyKey should return ErrIdempotencyKeyNotFound because it's expired
	_, err = repo.GetByIdempotencyKey(ctx, wsID, keyHash)
	if err != repository.ErrIdempotencyKeyNotFound {
		t.Errorf("expected ErrIdempotencyKeyNotFound for expired key, got %v", err)
	}

	// 3. CleanupExpired should delete the expired key
	deleted, err := repo.CleanupExpired(ctx)
	if err != nil {
		t.Fatalf("CleanupExpired failed: %v", err)
	}
	if deleted < 1 {
		t.Errorf("expected at least 1 deleted row in CleanupExpired, got %d", deleted)
	}

	// 4. CheckAndStore can now re-insert the key since it was deleted
	reinserted, err := repo.CheckAndStore(ctx, wsID, keyHash, traceID+"_new", 1*time.Hour)
	if err != nil {
		t.Fatalf("CheckAndStore failed after cleanup: %v", err)
	}
	if !reinserted {
		t.Errorf("expected reinserted=true after cleanup")
	}
}

func TestIdempotencyRepository_LedgerEdgeCases(t *testing.T) {
	pool := getTestPoolWithMigrations(t)
	if pool == nil {
		t.Skip("Postgres not available")
	}
	defer pool.Close()

	ctx := context.Background()
	wsID := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO workspaces (id, name) VALUES ($1, $2)`, wsID, "Ledger Edge WS")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	repo := repository.NewIdempotencyRepository(pool)
	traceID := "trace_ledger_edge_123"

	// Record with zero UUID (Nil) and empty status -> defaults apply
	ledgerEntry := &repository.IngressLedgerEntry{
		WorkspaceID:    wsID,
		TraceID:        traceID,
		IdempotencyKey: "ledger_key_hash",
		Channel:        "telegram",
		Recipient:      "@testuser",
		// Status empty -> should default to 'accepted'
	}

	err = repo.RecordLedger(ctx, ledgerEntry)
	if err != nil {
		t.Fatalf("RecordLedger failed: %v", err)
	}
	if ledgerEntry.ID == uuid.Nil {
		t.Errorf("expected auto-generated ID, got nil UUID")
	}
	if ledgerEntry.Status != "accepted" {
		t.Errorf("expected status 'accepted', got %s", ledgerEntry.Status)
	}

	// Update status with error reason
	errReason := "network timeout"
	err = repo.UpdateLedgerStatus(ctx, wsID, traceID, "failed", &errReason)
	if err != nil {
		t.Fatalf("UpdateLedgerStatus failed: %v", err)
	}

	// Verify ledger updated in DB directly
	var status string
	var reason *string
	err = pool.QueryRow(ctx, `SELECT status, error_reason FROM message_ingress_ledger WHERE workspace_id = $1 AND trace_id = $2`, wsID, traceID).Scan(&status, &reason)
	if err != nil {
		t.Fatalf("failed to query ledger: %v", err)
	}
	if status != "failed" {
		t.Errorf("expected status 'failed', got %s", status)
	}
	if reason == nil || *reason != errReason {
		t.Errorf("expected error reason '%s', got %v", errReason, reason)
	}
}

func TestIdempotencyRepository_InvalidStatusCheckConstraint(t *testing.T) {
	pool := getTestPoolWithMigrations(t)
	if pool == nil {
		t.Skip("Postgres not available")
	}
	defer pool.Close()

	ctx := context.Background()
	wsID := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO workspaces (id, name) VALUES ($1, $2)`, wsID, "Check Constraint WS")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	repo := repository.NewIdempotencyRepository(pool)
	traceID := "trace_invalid_status"

	// Record ledger with an invalid status that violates CHECK constraint
	ledgerEntry := &repository.IngressLedgerEntry{
		WorkspaceID:    wsID,
		TraceID:        traceID,
		IdempotencyKey: "key_invalid_status",
		Channel:        "telegram",
		Recipient:      "12345",
		Status:         "invalid_enum_val",
	}

	err = repo.RecordLedger(ctx, ledgerEntry)
	if err == nil {
		t.Errorf("expected error when inserting invalid status violating CHECK constraint, got nil")
	}
}

func TestIdempotencyRepository_InvalidJSON(t *testing.T) {
	pool := getTestPoolWithMigrations(t)
	if pool == nil {
		t.Skip("Postgres not available")
	}
	defer pool.Close()

	ctx := context.Background()
	wsID := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO workspaces (id, name) VALUES ($1, $2)`, wsID, "Invalid JSON WS")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	repo := repository.NewIdempotencyRepository(pool)
	keyHash := "invalid_json_key_hash"
	traceID := "trace_invalid_json"

	inserted, err := repo.CheckAndStore(ctx, wsID, keyHash, traceID, 1*time.Hour)
	if err != nil || !inserted {
		t.Fatalf("CheckAndStore failed: %v", err)
	}

	// Update response with invalid JSON string bytes
	err = repo.UpdateResponse(ctx, wsID, keyHash, 400, []byte("not valid json"), nil)
	if err == nil {
		t.Errorf("expected error when storing invalid JSON in JSONB column, got nil")
	}
}

func TestIdempotencyRepository_NonExistentUpdates(t *testing.T) {
	pool := getTestPoolWithMigrations(t)
	if pool == nil {
		t.Skip("Postgres not available")
	}
	defer pool.Close()

	ctx := context.Background()
	wsID := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO workspaces (id, name) VALUES ($1, $2)`, wsID, "Non Existent WS")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	repo := repository.NewIdempotencyRepository(pool)

	// UpdateResponse for non-existent key should not error
	err = repo.UpdateResponse(ctx, wsID, "non_existent_key", 200, []byte(`{"ok":true}`), nil)
	if err != nil {
		t.Errorf("expected nil error for updating non-existent idempotency key, got %v", err)
	}

	// UpdateLedgerStatus for non-existent trace_id should not error
	err = repo.UpdateLedgerStatus(ctx, wsID, "non_existent_trace", "enqueued", nil)
	if err != nil {
		t.Errorf("expected nil error for updating non-existent ledger trace, got %v", err)
	}
}
