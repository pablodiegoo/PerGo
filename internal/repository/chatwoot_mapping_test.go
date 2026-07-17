package repository_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestChatwootMappingRepository(t *testing.T) {
	pool := getTestPoolWithMigrations(t)
	defer pool.Close()

	ctx := context.Background()

	// Clean up tables to avoid conflicts
	_, _ = pool.Exec(ctx, "DELETE FROM chatwoot_mappings")
	_, _ = pool.Exec(ctx, "DELETE FROM connections")
	_, _ = pool.Exec(ctx, "DELETE FROM contacts")
	_, _ = pool.Exec(ctx, "DELETE FROM workspaces")

	wsRepo := repository.NewWorkspaceRepository(pool)
	contactRepo := repository.NewContactRepository(pool)

	kek := make([]byte, 32)
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	connRepo := repository.NewConnectionRepository(pool, enc)
	repo := repository.NewChatwootMappingRepository(pool)

	// 1. Setup workspace, contact, and connection
	ws, err := wsRepo.Create(ctx, "chatwoot_mapping_ws")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	contact, err := contactRepo.ResolveContact(ctx, ws.ID, "telegram", "12345", "Test Contact", "testuser", "+12345")
	if err != nil {
		t.Fatalf("failed to resolve contact: %v", err)
	}

	conn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "Test Connection",
		Channel:        "telegram",
		SenderIdentity: "bot_username",
		Status:         "active",
	}
	err = connRepo.Create(ctx, conn)
	if err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}
	defer func() {
		_ = connRepo.Delete(ctx, conn.ID)
	}()

	// 2. Test GetByContactAndConnection on non-existing mapping
	_, err = repo.GetByContactAndConnection(ctx, ws.ID, contact.ID, conn.ID)
	if !errors.Is(err, repository.ErrChatwootMappingNotFound) {
		t.Errorf("expected ErrChatwootMappingNotFound, got: %v", err)
	}

	// 3. Test Upsert & GetByContactAndConnection
	m := &repository.ChatwootMapping{
		WorkspaceID:            ws.ID,
		ContactID:              contact.ID,
		ConnectionID:           conn.ID,
		ChatwootContactID:      999111,
		ChatwootConversationID: 888222,
		Channel:                "telegram",
		SenderIdentity:         "bot_username",
	}

	err = repo.Upsert(ctx, m)
	if err != nil {
		t.Fatalf("failed to upsert chatwoot mapping: %v", err)
	}

	retrieved, err := repo.GetByContactAndConnection(ctx, ws.ID, contact.ID, conn.ID)
	if err != nil {
		t.Fatalf("failed to retrieve chatwoot mapping: %v", err)
	}

	if retrieved.ChatwootContactID != 999111 {
		t.Errorf("got ChatwootContactID %d, want 999111", retrieved.ChatwootContactID)
	}
	if retrieved.ChatwootConversationID != 888222 {
		t.Errorf("got ChatwootConversationID %d, want 888222", retrieved.ChatwootConversationID)
	}

	// 4. Test GetByConversationID
	retrievedByConv, err := repo.GetByConversationID(ctx, ws.ID, 888222)
	if err != nil {
		t.Fatalf("failed to retrieve by conversation id: %v", err)
	}
	if retrievedByConv.ContactID != contact.ID {
		t.Errorf("got ContactID %v, want %v", retrievedByConv.ContactID, contact.ID)
	}

	// 5. Test ON CONFLICT updates fields
	m.ChatwootConversationID = 888333
	m.ChatwootContactID = 999222
	err = repo.Upsert(ctx, m)
	if err != nil {
		t.Fatalf("failed to upsert update: %v", err)
	}

	retrievedUpdated, err := repo.GetByContactAndConnection(ctx, ws.ID, contact.ID, conn.ID)
	if err != nil {
		t.Fatalf("failed to retrieve updated mapping: %v", err)
	}
	if retrievedUpdated.ChatwootConversationID != 888333 {
		t.Errorf("got updated ChatwootConversationID %d, want 888333", retrievedUpdated.ChatwootConversationID)
	}
	if retrievedUpdated.ChatwootContactID != 999222 {
		t.Errorf("got updated ChatwootContactID %d, want 999222", retrievedUpdated.ChatwootContactID)
	}

	// 6. Test Delete
	err = repo.Delete(ctx, ws.ID, contact.ID, conn.ID)
	if err != nil {
		t.Fatalf("failed to delete mapping: %v", err)
	}

	_, err = repo.GetByContactAndConnection(ctx, ws.ID, contact.ID, conn.ID)
	if !errors.Is(err, repository.ErrChatwootMappingNotFound) {
		t.Errorf("expected ErrChatwootMappingNotFound after delete, got: %v", err)
	}
}
