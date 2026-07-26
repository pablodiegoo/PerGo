package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRecipientSessionRepository(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := NewWorkspaceRepository(pool)
	repo := NewRecipientSessionRepository(pool)

	// Create test workspace
	ws, err := wsRepo.Create(ctx, "session_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	recipient := "+1234567890"
	channelName := "whatsapp_cloud"
	recipientIdentity := "+5511999990001"
	now := time.Now().Truncate(time.Microsecond).UTC() // Postgres timestamptz truncation

	// Get non-existent session
	_, err = repo.Get(ctx, ws.ID, recipient, channelName, recipientIdentity)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got: %v", err)
	}

	// Upsert session
	err = repo.Upsert(ctx, ws.ID, recipient, channelName, recipientIdentity, now, "ctwa")
	if err != nil {
		t.Fatalf("failed to upsert session: %v", err)
	}

	// Get existing session
	sess, err := repo.Get(ctx, ws.ID, recipient, channelName, recipientIdentity)
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}

	if sess.WorkspaceID != ws.ID {
		t.Errorf("got WorkspaceID %v, want %v", sess.WorkspaceID, ws.ID)
	}
	if sess.RecipientPhone != recipient {
		t.Errorf("got RecipientPhone %s, want %s", sess.RecipientPhone, recipient)
	}
	if sess.Channel != channelName {
		t.Errorf("got Channel %s, want %s", sess.Channel, channelName)
	}
	if sess.RecipientIdentity != recipientIdentity {
		t.Errorf("got RecipientIdentity %s, want %s", sess.RecipientIdentity, recipientIdentity)
	}
	if !sess.LastInboundAt.Equal(now) {
		t.Errorf("got LastInboundAt %v, want %v", sess.LastInboundAt, now)
	}
	if sess.EntryPointType != "ctwa" {
		t.Errorf("got EntryPointType %s, want ctwa", sess.EntryPointType)
	}

	// Upsert again to update timestamp and reset entry_point_type
	newTime := now.Add(1 * time.Hour)
	err = repo.Upsert(ctx, ws.ID, recipient, channelName, recipientIdentity, newTime, "standard")
	if err != nil {
		t.Fatalf("failed to update/upsert session: %v", err)
	}

	sess2, err := repo.Get(ctx, ws.ID, recipient, channelName, recipientIdentity)
	if err != nil {
		t.Fatalf("failed to get updated session: %v", err)
	}
	if !sess2.LastInboundAt.Equal(newTime) {
		t.Errorf("got updated LastInboundAt %v, want %v", sess2.LastInboundAt, newTime)
	}
	if sess2.EntryPointType != "standard" {
		t.Errorf("got updated EntryPointType %s, want standard", sess2.EntryPointType)
	}

	// Test GetExpiringSessions and MarkNotifiedExpiring
	expiring, err := repo.GetExpiringSessions(ctx, newTime.Add(-2*time.Hour), newTime.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("failed to get expiring sessions: %v", err)
	}
	if len(expiring) == 0 {
		t.Errorf("expected at least 1 expiring session, got 0")
	}

	err = repo.MarkNotifiedExpiring(ctx, ws.ID, recipient, channelName, recipientIdentity, time.Now().UTC())
	if err != nil {
		t.Fatalf("failed to mark notified expiring: %v", err)
	}

	expiringAfter, err := repo.GetExpiringSessions(ctx, newTime.Add(-2*time.Hour), newTime.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("failed to get expiring sessions after mark: %v", err)
	}
	for _, s := range expiringAfter {
		if s.RecipientPhone == recipient && s.WorkspaceID == ws.ID {
			t.Errorf("expected session to be excluded from expiring sessions after marking")
		}
	}
}
