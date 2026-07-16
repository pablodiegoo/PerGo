package repository

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/pablojhp.pergo/internal/platform/crypto"
)

func TestWebhookSubscriptionRepository(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	// 1. Setup Encryptor
	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(i)
	}
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	repo := NewWebhookSubscriptionRepository(pool, enc)
	wsRepo := NewWorkspaceRepository(pool)

	// Create test workspace
	wsName := "webhook_sub_test_ws_" + uuid.New().String()
	ws, err := wsRepo.Create(ctx, wsName)
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	testURL := "https://example.com/webhooks/subs"
	testSecret := []byte("secret-token-for-sub")
	eventTypes := []string{"message.received", "message.sent"}

	// 2. Test Create
	sub, err := repo.Create(ctx, ws.ID, testURL, eventTypes, testSecret)
	if err != nil {
		t.Fatalf("failed to create webhook subscription: %v", err)
	}

	if sub.ID == uuid.Nil {
		t.Error("expected non-nil subscription ID")
	}
	if sub.WorkspaceID != ws.ID {
		t.Errorf("expected workspace ID %s, got %s", ws.ID, sub.WorkspaceID)
	}
	if sub.URL != testURL {
		t.Errorf("expected URL %q, got %q", testURL, sub.URL)
	}
	if !bytes.Equal(sub.Secret, testSecret) {
		t.Errorf("expected secret %q, got %q", string(testSecret), string(sub.Secret))
	}
	if len(sub.EventTypes) != 2 || sub.EventTypes[0] != "message.received" || sub.EventTypes[1] != "message.sent" {
		t.Errorf("unexpected event types: %v", sub.EventTypes)
	}
	if !sub.Active {
		t.Error("expected subscription to be active by default")
	}

	// Verify database encrypts the secret
	var dbSecret []byte
	err = pool.QueryRow(ctx, "SELECT secret FROM webhook_subscriptions WHERE id = $1", sub.ID).Scan(&dbSecret)
	if err != nil {
		t.Fatalf("failed to query raw DB secret: %v", err)
	}
	if bytes.Equal(dbSecret, testSecret) {
		t.Error("expected DB secret to be encrypted, but matched plaintext")
	}

	// 3. Test Get
	fetched, err := repo.Get(ctx, sub.ID)
	if err != nil {
		t.Fatalf("failed to get subscription: %v", err)
	}
	if fetched.ID != sub.ID {
		t.Errorf("expected ID %s, got %s", sub.ID, fetched.ID)
	}
	if !bytes.Equal(fetched.Secret, testSecret) {
		t.Errorf("expected decrypted secret %q, got %q", string(testSecret), string(fetched.Secret))
	}

	// 4. Test ListByWorkspace
	list, err := repo.ListByWorkspace(ctx, ws.ID)
	if err != nil {
		t.Fatalf("failed to list subscriptions: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(list))
	}
	if list[0].ID != sub.ID {
		t.Errorf("expected listed ID %s, got %s", sub.ID, list[0].ID)
	}

	// 5. Test Update url, event_types, active (without secret update)
	newURL := "https://example.com/updated"
	newEvents := []string{"*"}
	err = repo.Update(ctx, sub.ID, newURL, newEvents, false, nil)
	if err != nil {
		t.Fatalf("failed to update subscription: %v", err)
	}

	fetched, err = repo.Get(ctx, sub.ID)
	if err != nil {
		t.Fatalf("failed to get subscription after update: %v", err)
	}
	if fetched.URL != newURL {
		t.Errorf("expected updated URL %q, got %q", newURL, fetched.URL)
	}
	if len(fetched.EventTypes) != 1 || fetched.EventTypes[0] != "*" {
		t.Errorf("expected updated events %v, got %v", newEvents, fetched.EventTypes)
	}
	if fetched.Active {
		t.Error("expected subscription to be inactive")
	}
	// Secret should remain unchanged
	if !bytes.Equal(fetched.Secret, testSecret) {
		t.Errorf("expected secret %q, got %q after update without secret", string(testSecret), string(fetched.Secret))
	}

	// 6. Test Update secret
	newSecret := []byte("new-updated-secret-key")
	err = repo.Update(ctx, sub.ID, newURL, newEvents, true, newSecret)
	if err != nil {
		t.Fatalf("failed to update subscription secret: %v", err)
	}

	fetched, err = repo.Get(ctx, sub.ID)
	if err != nil {
		t.Fatalf("failed to get subscription after secret update: %v", err)
	}
	if !bytes.Equal(fetched.Secret, newSecret) {
		t.Errorf("expected secret %q, got %q after updating secret", string(newSecret), string(fetched.Secret))
	}

	// 7. Test Delete
	err = repo.Delete(ctx, sub.ID)
	if err != nil {
		t.Fatalf("failed to delete subscription: %v", err)
	}

	_, err = repo.Get(ctx, sub.ID)
	if !errors.Is(err, ErrWebhookSubscriptionNotFound) {
		t.Errorf("expected ErrWebhookSubscriptionNotFound, got: %v", err)
	}
}
