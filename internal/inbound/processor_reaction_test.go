package inbound_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/repository"
)

type mockPublisher struct {
	publishedSubject string
	publishedData    []byte
	publishedTraceID string
}

func (m *mockPublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	m.publishedSubject = subject
	m.publishedData = data
	m.publishedTraceID = traceID
	return nil
}

type mockReactionWebhookDispatcher struct {
	dispatchedWSID       uuid.UUID
	dispatchedMessageUID string
	dispatchedPayload    []byte
	dispatchedTraceID    string
}

func (m *mockReactionWebhookDispatcher) DispatchReaction(ctx context.Context, wsID uuid.UUID, msgUID string, payload []byte, traceID string) error {
	m.dispatchedWSID = wsID
	m.dispatchedMessageUID = msgUID
	m.dispatchedPayload = payload
	m.dispatchedTraceID = traceID
	return nil
}

func getInboundTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("PERGO_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("skipping: cannot connect to postgres: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping: cannot ping postgres: %v", err)
	}

	db, err := postgres.NewSQLDB(pool)
	if err == nil {
		defer db.Close()
		_ = postgres.RunMigrations(db)
	}
	return pool
}

func TestInboundProcessor_ReactionHandling(t *testing.T) {
	pool := getInboundTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	chatRepo := repository.NewChatRepository(pool)

	ws, err := wsRepo.Create(ctx, "Reaction Inbound Test WS")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	contact, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp", "+5511999990000", "John Doe", "", "+5511999990000")
	if err != nil {
		t.Fatalf("failed to resolve contact: %v", err)
	}

	chat, err := chatRepo.FindOrCreateChat(ctx, ws.ID, nil, contact.ID)
	if err != nil {
		t.Fatalf("failed to create chat: %v", err)
	}

	msgUID := "test-msg-" + uuid.New().String()
	chatMsg := &domain.ChatMessage{
		ChatID:      chat.ID,
		WorkspaceID: ws.ID,
		UID:         msgUID,
		Direction:   domain.DirectionInbound,
		SenderType:  domain.SenderTypeContact,
		SenderID:    "+5511999990000",
		Body:        "Hello there!",
		CreatedAt:   time.Now().UTC(),
	}
	if err := chatRepo.AddChatMessage(ctx, chatMsg); err != nil {
		t.Fatalf("failed to add chat message: %v", err)
	}

	pub := &mockPublisher{}
	whDisp := &mockReactionWebhookDispatcher{}

	proc := inbound.NewInboundProcessor(nil, wsRepo, nil, pub, nil, nil, contactRepo, nil, nil)
	proc.SetChatRepository(chatRepo)
	proc.SetReactionWebhookDispatcher(whDisp)

	// Step 1: Inbound Add Reaction
	addEvent := &inbound.InboundEvent{
		WorkspaceID: ws.ID,
		MessageID:   "evt-1",
		TraceID:     "trace-rx-add-1",
		Channel:     "whatsapp",
		From:        "+5511999990000",
		OccurredAt:  time.Now().UTC(),
		Reaction: &inbound.InboundReaction{
			MessageUID: msgUID,
			Emoji:      "❤️",
			Action:     "add",
		},
	}

	if err := proc.Process(ctx, addEvent); err != nil {
		t.Fatalf("failed to process reaction add: %v", err)
	}

	// Verify reaction in database
	dbMsg, err := chatRepo.GetChatMessageByUID(ctx, ws.ID, msgUID)
	if err != nil {
		t.Fatalf("failed to get message: %v", err)
	}
	if len(dbMsg.Reactions) != 1 || dbMsg.Reactions[0].Emoji != "❤️" {
		t.Fatalf("expected 1 reaction ❤️ in DB, got: %+v", dbMsg.Reactions)
	}

	// Verify NATS event published
	if pub.publishedSubject != "messages.events.reaction_updated" {
		t.Errorf("expected NATS subject messages.events.reaction_updated, got %s", pub.publishedSubject)
	}
	var rxPayload domain.ReactionUpdatedPayload
	if err := json.Unmarshal(pub.publishedData, &rxPayload); err != nil {
		t.Fatalf("failed to unmarshal NATS reaction payload: %v", err)
	}
	if rxPayload.Emoji != "❤️" || rxPayload.Action != "add" || rxPayload.MessageUID != msgUID {
		t.Errorf("unexpected NATS reaction payload: %+v", rxPayload)
	}

	// Verify Webhook dispatched
	if whDisp.dispatchedMessageUID != msgUID {
		t.Errorf("expected webhook dispatched for %s, got %s", msgUID, whDisp.dispatchedMessageUID)
	}

	// Step 2: Inbound Remove Reaction
	remEvent := &inbound.InboundEvent{
		WorkspaceID: ws.ID,
		MessageID:   "evt-2",
		TraceID:     "trace-rx-rem-2",
		Channel:     "whatsapp",
		From:        "+5511999990000",
		OccurredAt:  time.Now().UTC(),
		Reaction: &inbound.InboundReaction{
			MessageUID: msgUID,
			Emoji:      "❤️",
			Action:     "remove",
		},
	}

	if err := proc.Process(ctx, remEvent); err != nil {
		t.Fatalf("failed to process reaction remove: %v", err)
	}

	dbMsg2, err := chatRepo.GetChatMessageByUID(ctx, ws.ID, msgUID)
	if err != nil {
		t.Fatalf("failed to get message: %v", err)
	}
	if len(dbMsg2.Reactions) != 0 {
		t.Fatalf("expected 0 reactions in DB after remove, got: %+v", dbMsg2.Reactions)
	}
}
