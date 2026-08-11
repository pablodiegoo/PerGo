package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestIdempotencyRepository(t *testing.T) {
	pool := getTestPoolWithMigrations(t)
	if pool == nil {
		t.Skip("Postgres not available")
	}
	defer pool.Close()

	ctx := context.Background()

	// Clean up
	_, _ = pool.Exec(ctx, "DELETE FROM message_ingress_ledger")
	_, _ = pool.Exec(ctx, "DELETE FROM message_idempotency")
	_, _ = pool.Exec(ctx, "DELETE FROM workspaces")

	wsID := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO workspaces (id, name) VALUES ($1, $2)`, wsID, "Test Idempotency WS")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	repo := repository.NewIdempotencyRepository(pool)
	keyHash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	traceID := "trace_idempotency_123"

	t.Run("CheckAndStore_And_Get", func(t *testing.T) {
		inserted, err := repo.CheckAndStore(ctx, wsID, keyHash, traceID, 1*time.Hour)
		if err != nil {
			t.Fatalf("CheckAndStore failed: %v", err)
		}
		if !inserted {
			t.Errorf("expected inserted=true for new key")
		}

		// Conflict test
		insertedAgain, err := repo.CheckAndStore(ctx, wsID, keyHash, "trace_diff", 1*time.Hour)
		if err != nil {
			t.Fatalf("CheckAndStore second time failed: %v", err)
		}
		if insertedAgain {
			t.Errorf("expected inserted=false for duplicate key")
		}

		entry, err := repo.GetByIdempotencyKey(ctx, wsID, keyHash)
		if err != nil {
			t.Fatalf("GetByIdempotencyKey failed: %v", err)
		}
		if entry.KeyHash != keyHash || entry.TraceID != traceID {
			t.Errorf("unexpected entry: %+v", entry)
		}
	})

	t.Run("UpdateResponse", func(t *testing.T) {
		respBody := []byte(`{"status":"accepted","message_id":"msg_123"}`)
		providerMsgID := "wamid.HBgLMTIz"
		err := repo.UpdateResponse(ctx, wsID, keyHash, 202, respBody, &providerMsgID)
		if err != nil {
			t.Fatalf("UpdateResponse failed: %v", err)
		}

		entry, err := repo.GetByIdempotencyKey(ctx, wsID, keyHash)
		if err != nil {
			t.Fatalf("GetByIdempotencyKey failed: %v", err)
		}
		if entry.StatusCode == nil || *entry.StatusCode != 202 {
			t.Errorf("expected status_code 202, got %v", entry.StatusCode)
		}
		if entry.ProviderMessageID == nil || *entry.ProviderMessageID != providerMsgID {
			t.Errorf("expected provider_message_id %s, got %v", providerMsgID, entry.ProviderMessageID)
		}
	})

	t.Run("Record_And_Update_Ledger", func(t *testing.T) {
		ledgerEntry := &repository.IngressLedgerEntry{
			WorkspaceID:    wsID,
			TraceID:        traceID,
			IdempotencyKey: keyHash,
			Channel:        "whatsapp_cloud",
			Recipient:      "5511999998888",
			Status:         "accepted",
		}
		err := repo.RecordLedger(ctx, ledgerEntry)
		if err != nil {
			t.Fatalf("RecordLedger failed: %v", err)
		}

		err = repo.UpdateLedgerStatus(ctx, wsID, traceID, "enqueued", nil)
		if err != nil {
			t.Fatalf("UpdateLedgerStatus failed: %v", err)
		}
	})

	t.Run("CleanupExpired", func(t *testing.T) {
		cleaned, err := repo.CleanupExpired(ctx)
		if err != nil {
			t.Fatalf("CleanupExpired failed: %v", err)
		}
		_ = cleaned
	})
}
