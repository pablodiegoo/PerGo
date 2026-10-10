package outbound_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/outbound"
	"github.com/pablojhp.pergo/internal/repository"
)

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("PERGO_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("PostgreSQL ping failed: %v", err)
	}
	return pool
}

func TestProcessor_ChatOutboundTracking(t *testing.T) {
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

	ws, err := wsRepo.Create(ctx, "chat_outbound_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	connID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO connections (id, workspace_id, name, slug, channel, sender_identity, status, credentials, is_default)
		VALUES ($1, $2, 'Outbound Conn', 'whatsapp', 'whatsapp', '+551100000000', 'active', '{}', true)
	`, connID, ws.ID)
	if err != nil {
		t.Fatalf("failed to insert connection: %v", err)
	}

	conn := &repository.Connection{
		ID:             connID,
		WorkspaceID:    ws.ID,
		Name:           "Outbound Conn",
		Slug:           "whatsapp",
		Channel:        "whatsapp",
		SenderIdentity: "+551100000000",
		Status:         "active",
		IsDefault:      true,
	}

	resolver := &fakeRouteResolver{conn: conn}
	pub := &fakePublisher{}

	proc := outbound.NewProcessor(nil, nil, resolver, pub)
	proc.SetChatRepository(chatRepo)
	proc.SetContactRepository(contactRepo)

	req := &domain.CreateMessageRequest{
		To:      "+5511999991111",
		Channel: "whatsapp",
		From:    "+551100000000",
		Body:    "Hello from human agent!",
		Metadata: map[string]string{
			"sender_type": string(domain.SenderTypeHumanAgent),
			"sender_name": "Support Agent",
		},
	}

	traceID := "outbound-trace-001"
	qMsg, err := proc.Ingest(ctx, ws.ID, traceID, req)
	if err != nil {
		t.Fatalf("failed to ingest outbound message: %v", err)
	}
	if qMsg == nil {
		t.Fatal("expected non-nil QueueMessage")
	}

	// Verify contact exists
	contact, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp", "+5511999991111", "", "", "")
	if err != nil {
		t.Fatalf("failed to resolve contact: %v", err)
	}

	// Verify chat exists and has ai_disabled = true
	chat, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &connID, contact.ID)
	if err != nil {
		t.Fatalf("failed to get chat: %v", err)
	}
	if !chat.AIDisabled {
		t.Errorf("expected AIDisabled to be true for human agent reply")
	}

	// Verify chat message was recorded with direction = outbound
	msgs, err := chatRepo.ListChatMessages(ctx, ws.ID, chat.ID, "", "", 10)
	if err != nil {
		t.Fatalf("failed to list chat messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Direction != string(domain.DirectionOutbound) {
		t.Errorf("expected direction outbound, got %s", msgs[0].Direction)
	}
	if msgs[0].SenderType != string(domain.SenderTypeHumanAgent) {
		t.Errorf("expected sender type human_agent, got %s", msgs[0].SenderType)
	}
	if msgs[0].Body != "Hello from human agent!" {
		t.Errorf("expected body 'Hello from human agent!', got %s", msgs[0].Body)
	}
}
