package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/pablojhp.pergo/internal/platform/crypto"
)

func TestWebhookDLQRepository(t *testing.T) {
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

	repo := NewWebhookDLQRepository(pool, enc)

	// 2. Create two test workspaces (for isolation checks)
	wsRepo := NewWorkspaceRepository(pool)
	wsName1 := "webhook_test_ws_1_" + uuid.New().String()
	ws1, err := wsRepo.Create(ctx, wsName1)
	if err != nil {
		t.Fatalf("failed to create test workspace 1: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws1.ID) }()

	wsName2 := "webhook_test_ws_2_" + uuid.New().String()
	ws2, err := wsRepo.Create(ctx, wsName2)
	if err != nil {
		t.Fatalf("failed to create test workspace 2: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws2.ID) }()

	// --- 3. Test Webhook Config CRUD & Isolation ---
	testURL := "https://example.com/webhooks"
	testSecret := []byte("very-secure-webhook-secret-token")

	// Save config for WS1
	err = repo.SaveConfig(ctx, ws1.ID, testURL, testSecret)
	if err != nil {
		t.Fatalf("failed to save webhook config: %v", err)
	}

	// Verify DB is encrypted
	var dbSecret []byte
	err = pool.QueryRow(ctx, "SELECT secret FROM webhook_subscriptions WHERE workspace_id = $1", ws1.ID).Scan(&dbSecret)
	if err != nil {
		t.Fatalf("failed to query raw DB secret: %v", err)
	}
	if bytes.Equal(dbSecret, testSecret) {
		t.Error("expected DB secret to be encrypted, but matched plaintext")
	}

	// Get config and check values
	cfg, err := repo.GetConfig(ctx, ws1.ID)
	if err != nil {
		t.Fatalf("failed to retrieve config: %v", err)
	}
	if cfg.URL != testURL {
		t.Errorf("expected URL %q, got %q", testURL, cfg.URL)
	}
	if !bytes.Equal(cfg.Secret, testSecret) {
		t.Errorf("expected secret %q, got %q", string(testSecret), string(cfg.Secret))
	}

	// Check isolation: WS2 should not find WS1's config
	_, err = repo.GetConfig(ctx, ws2.ID)
	if !errors.Is(err, ErrWebhookConfigNotFound) {
		t.Errorf("expected ErrWebhookConfigNotFound for WS2, got: %v", err)
	}

	// --- 4. Test DLQ Operations ---
	traceID := "trace-123-abc"
	messageID := "msg-456-def"
	eventType := "failed"
	payload := []byte(`{"status":"failed","reason":"timeout"}`)
	attempts := 10
	failReason := "Gateway Timeout 504"

	// Insert into DLQ for WS1
	err = repo.InsertDLQ(ctx, ws1.ID, cfg.ID, traceID, messageID, eventType, payload, testURL, attempts, &failReason)
	if err != nil {
		t.Fatalf("failed to insert into DLQ: %v", err)
	}

	// Check Badge Count
	count, err := repo.GetDLQBadgeCount(ctx, ws1.ID)
	if err != nil {
		t.Fatalf("failed to get DLQ badge count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected DLQ count 1, got %d", count)
	}

	// WS2 badge count should be 0
	count2, err := repo.GetDLQBadgeCount(ctx, ws2.ID)
	if err != nil {
		t.Fatalf("failed to get WS2 DLQ badge count: %v", err)
	}
	if count2 != 0 {
		t.Errorf("expected WS2 DLQ count 0, got %d", count2)
	}

	// List DLQ items
	items, err := repo.ListDLQ(ctx, ws1.ID, 10, 0)
	if err != nil {
		t.Fatalf("failed to list DLQ items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 DLQ item, got %d", len(items))
	}

	item := items[0]
	if item.TraceID != traceID || item.MessageID != messageID || item.EventType != eventType || item.SubscriptionID != cfg.ID {
		t.Errorf("DLQ fields mismatch: %+v", item)
	}
	var expectedMap, actualMap map[string]interface{}
	if err := json.Unmarshal(payload, &expectedMap); err != nil {
		t.Fatalf("failed to unmarshal expected payload: %v", err)
	}
	if err := json.Unmarshal(item.Payload, &actualMap); err != nil {
		t.Fatalf("failed to unmarshal actual payload: %v", err)
	}
	for k, v := range expectedMap {
		if actualMap[k] != v {
			t.Errorf("expected payload key %s to be %v, got %v", k, v, actualMap[k])
		}
	}
	if item.FailureReason == nil || *item.FailureReason != failReason {
		t.Errorf("expected failure reason %s, got %v", failReason, item.FailureReason)
	}

	// Retrieve by ID
	fetchedItem, err := repo.GetDLQByID(ctx, item.ID)
	if err != nil {
		t.Fatalf("failed to get DLQ by ID: %v", err)
	}
	if fetchedItem.ID != item.ID {
		t.Errorf("expected item ID %s, got %s", item.ID, fetchedItem.ID)
	}

	// Delete DLQ item
	err = repo.DeleteDLQ(ctx, item.ID)
	if err != nil {
		t.Fatalf("failed to delete DLQ item: %v", err)
	}

	// Badge count should be 0 now
	count, err = repo.GetDLQBadgeCount(ctx, ws1.ID)
	if err != nil {
		t.Fatalf("failed to get DLQ count after delete: %v", err)
	}
	if count != 0 {
		t.Errorf("expected DLQ count 0 after delete, got %d", count)
	}
}
