package outbound_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/outbound"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/repository"
)

var (
	pgOnce sync.Once
	pgDSN  string
)

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("PERGO_DATABASE_URL")
	if dsn != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if pool, err := pgxpool.New(ctx, dsn); err == nil {
			if err := pool.Ping(ctx); err == nil {
				return pool
			}
			pool.Close()
		}
	}

	pgOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		defer func() {
			_ = recover()
		}()
		pgContainer, err := tcpostgres.Run(ctx,
			"postgres:16-alpine",
			tcpostgres.WithDatabase("pergo"),
			tcpostgres.WithUsername("postgres"),
			tcpostgres.WithPassword("postgres"),
		)
		if err != nil || pgContainer == nil {
			return
		}
		connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			return
		}
		var pool *pgxpool.Pool
		for i := 0; i < 10; i++ {
			pool, err = pgxpool.New(ctx, connStr)
			if err == nil {
				if pool.Ping(ctx) == nil {
					break
				}
				pool.Close()
			}
			time.Sleep(500 * time.Millisecond)
		}
		if pool == nil {
			return
		}
		defer pool.Close()
		db, err := postgres.NewSQLDB(pool)
		if err != nil {
			return
		}
		defer db.Close()
		if err := postgres.RunMigrations(db); err != nil {
			return
		}
		pgDSN = connStr
	})

	if pgDSN == "" {
		t.Skip("PostgreSQL testcontainer not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, pgDSN)
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
	_, _ = pool.Exec(ctx, "DELETE FROM waba_templates")
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
		VALUES ($1, $2, 'Outbound Conn', 'whatsapp_cloud', 'whatsapp_cloud', '+551100000000', 'active', '{}', true)
	`, connID, ws.ID)
	if err != nil {
		t.Fatalf("failed to insert connection: %v", err)
	}

	conn := &repository.Connection{
		ID:             connID,
		WorkspaceID:    ws.ID,
		Name:           "Outbound Conn",
		Slug:           "whatsapp_cloud",
		Channel:        "whatsapp_cloud",
		SenderIdentity: "+551100000000",
		Status:         "active",
		IsDefault:      true,
	}

	resolver := &fakeRouteResolver{conn: conn}
	pub := &fakePublisher{}
	debouncer := &fakeDebounceCanceller{}

	proc := outbound.NewProcessor(nil, nil, resolver, pub)
	proc.SetChatRepository(chatRepo)
	proc.SetContactRepository(contactRepo)
	proc.SetDebounceCanceller(debouncer)

	t.Run("Human agent outbound message triggers handoff and persists message", func(t *testing.T) {
		pub.published = nil
		pub.subjects = nil
		debouncer.cancelled = nil

		req := &domain.CreateMessageRequest{
			To:      "+5511999991111",
			Channel: "whatsapp_cloud",
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

		contact, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp_cloud", "+5511999991111", "", "", "")
		if err != nil {
			t.Fatalf("failed to resolve contact: %v", err)
		}

		// Verify chat exists and has AI paused (ai_disabled = true)
		chat, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &connID, contact.ID)
		if err != nil {
			t.Fatalf("failed to get chat: %v", err)
		}
		if !chat.AIDisabled || chat.AI.Effective(time.Now().UTC()).IsEngaged() {
			t.Errorf("expected AI to be paused for human agent reply, got %+v", chat.AI)
		}

		// Verify debounce buffer cancelled on handoff by chat.ID
		if len(debouncer.cancelled) != 1 || debouncer.cancelled[0] != chat.ID {
			t.Errorf("expected debouncer.Cancel called for chat %s, got %v", chat.ID, debouncer.cancelled)
		}

		// Verify NATS handoff subjects published
		hasGlobalHandoff := false
		hasWorkspaceHandoff := false
		for _, subj := range pub.subjects {
			if subj == "chat.handoff.human_takeover" {
				hasGlobalHandoff = true
			}
			if subj == "chat.handoff."+ws.ID.String() {
				hasWorkspaceHandoff = true
			}
		}
		if !hasGlobalHandoff || !hasWorkspaceHandoff {
			t.Errorf("expected handoff subjects published, got %v", pub.subjects)
		}

		// Verify chat message was recorded with direction = outbound
		msgs, err := chatRepo.ListChatMessages(ctx, ws.ID, chat.ID, "", "", 10)
		if err != nil {
			t.Fatalf("failed to list chat messages: %v", err)
		}
		if len(msgs) != 1 {
			t.Fatalf("expected 1 message, got %d", len(msgs))
		}
		if msgs[0].Direction != domain.DirectionOutbound {
			t.Errorf("expected direction outbound, got %s", msgs[0].Direction)
		}
		if msgs[0].SenderType != domain.SenderTypeHumanAgent {
			t.Errorf("expected sender type human_agent, got %s", msgs[0].SenderType)
		}
		if msgs[0].Body != "Hello from human agent!" {
			t.Errorf("expected body 'Hello from human agent!', got %s", msgs[0].Body)
		}
	})

	t.Run("AI agent outbound message does NOT trigger handoff", func(t *testing.T) {
		pub.published = nil
		pub.subjects = nil
		debouncer.cancelled = nil

		req := &domain.CreateMessageRequest{
			ConversationalIngestFields: domain.ConversationalIngestFields{
				SenderType: string(domain.SenderTypeAIAgent),
				SenderName: "AI Bot",
			},
			To:      "+5511999993333",
			Channel: "whatsapp_cloud",
			From:    "+551100000000",
			Body:    "Automated AI check-in",
		}

		if _, err := proc.Ingest(ctx, ws.ID, "outbound-trace-no-handoff", req); err != nil {
			t.Fatalf("failed to ingest outbound message: %v", err)
		}

		contact, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp_cloud", "+5511999993333", "", "", "")
		if err != nil {
			t.Fatalf("failed to resolve contact: %v", err)
		}
		chat, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &connID, contact.ID)
		if err != nil {
			t.Fatalf("failed to get chat: %v", err)
		}
		if chat.AIDisabled || !chat.EffectiveAI().IsEngaged() {
			t.Errorf("expected chat AI to remain engaged on AI agent turn")
		}
		if len(debouncer.cancelled) != 0 {
			t.Errorf("expected debouncer NOT to be called on AI agent turn, got %v", debouncer.cancelled)
		}
		for _, subj := range pub.subjects {
			if subj == "chat.handoff.human_takeover" || subj == "chat.handoff."+ws.ID.String() {
				t.Errorf("unexpected handoff event published on AI agent turn: %s", subj)
			}
		}
	})

	t.Run("ConversationalIngestFields persists ReplyToUID, reopens snoozed/closed chat, and records WABA template metadata", func(t *testing.T) {
		contact, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp_cloud", "+5511999992222", "Customer Two", "", "+5511999992222")
		if err != nil {
			t.Fatalf("failed to resolve contact: %v", err)
		}
		chat, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &connID, contact.ID)
		if err != nil {
			t.Fatalf("failed to create chat: %v", err)
		}

		// Snooze the chat first
		snoozeUntil := time.Now().UTC().Add(2 * time.Hour)
		if err := chatRepo.SnoozeChat(ctx, ws.ID, chat.ID, &snoozeUntil); err != nil {
			t.Fatalf("failed to snooze chat: %v", err)
		}

		parentUID := "inbound-parent-uid-100"
		_ = chatRepo.AddChatMessage(ctx, &domain.ChatMessage{
			ChatID:      chat.ID,
			WorkspaceID: ws.ID,
			UID:         parentUID,
			Direction:   domain.DirectionInbound,
			SenderType:  domain.SenderTypeContact,
			Body:        "Parent question",
			CreatedAt:   time.Now().UTC().Add(-5 * time.Minute),
		})

		// Send threaded AI reply using ConversationalIngestFields
		chatIDCopy := chat.ID
		replyUIDCopy := parentUID
		replyTraceID := "outbound-reply-trace-200"
		replyReq := &domain.CreateMessageRequest{
			ConversationalIngestFields: domain.ConversationalIngestFields{
				ChatID:     &chatIDCopy,
				ReplyToUID: &replyUIDCopy,
				SenderType: "ai",
				SenderName: "AI Concierge",
			},
			To:      "+5511999992222",
			Channel: "whatsapp_cloud",
			From:    "+551100000000",
			Body:    "Here is the answer to your question!",
		}

		if _, err := proc.Ingest(ctx, ws.ID, replyTraceID, replyReq); err != nil {
			t.Fatalf("Ingest reply failed: %v", err)
		}

		// Verify snoozed chat was reopened to "open"
		reopenedChat, err := chatRepo.GetChat(ctx, ws.ID, chat.ID)
		if err != nil {
			t.Fatalf("GetChat failed: %v", err)
		}
		if reopenedChat.Status != domain.ChatStatusOpen {
			t.Errorf("expected snoozed chat to reopen to 'open' on outbound activity, got %q", reopenedChat.Status)
		}
		if reopenedChat.SnoozedUntil != nil {
			t.Errorf("expected SnoozedUntil to be cleared, got %v", reopenedChat.SnoozedUntil)
		}

		// Verify persisted ChatMessage has ReplyToUID, SenderTypeAIAgent, SenderName
		persistedReply, err := chatRepo.GetChatMessageByUID(ctx, ws.ID, replyTraceID)
		if err != nil {
			t.Fatalf("GetChatMessageByUID failed: %v", err)
		}
		if persistedReply.ReplyToUID == nil || *persistedReply.ReplyToUID != parentUID {
			t.Errorf("got ReplyToUID %v, want %q", persistedReply.ReplyToUID, parentUID)
		}
		if persistedReply.SenderType != domain.SenderTypeAIAgent {
			t.Errorf("got SenderType %q, want %q", persistedReply.SenderType, domain.SenderTypeAIAgent)
		}
		if persistedReply.SenderName != "AI Concierge" {
			t.Errorf("got SenderName %q, want 'AI Concierge'", persistedReply.SenderName)
		}

		// Close the chat and send a WABA template message -> verify closed chat reopens and template metadata is saved
		_ = chatRepo.UpdateChatStatus(ctx, ws.ID, chat.ID, string(domain.ChatStatusClosed))
		_ = contactRepo.CloseThread(ctx, ws.ID, contact.ID)

		tmplTraceID := "outbound-tmpl-trace-300"
		tmplReq := &domain.CreateMessageRequest{
			ConversationalIngestFields: domain.ConversationalIngestFields{
				ChatID:     &chatIDCopy,
				SenderType: "ai",
				SenderName: "Template Dispatcher",
			},
			To:           "+5511999992222",
			Channel:      "whatsapp_cloud",
			From:         "+551100000000",
			TemplateName: "order_status_update",
			Language:     "pt_BR",
			Type:         "template",
		}

		if _, err := proc.Ingest(ctx, ws.ID, tmplTraceID, tmplReq); err != nil {
			t.Fatalf("Ingest template failed: %v", err)
		}

		reopenedFromClosed, err := chatRepo.GetChat(ctx, ws.ID, chat.ID)
		if err != nil {
			t.Fatalf("GetChat failed: %v", err)
		}
		if reopenedFromClosed.Status != domain.ChatStatusOpen {
			t.Errorf("expected closed chat to reopen to 'open', got %q", reopenedFromClosed.Status)
		}

		persistedTmpl, err := chatRepo.GetChatMessageByUID(ctx, ws.ID, tmplTraceID)
		if err != nil {
			t.Fatalf("GetChatMessageByUID failed: %v", err)
		}
		if persistedTmpl.Body != "[Template: order_status_update (pt_BR)]" {
			t.Errorf("got Body %q, want '[Template: order_status_update (pt_BR)]'", persistedTmpl.Body)
		}
		if persistedTmpl.Metadata["template_name"] != "order_status_update" {
			t.Errorf("got metadata template_name %v, want 'order_status_update'", persistedTmpl.Metadata["template_name"])
		}
		if persistedTmpl.Metadata["language"] != "pt_BR" {
			t.Errorf("got metadata language %v, want 'pt_BR'", persistedTmpl.Metadata["language"])
		}
		if persistedTmpl.Metadata["type"] != "template" {
			t.Errorf("got metadata type %v, want 'template'", persistedTmpl.Metadata["type"])
		}

		// Verify StrictServiceWindow against chat's expired ServiceWindowExpiresAt
		expiredAt := time.Now().UTC().Add(-1 * time.Hour)
		_ = chatRepo.UpdateServiceWindow(ctx, ws.ID, chat.ID, &expiredAt)

		strictReq := &domain.CreateMessageRequest{
			ConversationalIngestFields: domain.ConversationalIngestFields{
				ChatID:              &chatIDCopy,
				StrictServiceWindow: true,
			},
			To:      "+5511999992222",
			Channel: "whatsapp_cloud",
			From:    "+551100000000",
			Body:    "Freeform message when chat service window is expired",
		}
		_, err = proc.Ingest(ctx, ws.ID, "outbound-strict-blocked-400", strictReq)
		if err == nil {
			t.Fatalf("expected StrictServiceWindow to block freeform message on expired chat")
		}
		var valErr *domain.ValidationError
		if !errors.As(err, &valErr) {
			t.Fatalf("expected *domain.ValidationError, got %T: %v", err, err)
		}
	})
}
