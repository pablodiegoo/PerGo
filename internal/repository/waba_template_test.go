package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestWABATemplateRepository(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	// 1. Setup repos
	wsRepo := NewWorkspaceRepository(pool)
	repo := NewWABATemplateRepository(pool)

	// 2. Create a test workspace
	wsName := "template_test_ws_" + uuid.New().String()
	ws, err := wsRepo.Create(ctx, wsName)
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	// 3. Test Create
	tmpl := &WABATemplate{
		WorkspaceID:    ws.ID,
		MetaTemplateID: "meta-id-123",
		Name:           "welcome_template",
		Language:       "en",
		Status:         "PENDING",
		Category:       "UTILITY",
		Components:     json.RawMessage(`[{"type":"BODY","text":"Hello World"}]`),
	}

	created, err := repo.Create(ctx, tmpl)
	if err != nil {
		t.Fatalf("failed to create WABA template: %v", err)
	}

	if created.ID == uuid.Nil {
		t.Error("expected non-nil UUID for created template")
	}
	if created.WorkspaceID != ws.ID {
		t.Errorf("expected workspace ID %s, got %s", ws.ID, created.WorkspaceID)
	}
	if created.Name != "welcome_template" {
		t.Errorf("expected name welcome_template, got %s", created.Name)
	}

	// 4. Test GetByID
	retrieved, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("failed to get template by ID: %v", err)
	}
	if retrieved.MetaTemplateID != "meta-id-123" {
		t.Errorf("expected meta template ID meta-id-123, got %s", retrieved.MetaTemplateID)
	}

	// 5. Test GetByNameAndLanguage
	retrievedByName, err := repo.GetByNameAndLanguage(ctx, ws.ID, "welcome_template", "en")
	if err != nil {
		t.Fatalf("failed to get template by name/language: %v", err)
	}
	if retrievedByName.ID != created.ID {
		t.Errorf("expected template ID %s, got %s", created.ID, retrievedByName.ID)
	}

	// 6. Test UpdateStatus
	err = repo.UpdateStatus(ctx, created.ID, "APPROVED")
	if err != nil {
		t.Fatalf("failed to update status: %v", err)
	}

	retrievedUpdated, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("failed to get template after update: %v", err)
	}
	if retrievedUpdated.Status != "APPROVED" {
		t.Errorf("expected status APPROVED, got %s", retrievedUpdated.Status)
	}

	// 7. Test ListByWorkspace
	list, err := repo.ListByWorkspace(ctx, ws.ID)
	if err != nil {
		t.Fatalf("failed to list templates: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 template in list, got %d", len(list))
	}
	if list[0].ID != created.ID {
		t.Errorf("expected list element ID %s, got %s", created.ID, list[0].ID)
	}

	// 8. Test Delete
	err = repo.Delete(ctx, created.ID)
	if err != nil {
		t.Fatalf("failed to delete template: %v", err)
	}

	_, err = repo.GetByID(ctx, created.ID)
	if err == nil {
		t.Error("expected error retrieving deleted template, got nil")
	}
}
