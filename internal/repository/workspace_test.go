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

	t.Run("EnsureWorkspace dynamically provisions when empty and reuses existing", func(t *testing.T) {
		// Clean up
		_, _ = pool.Exec(ctx, "DELETE FROM workspaces")

		// 1. Database contains 0 workspaces -> dynamic creation with random UUID
		ws1, err := repo.EnsureWorkspace(ctx, "Agora")
		if err != nil {
			t.Fatalf("unexpected error ensuring workspace on empty DB: %v", err)
		}
		if ws1 == nil {
			t.Fatal("expected workspace not to be nil")
		}
		if ws1.ID == uuid.Nil {
			t.Fatal("expected non-nil UUID for ensured workspace")
		}
		if ws1.Name != "Agora" {
			t.Errorf("expected name Agora, got %s", ws1.Name)
		}

		// 2. Database contains >= 1 workspace -> reuses existing without creating second or renaming
		ws2, err := repo.EnsureWorkspace(ctx, "DifferentName")
		if err != nil {
			t.Fatalf("unexpected error ensuring workspace on populated DB: %v", err)
		}
		if ws2.ID != ws1.ID {
			t.Errorf("expected ensured workspace to return existing ID %s, got %s", ws1.ID, ws2.ID)
		}
		if ws2.Name != "Agora" {
			t.Errorf("expected existing name Agora to be preserved, got %s", ws2.Name)
		}
	})

	t.Run("GetEarliest returns the earliest created workspace", func(t *testing.T) {
		// Clean up
		_, _ = pool.Exec(ctx, "DELETE FROM workspaces")

		// Empty DB returns ErrWorkspaceNotFound
		earliest, err := repo.GetEarliest(ctx)
		if err == nil || earliest != nil {
			t.Fatalf("expected error on empty DB, got ws=%v, err=%v", earliest, err)
		}

		// Create first workspace
		wsFirst, err := repo.Create(ctx, "First Workspace")
		if err != nil {
			t.Fatalf("failed to create first workspace: %v", err)
		}

		// Create second workspace
		_, err = repo.Create(ctx, "Second Workspace")
		if err != nil {
			t.Fatalf("failed to create second workspace: %v", err)
		}

		// GetEarliest should return wsFirst
		earliest, err = repo.GetEarliest(ctx)
		if err != nil {
			t.Fatalf("unexpected error getting earliest: %v", err)
		}
		if earliest.ID != wsFirst.ID {
			t.Errorf("expected earliest workspace ID %s, got %s", wsFirst.ID, earliest.ID)
		}
		if earliest.Name != "First Workspace" {
			t.Errorf("expected earliest workspace name 'First Workspace', got %s", earliest.Name)
		}
	})

	t.Run("SetFlowWebhookURL updates and retrieves flow_webhook_url correctly", func(t *testing.T) {
		ws, err := repo.Create(ctx, "Flow Workspace")
		if err != nil {
			t.Fatalf("failed to create workspace: %v", err)
		}
		if ws.FlowWebhookURL != nil {
			t.Fatalf("expected initial flow_webhook_url to be nil, got %v", ws.FlowWebhookURL)
		}

		flowURL := "https://example.com/api/flows/webhook"
		if err := repo.SetFlowWebhookURL(ctx, ws.ID, &flowURL); err != nil {
			t.Fatalf("SetFlowWebhookURL failed: %v", err)
		}

		fetched, err := repo.GetByID(ctx, ws.ID)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if fetched.FlowWebhookURL == nil || *fetched.FlowWebhookURL != flowURL {
			t.Errorf("expected FlowWebhookURL %q, got %v", flowURL, fetched.FlowWebhookURL)
		}

		// Clear URL
		if err := repo.SetFlowWebhookURL(ctx, ws.ID, nil); err != nil {
			t.Fatalf("SetFlowWebhookURL nil failed: %v", err)
		}
		cleared, err := repo.GetByID(ctx, ws.ID)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if cleared.FlowWebhookURL != nil {
			t.Errorf("expected FlowWebhookURL to be nil after clearing, got %v", cleared.FlowWebhookURL)
		}
	})
}
