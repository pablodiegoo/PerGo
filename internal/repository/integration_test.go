package repository

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/platform/crypto"
)

func TestIntegrationRepository(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := NewWorkspaceRepository(pool)

	// Create test workspace
	ws, err := wsRepo.Create(ctx, "integration_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	// 1. Setup Encryptor
	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(i)
	}
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	repo := NewIntegrationRepository(pool, enc)

	// 2. Test GetByProvider of non-existent integration
	_, err = repo.GetByProvider(ctx, ws.ID, "chatwoot")
	if !errors.Is(err, ErrIntegrationNotFound) {
		t.Errorf("expected ErrIntegrationNotFound, got: %v", err)
	}

	// 3. Test Save & GetByProvider
	plaintextConfig := []byte(`{"api_url":"https://chatwoot.example.com","access_token":"secret-token","inbox_id":123,"account_id":456}`)
	integration := &Integration{
		ID:          uuid.New(),
		WorkspaceID: ws.ID,
		Name:        "Chatwoot Production",
		Provider:    "chatwoot",
		Active:      true,
		Config:      plaintextConfig,
	}

	err = repo.Save(ctx, integration)
	if err != nil {
		t.Fatalf("failed to save integration: %v", err)
	}

	// Verify database content is encrypted
	var dbConfig []byte
	var dbKeyID string
	var dbKeyVersion int
	err = pool.QueryRow(ctx, "SELECT config, key_id, key_version FROM integrations WHERE id = $1", integration.ID).Scan(&dbConfig, &dbKeyID, &dbKeyVersion)
	if err != nil {
		t.Fatalf("failed to query database row: %v", err)
	}

	if bytes.Equal(dbConfig, plaintextConfig) {
		t.Error("expected config stored in database to be encrypted, but it was stored as plaintext")
	}

	if dbKeyID == "" {
		t.Error("expected key_id to be populated, but it was empty")
	}

	// Retrieve integration and verify decryption
	retrieved, err := repo.GetByProvider(ctx, ws.ID, "chatwoot")
	if err != nil {
		t.Fatalf("failed to retrieve integration: %v", err)
	}

	if retrieved.ID != integration.ID {
		t.Errorf("got ID %v, want %v", retrieved.ID, integration.ID)
	}
	if retrieved.Name != integration.Name {
		t.Errorf("got Name %s, want %s", retrieved.Name, integration.Name)
	}
	if retrieved.Provider != integration.Provider {
		t.Errorf("got Provider %s, want %s", retrieved.Provider, integration.Provider)
	}
	if retrieved.Active != integration.Active {
		t.Errorf("got Active %t, want %t", retrieved.Active, integration.Active)
	}
	if !bytes.Equal(retrieved.Config, plaintextConfig) {
		t.Errorf("got Config %s, want %s", string(retrieved.Config), string(plaintextConfig))
	}

	// 4. Test Update ON CONFLICT (workspace_id, provider)
	updatedConfig := []byte(`{"api_url":"https://chatwoot.example.com","access_token":"updated-token","inbox_id":123,"account_id":456}`)
	integration.Name = "Chatwoot Updated"
	integration.Config = updatedConfig

	err = repo.Save(ctx, integration)
	if err != nil {
		t.Fatalf("failed to update integration: %v", err)
	}

	retrieved2, err := repo.GetByProvider(ctx, ws.ID, "chatwoot")
	if err != nil {
		t.Fatalf("failed to retrieve updated integration: %v", err)
	}

	if retrieved2.Name != "Chatwoot Updated" {
		t.Errorf("got updated Name %s, want %s", retrieved2.Name, "Chatwoot Updated")
	}
	if !bytes.Equal(retrieved2.Config, updatedConfig) {
		t.Errorf("got updated Config %s, want %s", string(retrieved2.Config), string(updatedConfig))
	}
}
