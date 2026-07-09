package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestTelegramContactRepository(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := NewWorkspaceRepository(pool)
	repo := NewTelegramContactRepository(pool)

	ws, err := wsRepo.Create(ctx, "tg_contact_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	chatID := "987654321"
	username := "@pablo_test"
	phone := "+5511988887777"
	first := "Pablo"
	last := "Test"

	// 1. Resolve non-existent contact
	_, err = repo.Resolve(ctx, ws.ID, username)
	if !errors.Is(err, ErrTelegramContactNotFound) {
		t.Errorf("expected ErrTelegramContactNotFound, got: %v", err)
	}

	// 2. Upsert mapping
	err = repo.Upsert(ctx, ws.ID, chatID, &username, &phone, &first, &last)
	if err != nil {
		t.Fatalf("failed to upsert contact: %v", err)
	}

	// 3. Resolve by username (with @)
	resID, err := repo.Resolve(ctx, ws.ID, "@pablo_test")
	if err != nil {
		t.Fatalf("resolve by username with @ failed: %v", err)
	}
	if resID != chatID {
		t.Errorf("got resolved ID %s, want %s", resID, chatID)
	}

	// 4. Resolve by username (without @, case-insensitive)
	resID, err = repo.Resolve(ctx, ws.ID, "PABLO_TEST")
	if err != nil {
		t.Fatalf("resolve by username case-insensitive failed: %v", err)
	}
	if resID != chatID {
		t.Errorf("got resolved ID %s, want %s", resID, chatID)
	}

	// 5. Resolve by phone number
	resID, err = repo.Resolve(ctx, ws.ID, "+5511988887777")
	if err != nil {
		t.Fatalf("resolve by phone number failed: %v", err)
	}
	if resID != chatID {
		t.Errorf("got resolved ID %s, want %s", resID, chatID)
	}

	// 6. Resolve pure numeric chat ID (should return as-is directly)
	resID, err = repo.Resolve(ctx, ws.ID, "987654321")
	if err != nil {
		t.Fatalf("resolve pure numeric failed: %v", err)
	}
	if resID != "987654321" {
		t.Errorf("got resolved ID %s, want %s", resID, "987654321")
	}

	// 7. Resolve negative numeric chat ID (Telegram group chat IDs are negative)
	resID, err = repo.Resolve(ctx, ws.ID, "-100123456789")
	if err != nil {
		t.Fatalf("resolve negative numeric failed: %v", err)
	}
	if resID != "-100123456789" {
		t.Errorf("got resolved ID %s, want %s", resID, "-100123456789")
	}

	// 8. Get contact details
	contact, err := repo.Get(ctx, ws.ID, chatID)
	if err != nil {
		t.Fatalf("failed to Get contact: %v", err)
	}
	if contact.Username == nil || *contact.Username != "pablo_test" {
		t.Errorf("got username %v, want %s", contact.Username, "pablo_test")
	}
	if contact.FirstName == nil || *contact.FirstName != first {
		t.Errorf("got first name %v, want %s", contact.FirstName, first)
	}
	if contact.LastName == nil || *contact.LastName != last {
		t.Errorf("got last name %v, want %s", contact.LastName, last)
	}
}

