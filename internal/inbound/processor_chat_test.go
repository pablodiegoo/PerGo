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
	if msgs[0].Direction != string(domain.DirectionInbound) {
		t.Errorf("expected direction inbound, got %s", msgs[0].Direction)
	}
	if msgs[0].Body != "Hello from customer!" {
		t.Errorf("expected body 'Hello from customer!', got %s", msgs[0].Body)
	}
}
