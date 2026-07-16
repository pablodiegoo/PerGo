package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/platform/crypto"
)

func TestWABATemplateRepository(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	// 1. Setup repos & encryptor for connections
	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(i)
	}
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	wsRepo := NewWorkspaceRepository(pool)
	connRepo := NewConnectionRepository(pool, enc)
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

	// 3. Create a test connection
	conn := &Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "WABA test connection",
		Channel:        "whatsapp_cloud",
		SenderIdentity: "5511999990003",
		Status:         "connected",
		Credentials:    []byte(`{"phone_number_id":"123","waba_account_id":"456","token":"token"}`),
	}
	if err := connRepo.Create(ctx, conn); err != nil {
		t.Fatalf("failed to create test connection: %v", err)
	}

	// 4. Test Create
	tmpl := &WABATemplate{
		WorkspaceID:    ws.ID,
		ConnectionID:   conn.ID,
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
	if created.ConnectionID != conn.ID {
		t.Errorf("expected connection ID %s, got %s", conn.ID, created.ConnectionID)
	}
	if created.Name != "welcome_template" {
		t.Errorf("expected name welcome_template, got %s", created.Name)
	}

	// 5. Test GetByID
	retrieved, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("failed to get template by ID: %v", err)
	}
	if retrieved.MetaTemplateID != "meta-id-123" {
		t.Errorf("expected meta template ID meta-id-123, got %s", retrieved.MetaTemplateID)
	}

	// 6. Test GetByNameAndLanguage
	retrievedByName, err := repo.GetByNameAndLanguage(ctx, conn.ID, "welcome_template", "en")
	if err != nil {
		t.Fatalf("failed to get template by name/language: %v", err)
	}
	if retrievedByName.ID != created.ID {
		t.Errorf("expected template ID %s, got %s", created.ID, retrievedByName.ID)
	}

	// 7. Test UpdateStatus
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

	// 8. Test ListByWorkspace
	list, err := repo.ListByWorkspace(ctx, ws.ID)
	if err != nil {
		t.Fatalf("failed to list templates by workspace: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 template in workspace list, got %d", len(list))
	}
	if list[0].ID != created.ID {
		t.Errorf("expected list element ID %s, got %s", created.ID, list[0].ID)
	}

	// 9. Test ListByConnection
	listByConn, err := repo.ListByConnection(ctx, conn.ID)
	if err != nil {
		t.Fatalf("failed to list templates by connection: %v", err)
	}
	if len(listByConn) != 1 {
		t.Errorf("expected 1 template in connection list, got %d", len(listByConn))
	}
	if listByConn[0].ID != created.ID {
		t.Errorf("expected list element ID %s, got %s", created.ID, listByConn[0].ID)
	}

	// 10. Test Delete
	err = repo.Delete(ctx, created.ID)
	if err != nil {
		t.Fatalf("failed to delete template: %v", err)
	}

	_, err = repo.GetByID(ctx, created.ID)
	if err == nil {
		t.Error("expected error retrieving deleted template, got nil")
	}
}
