package repository_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestWorkspaceRepository_CreateWithID_And_GetByName(t *testing.T) {
	pool := getTestPoolWithMigrations(t)
	defer pool.Close()

	ctx := context.Background()

	// Clean up
	_, _ = pool.Exec(ctx, "DELETE FROM workspaces")

	repo := repository.NewWorkspaceRepository(pool)
	deterministicID := uuid.MustParse("a0000000-0000-0000-0000-000000000001")

	t.Run("CreateWithID creates new workspace with deterministic UUID", func(t *testing.T) {
		ws, err := repo.CreateWithID(ctx, deterministicID, "Agora")
		if err != nil {
			t.Fatalf("unexpected error creating workspace with ID: %v", err)
		}
		if ws == nil {
			t.Fatal("expected workspace not to be nil")
		}
		if ws.ID != deterministicID {
			t.Errorf("expected ID %s, got %s", deterministicID, ws.ID)
		}
		if ws.Name != "Agora" {
			t.Errorf("expected Name 'Agora', got %s", ws.Name)
		}
	})

	t.Run("CreateWithID is idempotent on conflict", func(t *testing.T) {
		ws, err := repo.CreateWithID(ctx, deterministicID, "Agora Updated")
		if err != nil {
			t.Fatalf("unexpected error on idempotent call: %v", err)
		}
		if ws.ID != deterministicID {
			t.Errorf("expected ID %s, got %s", deterministicID, ws.ID)
		}
		if ws.Name != "Agora Updated" {
			t.Errorf("expected Name 'Agora Updated', got %s", ws.Name)
		}
	})

	t.Run("GetByName retrieves workspace by name", func(t *testing.T) {
		ws, err := repo.GetByName(ctx, "Agora Updated")
		if err != nil {
			t.Fatalf("unexpected error getting workspace by name: %v", err)
		}
		if ws == nil || ws.ID != deterministicID {
			t.Errorf("expected workspace with ID %s, got %+v", deterministicID, ws)
		}

		// Non-existent
		missing, err := repo.GetByName(ctx, "NonExistent")
		if err == nil {
			t.Errorf("expected error for non-existent workspace name, got nil (ws: %+v)", missing)
		}
	})
}
