package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/api/handler/admin"
	mcppkg "github.com/pablojhp.pergo/internal/api/mcp"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/i18n"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/security"
	"github.com/pablojhp.pergo/internal/webhook"
)

type reactionsMockPublisher struct {
	mu        sync.Mutex
	published []publishedRecord
}

type publishedRecord struct {
	Subject string
	Data    []byte
	TraceID string
}

func (p *reactionsMockPublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.published = append(p.published, publishedRecord{
		Subject: subject,
		Data:    data,
		TraceID: traceID,
	})
	return nil
}

func TestMessageReactionsIntegration(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("skipping: PostgreSQL not available")
	}

	ctx := context.Background()

	// Clean up tables
	_, _ = pool.Exec(ctx, "DELETE FROM webhook_subscriptions")
	_, _ = pool.Exec(ctx, "DELETE FROM chat_messages")
	_, _ = pool.Exec(ctx, "DELETE FROM chats")
	_, _ = pool.Exec(ctx, "DELETE FROM contact_identities")
	_, _ = pool.Exec(ctx, "DELETE FROM contacts")
	_, _ = pool.Exec(ctx, "DELETE FROM connections")
	_, _ = pool.Exec(ctx, "DELETE FROM audit_logs")
	_, _ = pool.Exec(ctx, "DELETE FROM workspaces")

	wsRepo := repository.NewWorkspaceRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	chatRepo := repository.NewChatRepository(pool)
	auditRepo := repository.NewAuditRepository(pool)

	kek := make([]byte, 32)
	copy(kek, []byte("dev-development-key-32-bytes-kek"))
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	connRepo := repository.NewConnectionRepository(pool, enc)
	webhookSubRepo := repository.NewWebhookSubscriptionRepository(pool, enc)
	webhookDLQRepo := repository.NewWebhookDLQRepository(pool, enc)

	// 1. Create Workspace & Connection
	ws, err := wsRepo.Create(ctx, "Reactions Test Workspace")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	conn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "Reactions WhatsApp",
		Slug:           "whatsapp",
		Channel:        "whatsapp",
		SenderIdentity: "+551100008888",
		Status:         "active",
		IsDefault:      true,
		Credentials:    []byte(`{"token":"dummy"}`),
	}
	if err := connRepo.Create(ctx, conn); err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}

	// 2. Setup Webhook Destination & Server
	var (
		whMu             sync.Mutex
		receivedPayloads [][]byte
		receivedHeaders  []http.Header
	)
	webhookSecret := "super-secret-webhook-key-32-bytes!"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		whMu.Lock()
		receivedPayloads = append(receivedPayloads, body)
		receivedHeaders = append(receivedHeaders, r.Header.Clone())
		whMu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// 3. Register Webhook Subscription
	_, err = webhookSubRepo.Create(ctx, ws.ID, ts.URL, []string{"message.reaction.updated"}, []byte(webhookSecret))
	if err != nil {
		t.Fatalf("failed to create webhook subscription: %v", err)
	}

	safeClient := security.NewSafeWebhookClient(
		security.WithTimeout(5*time.Second),
		security.WithAllowlist("127.0.0.1", "localhost", "::1"),
	)
	verbsEngine := webhook.NewVerbsEngine(nil, contactRepo, nil, connRepo)
	webhookDispatcher := webhook.NewDefaultDispatcher(webhookSubRepo, webhookDLQRepo, wsRepo, safeClient, verbsEngine)

	mockPub := &reactionsMockPublisher{}

	// 4. Setup Inbound Message, Contact, Chat and Audit Log
	msgUID := "wamid.HBgLMjAyNi0xMC0xMA=="
	customerPhone := "+5511999994321"
	customerName := "Diana Prince"
	messageText := "This is a message to react to!"

	inboundProc := inbound.NewInboundProcessor(nil, wsRepo, nil, nil, nil, nil, contactRepo, nil, nil)
	inboundProc.SetChatRepository(chatRepo)

	inboundEv := &inbound.InboundEvent{
		WorkspaceID:  ws.ID,
		ConnectionID: conn.ID,
		MessageID:    msgUID,
		TraceID:      "trace-react-100",
		Channel:      "whatsapp",
		From:         customerPhone,
		To:           "+551100008888",
		Body:         messageText,
		SenderName:   customerName,
		OccurredAt:   time.Now().UTC(),
	}
	if err := inboundProc.Process(ctx, inboundEv); err != nil {
		t.Fatalf("failed to process inbound event: %v", err)
	}

	contact, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp", customerPhone, "", "", "")
	if err != nil {
		t.Fatalf("failed to resolve contact: %v", err)
	}

	// Add audit log entry matching this inbound message
	auditPayload, _ := json.Marshal(map[string]any{
		"channel":    "whatsapp",
		"from":       customerPhone,
		"body":       messageText,
		"message_id": msgUID,
	})
	_, _ = pool.Exec(ctx, `
		INSERT INTO audit_logs (id, workspace_id, trace_id, event_type, payload, created_at)
		VALUES (gen_random_uuid(), $1, 'trace-react-100', 'inbound_message', $2, NOW())
	`, ws.ID, auditPayload)

	// 5. Setup MCP Server
	mcpServer := mcppkg.NewServer(
		wsRepo,
		connRepo,
		contactRepo,
		auditRepo,
		nil,
		nil,
		webhookSubRepo,
		nil,
		webhookDispatcher,
		[]byte("secret"),
		"http://localhost:8080",
		mcppkg.WithChatRepo(chatRepo),
		mcppkg.WithPublisher(mockPub),
	)

	// 6. Setup Admin Inbox Handler
	inboxHandler := &admin.InboxHandler{
		Repo:              auditRepo,
		ContactRepo:       contactRepo,
		ChatRepo:          chatRepo,
		Connections:       connRepo,
		Workspaces:        wsRepo,
		Publisher:         mockPub,
		WebhookSubRepo:    webhookSubRepo,
		WebhookDispatcher: webhookDispatcher,
	}

	// =========================================================================
	// PHASE A: Invoke `message_react` via MCP to ADD a reaction ("🔥")
	// =========================================================================
	t.Run("MCP message_react adds reaction and fires webhook + NATS", func(t *testing.T) {
		reactArgs := map[string]any{
			"workspace_id": ws.ID.String(),
			"message_uid":  msgUID,
			"emoji":        "🔥",
			"action":       "add",
			"sender":       "ai-agent-007",
		}

		res, err := mcpServer.CallTool(ctx, "message_react", reactArgs)
		if err != nil {
			t.Fatalf("mcp message_react returned error: %v", err)
		}
		if res.IsError {
			t.Fatalf("mcp message_react tool failure: %+v", res.Content)
		}

		// 1. Verify reaction updated in database chat_messages.reactions JSONB
		var rxJSON []byte
		err = pool.QueryRow(ctx, "SELECT reactions FROM chat_messages WHERE uid = $1", msgUID).Scan(&rxJSON)
		if err != nil {
			t.Fatalf("failed to query reactions from database: %v", err)
		}
		var dbReactions []domain.Reaction
		if err := json.Unmarshal(rxJSON, &dbReactions); err != nil {
			t.Fatalf("failed to unmarshal db reactions: %v", err)
		}
		if len(dbReactions) != 1 {
			t.Fatalf("expected 1 reaction in DB, got %d", len(dbReactions))
		}
		if dbReactions[0].Emoji != "🔥" || dbReactions[0].Sender != "ai-agent-007" {
			t.Errorf("unexpected reaction in DB: %+v", dbReactions[0])
		}

		// 2. Verify NATS event published to messages.events.reaction_updated
		mockPub.mu.Lock()
		publishedCount := len(mockPub.published)
		var lastPub publishedRecord
		if publishedCount > 0 {
			lastPub = mockPub.published[publishedCount-1]
		}
		mockPub.mu.Unlock()

		if publishedCount == 0 {
			t.Fatalf("expected NATS reaction event published, got 0")
		}
		if lastPub.Subject != "messages.events.reaction_updated" {
			t.Errorf("expected NATS subject messages.events.reaction_updated, got %s", lastPub.Subject)
		}

		// 3. Verify HMAC-signed webhook delivery to endpoint
		whMu.Lock()
		delivCount := len(receivedPayloads)
		var lastPayload []byte
		var lastHeaders http.Header
		if delivCount > 0 {
			lastPayload = receivedPayloads[delivCount-1]
			lastHeaders = receivedHeaders[delivCount-1]
		}
		whMu.Unlock()

		if delivCount != 1 {
			t.Fatalf("expected 1 webhook delivery, got %d", delivCount)
		}

		sigHeader := lastHeaders.Get("X-PerGo-Signature")
		if sigHeader == "" {
			t.Fatal("expected X-PerGo-Signature header on webhook request")
		}
		if !webhook.VerifyPerGoSignature(lastPayload, sigHeader, webhookSecret) {
			t.Errorf("HMAC-SHA256 signature verification failed for delivered payload (header: %s)", sigHeader)
		}

		var whEvent domain.ReactionUpdatedPayload
		if err := json.Unmarshal(lastPayload, &whEvent); err != nil {
			t.Fatalf("failed to unmarshal webhook payload: %v", err)
		}
		if whEvent.Event != "message.reaction.updated" || whEvent.Emoji != "🔥" || whEvent.Action != "add" {
			t.Errorf("unexpected webhook event content: %+v", whEvent)
		}
		if whEvent.MessageUID != msgUID {
			t.Errorf("expected webhook MessageUID %s, got %s", msgUID, whEvent.MessageUID)
		}

		// 4. Verify Web Console HTML contains reaction badge & popover trigger
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/admin/inbox/chat?contact_id="+contact.ID.String(), nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()

		reqCtx := tenant.WithWorkspaceID(req.Context(), ws.ID)
		reqCtx = domain.ContextWithWorkspaceID(reqCtx, ws.ID)
		reqCtx = i18n.WithLocale(reqCtx, "pt-BR")
		req = req.WithContext(reqCtx)
		c := e.NewContext(req, rec)

		if err := inboxHandler.ChatPanel(c); err != nil {
			t.Fatalf("ChatPanel handler failed: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("expected HTTP 200 from ChatPanel, got %d", rec.Code)
		}

		html := rec.Body.String()
		if !strings.Contains(html, "🔥") {
			t.Errorf("expected ChatPanel HTML to contain emoji 🔥, got: %s", html)
		}
		if !strings.Contains(html, "reaction-badge") {
			t.Errorf("expected ChatPanel HTML to contain reaction-badge class")
		}
		if !strings.Contains(html, "reaction-trigger-btn") {
			t.Errorf("expected ChatPanel HTML to contain reaction-trigger-btn class")
		}
	})

	// =========================================================================
	// PHASE B: Web Console HTMX Toggle Endpoint (`POST /admin/inbox/reactions`)
	// =========================================================================
	t.Run("Web Console HTMX ToggleReaction toggles emoji and updates UI", func(t *testing.T) {
		// 1. Toggle add "👍" via HTMX
		e := echo.New()
		form := url.Values{}
		form.Set("message_uid", msgUID)
		form.Set("emoji", "👍")
		req := httptest.NewRequest(http.MethodPost, "/admin/inbox/reactions", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()

		reqCtx := tenant.WithWorkspaceID(req.Context(), ws.ID)
		reqCtx = domain.ContextWithWorkspaceID(reqCtx, ws.ID)
		reqCtx = i18n.WithLocale(reqCtx, "pt-BR")
		req = req.WithContext(reqCtx)
		c := e.NewContext(req, rec)

		if err := inboxHandler.ToggleReaction(c); err != nil {
			t.Fatalf("ToggleReaction handler failed: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("expected HTTP 200 from ToggleReaction, got %d", rec.Code)
		}

		html := rec.Body.String()
		if !strings.Contains(html, `title="Reação 👍`) || !strings.Contains(html, `title="Reação 🔥`) {
			t.Errorf("expected returned ReactionBadgeList to contain both 👍 and 🔥 badges, got: %s", html)
		}

		// Verify 2 reactions in DB
		reactions, err := chatRepo.GetReactions(ctx, ws.ID, msgUID)
		if err != nil {
			t.Fatalf("failed to get reactions from repo: %v", err)
		}
		if len(reactions) != 2 {
			t.Fatalf("expected 2 reactions in DB, got %d", len(reactions))
		}

		// 2. Toggle remove "👍" via HTMX
		req2 := httptest.NewRequest(http.MethodPost, "/admin/inbox/reactions", strings.NewReader(form.Encode()))
		req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec2 := httptest.NewRecorder()
		req2 = req2.WithContext(reqCtx)
		c2 := e.NewContext(req2, rec2)

		if err := inboxHandler.ToggleReaction(c2); err != nil {
			t.Fatalf("ToggleReaction handler failed: %v", err)
		}
		if rec2.Code != http.StatusOK {
			t.Fatalf("expected HTTP 200 from ToggleReaction, got %d", rec2.Code)
		}

		html2 := rec2.Body.String()
		if strings.Contains(html2, `title="Reação 👍`) {
			t.Errorf("expected returned ReactionBadgeList to no longer contain 👍 badge, got: %s", html2)
		}
		if !strings.Contains(html2, `title="Reação 🔥`) {
			t.Errorf("expected returned ReactionBadgeList to still contain 🔥 badge, got: %s", html2)
		}

		reactionsAfterToggle, err := chatRepo.GetReactions(ctx, ws.ID, msgUID)
		if err != nil {
			t.Fatalf("failed to get reactions from repo: %v", err)
		}
		if len(reactionsAfterToggle) != 1 {
			t.Fatalf("expected 1 reaction in DB after toggle, got %d", len(reactionsAfterToggle))
		}
	})

	// =========================================================================
	// PHASE C: Remove reaction via MCP `message_react` (action: "remove")
	// =========================================================================
	t.Run("MCP message_react removes reaction cleanly", func(t *testing.T) {
		reactArgs := map[string]any{
			"workspace_id": ws.ID.String(),
			"message_uid":  msgUID,
			"emoji":        "🔥",
			"action":       "remove",
			"sender":       "ai-agent-007",
		}

		res, err := mcpServer.CallTool(ctx, "message_react", reactArgs)
		if err != nil {
			t.Fatalf("mcp message_react remove returned error: %v", err)
		}
		if res.IsError {
			t.Fatalf("mcp message_react tool error: %+v", res.Content)
		}

		reactions, err := chatRepo.GetReactions(ctx, ws.ID, msgUID)
		if err != nil {
			t.Fatalf("failed to get reactions: %v", err)
		}
		if len(reactions) != 0 {
			t.Fatalf("expected 0 reactions in DB, got %d", len(reactions))
		}
	})
}
