package inbound_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/chat/activity"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/repository"
)

type spyInboundRouter struct {
	calls int
}

func (s *spyInboundRouter) Route(_ context.Context, _ *domain.Contact, _ *inbound.InboundEvent) error {
	s.calls++
	return nil
}

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
	rec := activity.New(pool, activity.NewMemoryPublisher(), activity.NewMemoryDebounceCanceller(), nil)

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

	pub := &fakePublisher{}
	router := &spyInboundRouter{}
	proc := inbound.NewInboundProcessor(nil, wsRepo, nil, pub, nil, nil, contactRepo, nil, router)
	proc.SetChatRepository(chatRepo)
	proc.SetActivity(rec)

	// 1. Inbound turn delegates to activity.Inbound and routes when AIMayAct() is true
	msgID := "wamid-tracer-001"
	now := time.Now().UTC().Truncate(time.Microsecond)
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
		OccurredAt:   now,
	}

	if err := proc.Process(ctx, event); err != nil {
		t.Fatalf("failed to process inbound event: %v", err)
	}
	if len(pub.published) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(pub.published))
	}
	if router.calls != 1 {
		t.Fatalf("expected router to be called once when AIMayAct() is true, got %d", router.calls)
	}

	contact, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp_cloud", "+5511988887777", "", "", "")
	if err != nil {
		t.Fatalf("failed to resolve contact: %v", err)
	}
	chat, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &connID, contact.ID)
	if err != nil {
		t.Fatalf("failed to get chat: %v", err)
	}
	if chat.UnreadCount != 1 {
		t.Errorf("expected unread count 1, got %d", chat.UnreadCount)
	}

	// 2. Duplicate delivery short-circuits via receipt.Duplicate before NATS publish or Router
	if err := proc.Process(ctx, event); err != nil {
		t.Fatalf("duplicate Process returned error: %v", err)
	}
	if len(pub.published) != 1 {
		t.Errorf("expected duplicate to short-circuit before NATS publish, got %d published events", len(pub.published))
	}
	if router.calls != 1 {
		t.Errorf("expected duplicate to short-circuit before Router, got %d router calls", router.calls)
	}

	// 3. Early-return ordering: activity.Inbound runs before NATS publish, so if NATS publish errors, Chat state has already advanced
	pub.err = errors.New("nats down")
	earlyErrEv := &inbound.InboundEvent{
		WorkspaceID:  ws.ID,
		ConnectionID: connID,
		MessageID:    "wamid-early-err-002",
		TraceID:      "trace-002",
		Channel:      "whatsapp_cloud",
		From:         "+5511988887777",
		To:           "bot123",
		Body:         "Persisted before NATS failure",
		SenderName:   "Customer Bob",
		OccurredAt:   now.Add(time.Minute),
	}
	if err := proc.Process(ctx, earlyErrEv); err == nil {
		t.Fatalf("expected NATS publish error")
	}
	pub.err = nil

	chatAfterErr, err := chatRepo.GetChat(ctx, ws.ID, chat.ID)
	if err != nil {
		t.Fatalf("failed to get chat after NATS error: %v", err)
	}
	if chatAfterErr.UnreadCount != 2 {
		t.Errorf("expected Chat unread count to have advanced to 2 before NATS publish error, got %d", chatAfterErr.UnreadCount)
	}

	// 4. AIMayAct() gating: when AI is off, inbound turn still records in Chat but Router is bypassed
	if _, err := rec.SetAI(ctx, ws.ID, activity.ChatID(chat.ID), activity.AIOff()); err != nil {
		t.Fatalf("SetAI(AIOff) failed: %v", err)
	}
	aiOffEv := &inbound.InboundEvent{
		WorkspaceID:  ws.ID,
		ConnectionID: connID,
		MessageID:    "wamid-ai-off-003",
		TraceID:      "trace-003",
		Channel:      "whatsapp_cloud",
		From:         "+5511988887777",
		To:           "bot123",
		Body:         "Message while AI is off",
		SenderName:   "Customer Bob",
		OccurredAt:   now.Add(2 * time.Minute),
	}
	if err := proc.Process(ctx, aiOffEv); err != nil {
		t.Fatalf("failed to process aiOffEv: %v", err)
	}
	if router.calls != 1 {
		t.Errorf("expected Router to be gated off when AIMayAct() is false, got %d calls", router.calls)
	}
	chatAfterAIOff, err := chatRepo.GetChat(ctx, ws.ID, chat.ID)
	if err != nil {
		t.Fatalf("failed to get chat after aiOffEv: %v", err)
	}
	if chatAfterAIOff.UnreadCount != 3 {
		t.Errorf("expected unread count 3 after aiOffEv, got %d", chatAfterAIOff.UnreadCount)
	}
}
