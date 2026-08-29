package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/domain"
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
	key := domain.NewSessionKey(ws.ID, recipient, channelName, recipientIdentity)

	// Get non-existent session
	_, err = repo.Get(ctx, key)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got: %v", err)
	}

	// Upsert session
	err = repo.Upsert(ctx, key, now, "ctwa")
	if err != nil {
		t.Fatalf("failed to upsert session: %v", err)
	}

	// Get existing session
	sess, err := repo.Get(ctx, key)
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
	err = repo.Upsert(ctx, key, newTime, "standard")
	if err != nil {
		t.Fatalf("failed to update/upsert session: %v", err)
	}

	sess2, err := repo.Get(ctx, key)
	if err != nil {
		t.Fatalf("failed to get updated session: %v", err)
	}
	if !sess2.LastInboundAt.Equal(newTime) {
		t.Errorf("got updated LastInboundAt %v, want %v", sess2.LastInboundAt, newTime)
	}
	if sess2.EntryPointType != "standard" {
		t.Errorf("got updated EntryPointType %s, want standard", sess2.EntryPointType)
	}

	// Test RecordOutbound on existing session
	outboundTime := now.Add(2 * time.Hour)
	err = repo.RecordOutbound(ctx, key, outboundTime)
	if err != nil {
		t.Fatalf("failed to record outbound: %v", err)
	}

	sessOutbound, err := repo.Get(ctx, key)
	if err != nil {
		t.Fatalf("failed to get session after recording outbound: %v", err)
	}
	if sessOutbound.LastOutboundAt == nil {
		t.Fatal("expected LastOutboundAt to be set, got nil")
	}
	if !sessOutbound.LastOutboundAt.Equal(outboundTime) {
		t.Errorf("got LastOutboundAt %v, want %v", *sessOutbound.LastOutboundAt, outboundTime)
	}
	// Verify LastInboundAt was preserved
	if !sessOutbound.LastInboundAt.Equal(newTime) {
		t.Errorf("expected LastInboundAt %v preserved, got %v", newTime, sessOutbound.LastInboundAt)
	}

	// Test RecordOutbound on new recipient without prior session
	newRecipient := "+5511998877665"
	newOutboundTime := now.Add(3 * time.Hour)
	newKey := domain.NewSessionKey(ws.ID, newRecipient, channelName, recipientIdentity)
	err = repo.RecordOutbound(ctx, newKey, newOutboundTime)
	if err != nil {
		t.Fatalf("failed to record outbound for new recipient: %v", err)
	}

	newSess, err := repo.Get(ctx, newKey)
	if err != nil {
		t.Fatalf("failed to get new session: %v", err)
	}
	if newSess.LastOutboundAt == nil {
		t.Fatal("expected LastOutboundAt on new session to be set")
	}
	if !newSess.LastOutboundAt.Equal(newOutboundTime) {
		t.Errorf("got new session LastOutboundAt %v, want %v", *newSess.LastOutboundAt, newOutboundTime)
	}

	// Test phone normalization (e.g. without plus) updates existing session
	trimmedRecipient := "5511998877665"
	updatedOutboundTime := now.Add(4 * time.Hour)
	trimmedKey := domain.NewSessionKey(ws.ID, trimmedRecipient, channelName, recipientIdentity)
	err = repo.RecordOutbound(ctx, trimmedKey, updatedOutboundTime)
	if err != nil {
		t.Fatalf("failed to record outbound with trimmed recipient: %v", err)
	}
	updatedSess, err := repo.Get(ctx, newKey)
	if err != nil {
		t.Fatalf("failed to get updated session: %v", err)
	}
	if !updatedSess.LastOutboundAt.Equal(updatedOutboundTime) {
		t.Errorf("expected updated LastOutboundAt %v, got %v", updatedOutboundTime, *updatedSess.LastOutboundAt)
	}
}
