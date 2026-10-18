package inbound_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestInboundProcessor_ChatIngress(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	// Clean up
	_, _ = pool.Exec(ctx, "DELETE FROM chat_messages")
	_, _ = pool.Exec(ctx, "DELETE FROM chats")
	_, _ = pool.Exec(ctx, "DELETE FROM contact_identities")
	_, _ = pool.Exec(ctx, "DELETE FROM contacts")
	_, _ = pool.Exec(ctx, "DELETE FROM connections")
	_, _ = pool.Exec(ctx, "DELETE FROM workspaces")

	wsRepo := repository.NewWorkspaceRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	chatRepo := repository.NewChatRepository(pool)

	ws, err := wsRepo.Create(ctx, "chat_inbound_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	connID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO connections (id, workspace_id, name, slug, channel, sender_identity, status, credentials, is_default)
		VALUES ($1, $2, 'Inbound Cloud', 'inbound-cloud', 'whatsapp_cloud', 'bot123', 'active', '{}', true)
	`, connID, ws.ID)
	if err != nil {
		t.Fatalf("failed to insert connection: %v", err)
	}

	proc := inbound.NewInboundProcessor(nil, wsRepo, nil, nil, nil, nil, contactRepo, nil, nil)
	proc.SetChatRepository(chatRepo)

	msgID := "wamid-tracer-001"
	event := &inbound.InboundEvent{
		WorkspaceID:  ws.ID,
		ConnectionID: connID,
		MessageID:    msgID,
		TraceID:      "trace-001",
		Channel:      "whatsapp_cloud",
		From:         "+5511988887777",
		To:           "bot123",
		Body:         "Hello from customer!",
		SenderName:   "Customer Bob",
		OccurredAt:   time.Now().UTC(),
	}

	if err := proc.Process(ctx, event); err != nil {
		t.Fatalf("failed to process inbound event: %v", err)
	}

	// Verify contact was created
	contact, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp_cloud", "+5511988887777", "", "", "")
	if err != nil {
		t.Fatalf("failed to resolve contact: %v", err)
	}

	// Verify chat was created
	chat, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &connID, contact.ID)
	if err != nil {
		t.Fatalf("failed to get chat: %v", err)
	}
	if chat.UnreadCount != 1 {
		t.Errorf("expected unread count 1, got %d", chat.UnreadCount)
	}
	if chat.ServiceWindowExpiresAt == nil {
		t.Errorf("expected service window to be set for whatsapp_cloud")
	}

	// Verify chat message was stored
	msgs, err := chatRepo.ListChatMessages(ctx, ws.ID, chat.ID, "", "", 10)
	if err != nil {
		t.Fatalf("failed to list chat messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 chat message, got %d", len(msgs))
	}
	if msgs[0].UID != msgID {
		t.Errorf("expected message UID %s, got %s", msgID, msgs[0].UID)
	}
	if msgs[0].Direction != domain.DirectionInbound {
		t.Errorf("expected direction inbound, got %s", msgs[0].Direction)
	}
	if msgs[0].Body != "Hello from customer!" {
		t.Errorf("expected body 'Hello from customer!', got %s", msgs[0].Body)
	}

	// Test 72h window on CTWA
	now := time.Now().UTC()
	ctwaEvent := &inbound.InboundEvent{
		WorkspaceID:  ws.ID,
		ConnectionID: connID,
		MessageID:    "wamid-ctwa-001",
		TraceID:      "trace-ctwa-001",
		Channel:      "whatsapp_cloud",
		From:         "+5511988887777",
		To:           "bot123",
		Body:         "Hello from ad!",
		OccurredAt:   now,
		Metadata:     map[string]string{"entry_point_type": "ctwa"},
	}
	if err := proc.Process(ctx, ctwaEvent); err != nil {
		t.Fatalf("failed to process CTWA inbound event: %v", err)
	}
	chatAfter, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &connID, contact.ID)
	if err != nil {
		t.Fatalf("failed to get chat after CTWA: %v", err)
	}
	if chatAfter.ServiceWindowExpiresAt == nil {
		t.Fatalf("expected service window to be set")
	}
	diff := chatAfter.ServiceWindowExpiresAt.Sub(now)
	if diff < 71*time.Hour || diff > 73*time.Hour {
		t.Errorf("expected ~72h window duration, got %v", diff)
	}
	if !chatAfter.IsServiceWindowOpen() {
		t.Errorf("expected IsServiceWindowOpen() to be true")
	}

	// Test Snoozed & Closed Chat reopen synchronizes Contact.ReopenThread
	spyContacts := &spyContactStore{
		repo:            contactRepo,
		fixedContact:    contact,
		preserveClosed:  true,
	}
	proc.SetContactStore(spyContacts)

	// 1. Snoozed chat -> inbound message reopens chat and calls ReopenThread on contact
	snoozedUntil := time.Now().UTC().Add(2 * time.Hour)
	if err := chatRepo.SnoozeChat(ctx, ws.ID, chat.ID, &snoozedUntil); err != nil {
		t.Fatalf("failed to snooze chat: %v", err)
	}
	if err := contactRepo.CloseThread(ctx, ws.ID, contact.ID); err != nil {
		t.Fatalf("failed to close contact thread: %v", err)
	}

	snoozeWakeEvent := &inbound.InboundEvent{
		WorkspaceID:  ws.ID,
		ConnectionID: connID,
		MessageID:    "wamid-snooze-wake-001",
		TraceID:      "trace-snooze-wake-001",
		Channel:      "whatsapp_cloud",
		From:         "+5511988887777",
		To:           "bot123",
		Body:         "I have another question while snoozed",
		OccurredAt:   time.Now().UTC(),
	}
	if err := proc.Process(ctx, snoozeWakeEvent); err != nil {
		t.Fatalf("failed to process snooze wake event: %v", err)
	}

	if spyContacts.reopenCalls != 1 {
		t.Fatalf("expected ContactStore.ReopenThread to be called 1 time on snoozed chat inbound, got %d", spyContacts.reopenCalls)
	}
	reopenedChat, err := chatRepo.GetChat(ctx, ws.ID, chat.ID)
	if err != nil {
		t.Fatalf("failed to get chat after snooze wake: %v", err)
	}
	if reopenedChat.Status != domain.ChatStatusOpen || reopenedChat.SnoozedUntil != nil {
		t.Errorf("expected snoozed chat to be reopened with nil SnoozedUntil, got status=%s snoozedUntil=%v", reopenedChat.Status, reopenedChat.SnoozedUntil)
	}
	reopenedContact, err := contactRepo.GetByID(ctx, ws.ID, contact.ID)
	if err != nil {
		t.Fatalf("failed to get contact after snooze wake: %v", err)
	}
	if reopenedContact.ClosedAt != nil {
		t.Errorf("expected contact ClosedAt to be nil after snoozed chat reopen, got %v", reopenedContact.ClosedAt)
	}

	// 2. Closed chat -> inbound message reopens chat and calls ReopenThread on contact
	if err := chatRepo.UpdateChatStatus(ctx, ws.ID, chat.ID, string(domain.ChatStatusClosed)); err != nil {
		t.Fatalf("failed to close chat: %v", err)
	}
	if err := contactRepo.CloseThread(ctx, ws.ID, contact.ID); err != nil {
		t.Fatalf("failed to close contact thread: %v", err)
	}

	closedWakeEvent := &inbound.InboundEvent{
		WorkspaceID:  ws.ID,
		ConnectionID: connID,
		MessageID:    "wamid-closed-wake-001",
		TraceID:      "trace-closed-wake-001",
		Channel:      "whatsapp_cloud",
		From:         "+5511988887777",
		To:           "bot123",
		Body:         "Reopening closed conversation",
		OccurredAt:   time.Now().UTC(),
	}
	if err := proc.Process(ctx, closedWakeEvent); err != nil {
		t.Fatalf("failed to process closed wake event: %v", err)
	}

	if spyContacts.reopenCalls != 2 {
		t.Fatalf("expected ContactStore.ReopenThread to be called 2 times total after closed chat inbound, got %d", spyContacts.reopenCalls)
	}
	reopenedChat2, err := chatRepo.GetChat(ctx, ws.ID, chat.ID)
	if err != nil {
		t.Fatalf("failed to get chat after closed wake: %v", err)
	}
	if reopenedChat2.Status != domain.ChatStatusOpen {
		t.Errorf("expected closed chat to be reopened, got status=%s", reopenedChat2.Status)
	}
	reopenedContact2, err := contactRepo.GetByID(ctx, ws.ID, contact.ID)
	if err != nil {
		t.Fatalf("failed to get contact after closed wake: %v", err)
	}
	if reopenedContact2.ClosedAt != nil {
		t.Errorf("expected contact ClosedAt to be nil after closed chat reopen, got %v", reopenedContact2.ClosedAt)
	}
}

type spyContactStore struct {
	repo           *repository.ContactRepository
	fixedContact   *domain.Contact
	preserveClosed bool
	reopenCalls    int
}

func (s *spyContactStore) ResolveContact(ctx context.Context, workspaceID uuid.UUID, channel, senderIdentity, name, username, phone string) (*domain.Contact, error) {
	if s.preserveClosed && s.fixedContact != nil {
		return s.repo.GetByID(ctx, workspaceID, s.fixedContact.ID)
	}
	return s.repo.ResolveContact(ctx, workspaceID, channel, senderIdentity, name, username, phone)
}

func (s *spyContactStore) ReopenThread(ctx context.Context, workspaceID, contactID uuid.UUID) error {
	s.reopenCalls++
	return s.repo.ReopenThread(ctx, workspaceID, contactID)
}
