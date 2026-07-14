package repository_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestAPIKeyRepository_CountActive(t *testing.T) {
	pool := getTestPoolWithMigrations(t)
	defer pool.Close()

	ctx := context.Background()

	// Clean up api_keys and workspaces
	_, _ = pool.Exec(ctx, "DELETE FROM api_keys")
	_, _ = pool.Exec(ctx, "DELETE FROM workspaces")

	repo := repository.NewAPIKeyRepository(pool)
	wsRepo := repository.NewWorkspaceRepository(pool)

	// Create test workspace
	ws, err := wsRepo.Create(ctx, "apikey_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	// 1. Initially active keys count should be 0
	count, err := repo.CountActive(ctx, ws.ID)
	if err != nil {
		t.Fatalf("failed to count active keys: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 active keys, got %d", count)
	}

	// 2. Create active key
	key1, _, err := repo.Create(ctx, ws.ID, "Key 1")
	if err != nil {
		t.Fatalf("failed to create API key: %v", err)
	}

	count, err = repo.CountActive(ctx, ws.ID)
	if err != nil {
		t.Fatalf("failed to count active keys: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 active key, got %d", count)
	}

	// 3. Create another active key
	key2, _, err := repo.Create(ctx, ws.ID, "Key 2")
	if err != nil {
		t.Fatalf("failed to create second API key: %v", err)
	}

	count, err = repo.CountActive(ctx, ws.ID)
	if err != nil {
		t.Fatalf("failed to count active keys: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 active keys, got %d", count)
	}

	// 4. Revoke one key and check count
	err = repo.Revoke(ctx, key1.ID)
	if err != nil {
		t.Fatalf("failed to revoke API key: %v", err)
	}

	count, err = repo.CountActive(ctx, ws.ID)
	if err != nil {
		t.Fatalf("failed to count active keys: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 active key after revoking one, got %d", count)
	}

	// 5. Revoke the second key
	err = repo.Revoke(ctx, key2.ID)
	if err != nil {
		t.Fatalf("failed to revoke second API key: %v", err)
	}

	count, err = repo.CountActive(ctx, ws.ID)
	if err != nil {
		t.Fatalf("failed to count active keys: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 active keys after revoking all, got %d", count)
	}
}
