package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
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
	"github.com/redis/go-redis/v9"
)

type recordingPub struct {
	published []struct {
		subject string
		data    []byte
		traceID string
	}
}

func (r *recordingPub) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	r.published = append(r.published, struct {
		subject string
		data    []byte
		traceID string
	}{subject: subject, data: data, traceID: traceID})
	return nil
}

func TestDebounceHandoffIntegration(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("skipping: PostgreSQL not available")
	}

	redisAddr := os.Getenv("PERGO_TEST_REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "127.0.0.1:6379"
	}
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("skipping: Redis not available at %s: %v", redisAddr, err)
	}
	defer rdb.Close()

	ctx = context.Background()

	// Clean up database tables
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

	// 1. Create Workspace & Connection
	ws, err := wsRepo.Create(ctx, "Debounce & Human Handoff Workspace")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	kek := make([]byte, 32)
	copy(kek, []byte("dev-development-key-32-bytes-kek"))
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	connRepo := repository.NewConnectionRepository(pool, enc)

	conn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "Debounce WhatsApp",
		Slug:           "whatsapp",
		Channel:        "whatsapp",
		SenderIdentity: "+551100009999",
		Status:         "active",
		IsDefault:      true,
		Credentials:    []byte(`{"token":"dummy"}`),
	}
	if err := connRepo.Create(ctx, conn); err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}
	connID := conn.ID

	// 2. Setup Debouncer with 300ms window
	debounceTTL := 300 * time.Millisecond
	coalescedEvents := make(chan *inbound.CoalescedTurnEvent, 10)
	debPub := &recordingPub{}

	debouncer := inbound.NewDebouncer(
		rdb,
		inbound.WithDebounceTTL(debounceTTL),
		inbound.WithDebouncePublisher(debPub),
		inbound.WithDebounceHandler(func(ctx context.Context, ev *inbound.CoalescedTurnEvent) error {
			coalescedEvents <- ev
			return nil
		}),
	)
	defer debouncer.Close()

	// 3. Setup InboundProcessor
	inboundProc := inbound.NewInboundProcessor(nil, wsRepo, nil, nil, nil, nil, contactRepo, nil, nil)
	inboundProc.SetChatRepository(chatRepo)
	inboundProc.SetDebouncer(debouncer)

	// 4. Setup MCP Server
	mcpServer := mcppkg.NewServer(
		wsRepo,
		connRepo,
		contactRepo,
		auditRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
		[]byte("secret"),
		"http://localhost:8080",
		mcppkg.WithChatRepo(chatRepo),
	)

	// 5. Setup Admin Inbox Handler
	inboxPub := &recordingPub{}
	inboxHandler := &admin.InboxHandler{
		Repo:        auditRepo,
		ContactRepo: contactRepo,
		ChatRepo:    chatRepo,
		Connections: connRepo,
		Workspaces:  wsRepo,
		Publisher:   inboxPub,
	}

	customerPhone := "+5511988887777"
	customerName := "Clark Kent"

	// -------------------------------------------------------------
	// STEP A: Verify 3 rapid inbound messages within 2s produce
	//         exactly 1 coalesced turn.
	// -------------------------------------------------------------
	t.Log("Step A: Dispatching 3 rapid inbound messages within sliding window...")
	now := time.Now().UTC()

	ev1 := &inbound.InboundEvent{
		WorkspaceID:  ws.ID,
		ConnectionID: connID,
		MessageID:    "msg-burst-001",
		TraceID:      "trace-burst-001",
		Channel:      "whatsapp",
		From:         customerPhone,
		To:           "+551100009999",
		Body:         "Hello support",
		SenderName:   customerName,
		OccurredAt:   now,
	}
	ev2 := &inbound.InboundEvent{
		WorkspaceID:  ws.ID,
		ConnectionID: connID,
		MessageID:    "msg-burst-002",
		TraceID:      "trace-burst-002",
		Channel:      "whatsapp",
		From:         customerPhone,
		To:           "+551100009999",
		Body:         "I need help with my account",
		SenderName:   customerName,
		OccurredAt:   now.Add(30 * time.Millisecond),
	}
	ev3 := &inbound.InboundEvent{
		WorkspaceID:  ws.ID,
		ConnectionID: connID,
		MessageID:    "msg-burst-003",
		TraceID:      "trace-burst-003",
		Channel:      "whatsapp",
		From:         customerPhone,
		To:           "+551100009999",
		Body:         "Can someone reply?",
		SenderName:   customerName,
		OccurredAt:   now.Add(60 * time.Millisecond),
	}

	if err := inboundProc.Process(ctx, ev1); err != nil {
		t.Fatalf("process ev1: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	if err := inboundProc.Process(ctx, ev2); err != nil {
		t.Fatalf("process ev2: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	if err := inboundProc.Process(ctx, ev3); err != nil {
		t.Fatalf("process ev3: %v", err)
	}

	// Wait for the debounce window to expire and coalesce
	var coalesced *inbound.CoalescedTurnEvent
	select {
	case coalesced = <-coalescedEvents:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: expected 1 coalesced turn event from 3 rapid messages")
	}

	if coalesced.MessageCount != 3 {
		t.Fatalf("expected message_count = 3, got %d", coalesced.MessageCount)
	}

	expectedCoalescedBody := "Hello support\n\nI need help with my account\n\nCan someone reply?"
	if coalesced.Body != expectedCoalescedBody {
		t.Errorf("coalesced body mismatch:\nexpected: %q\ngot:      %q", expectedCoalescedBody, coalesced.Body)
	}

	if len(coalesced.MessageIDs) != 3 || coalesced.MessageIDs[0] != "msg-burst-001" || coalesced.MessageIDs[2] != "msg-burst-003" {
		t.Errorf("unexpected message IDs: %+v", coalesced.MessageIDs)
	}

	// Verify no extra events were emitted
	time.Sleep(200 * time.Millisecond)
	if len(coalescedEvents) != 0 {
		t.Fatalf("expected exactly 1 coalesced event, but found %d extra events", len(coalescedEvents))
	}

	// Verify contact and chat state in database
	contact, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp", customerPhone, "", "", "")
	if err != nil {
		t.Fatalf("failed to resolve contact: %v", err)
	}

	chat, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &connID, contact.ID)
	if err != nil {
		t.Fatalf("failed to find chat: %v", err)
	}
	if chat.UnreadCount != 3 {
		t.Errorf("expected unread count 3, got %d", chat.UnreadCount)
	}
	if chat.AIDisabled {
		t.Errorf("expected AI to be enabled initially")
	}

	// Verify all 3 individual messages are present in chat_messages table
	msgs, err := chatRepo.ListChatMessages(ctx, ws.ID, chat.ID, "", "", 10)
	if err != nil {
		t.Fatalf("failed to list chat messages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 chat messages in DB, got %d", len(msgs))
	}

	// -------------------------------------------------------------
	// STEP B: Verify human outbound reply sets ai_disabled = true
	//         and silences AI.
	// -------------------------------------------------------------
	t.Log("Step B: Sending human outbound reply to trigger human takeover...")
	e := echo.New()
	formVals := url.Values{}
	formVals.Set("contact", customerPhone)
	formVals.Set("channel", "whatsapp")
	formVals.Set("recipient_identity", "+551100009999")
	formVals.Set("body", "Hello Clark, this is agent Lois taking over.")

	sendReq := httptest.NewRequest(http.MethodPost, "/admin/inbox/send", strings.NewReader(formVals.Encode()))
	sendReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqCtx := tenant.WithWorkspaceID(sendReq.Context(), ws.ID)
	reqCtx = domain.ContextWithWorkspaceID(reqCtx, ws.ID)
	reqCtx = i18n.WithLocale(reqCtx, "pt-BR")
	sendReq = sendReq.WithContext(reqCtx)

	sendRec := httptest.NewRecorder()
	sendCtx := e.NewContext(sendReq, sendRec)

	if err := inboxHandler.SendMessage(sendCtx); err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	if sendRec.Code != http.StatusNoContent {
		t.Fatalf("expected HTTP 204 from SendMessage, got %d", sendRec.Code)
	}

	// Verify chat updated: ai_disabled = true
	updatedChat, err := chatRepo.GetChat(ctx, ws.ID, chat.ID)
	if err != nil {
		t.Fatalf("failed to get updated chat: %v", err)
	}
	if !updatedChat.AIDisabled {
		t.Errorf("expected ai_disabled = true after human agent reply")
	}

	// Verify contact bot_active = false
	updatedContact, err := contactRepo.GetByID(ctx, ws.ID, contact.ID)
	if err != nil {
		t.Fatalf("failed to get updated contact: %v", err)
	}
	if updatedContact.BotActive {
		t.Errorf("expected contact bot_active = false after human takeover")
	}

	// Verify handoff event was published
	var foundHandoff bool
	for _, p := range inboxPub.published {
		if p.subject == "chat.handoff.human_takeover" {
			foundHandoff = true
			var payload map[string]interface{}
			_ = json.Unmarshal(p.data, &payload)
			if payload["event"] != "chat.handoff.human_takeover" {
				t.Errorf("expected event chat.handoff.human_takeover, got %v", payload["event"])
			}
			if payload["chat_id"] != chat.ID.String() {
				t.Errorf("expected chat_id %s, got %v", chat.ID.String(), payload["chat_id"])
			}
		}
	}
	if !foundHandoff {
		t.Errorf("expected chat.handoff.human_takeover event to be published by SendMessage")
	}

	// Verify Web Console renders "AI Paused (Human Takeover)" badge and "Resume AI" button
	panelReq := httptest.NewRequest(http.MethodGet, "/admin/inbox/chat?contact_id="+contact.ID.String(), nil)
	panelReq.Header.Set("HX-Request", "true")
	panelReq = panelReq.WithContext(reqCtx)
	panelRec := httptest.NewRecorder()
	panelCtx := e.NewContext(panelReq, panelRec)

	if err := inboxHandler.ChatPanel(panelCtx); err != nil {
		t.Fatalf("ChatPanel failed: %v", err)
	}
	if panelRec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 from ChatPanel, got %d", panelRec.Code)
	}
	panelHTML := panelRec.Body.String()
	if !strings.Contains(panelHTML, "AI Paused (Human Takeover)") {
		t.Errorf("expected ChatPanel to render 'AI Paused (Human Takeover)' badge, got:\n%s", panelHTML)
	}
	if !strings.Contains(panelHTML, "Resume AI") {
		t.Errorf("expected ChatPanel to render 'Resume AI' button, got:\n%s", panelHTML)
	}
	if !strings.Contains(panelHTML, "enable-ai?enabled=true") {
		t.Errorf("expected Resume AI button to call enable-ai endpoint, got:\n%s", panelHTML)
	}

	// Now send a 4th inbound message while ai_disabled == true
	ev4 := &inbound.InboundEvent{
		WorkspaceID:  ws.ID,
		ConnectionID: connID,
		MessageID:    "msg-burst-004",
		TraceID:      "trace-burst-004",
		Channel:      "whatsapp",
		From:         customerPhone,
		To:           "+551100009999",
		Body:         "Message 4: Thanks Lois, I appreciate the human help.",
		SenderName:   customerName,
		OccurredAt:   time.Now().UTC(),
	}

	if err := inboundProc.Process(ctx, ev4); err != nil {
		t.Fatalf("process ev4: %v", err)
	}

	// Wait past debounce window — verify AI is silenced (no coalesced turn emitted)
	time.Sleep(debounceTTL + 150*time.Millisecond)
	if len(coalescedEvents) != 0 {
		t.Fatalf("expected AI to be silenced while ai_disabled=true, but received coalesced event: %+v", <-coalescedEvents)
	}

	// -------------------------------------------------------------
	// STEP C: Verify resuming AI re-enables automated response dispatch.
	// -------------------------------------------------------------
	t.Log("Step C: Resuming AI via chat_enable_ai MCP tool...")

	enableRes, err := mcpServer.CallTool(ctx, "chat_enable_ai", map[string]any{
		"workspace_id": ws.ID.String(),
		"chat_id":      chat.ID.String(),
		"enabled":      true,
	})
	if err != nil {
		t.Fatalf("chat_enable_ai MCP call failed: %v", err)
	}
	if enableRes.IsError {
		t.Fatalf("chat_enable_ai returned error: %+v", enableRes.Content)
	}

	// Verify chat state in DB has ai_disabled = false
	resumedChat, err := chatRepo.GetChat(ctx, ws.ID, chat.ID)
	if err != nil {
		t.Fatalf("get resumed chat: %v", err)
	}
	if resumedChat.AIDisabled {
		t.Errorf("expected ai_disabled = false after resuming AI")
	}

	// Now send 5th inbound message — should trigger automated AI debounce & dispatch!
	t.Log("Step C.2: Sending inbound message 5 with AI re-enabled...")
	ev5 := &inbound.InboundEvent{
		WorkspaceID:  ws.ID,
		ConnectionID: connID,
		MessageID:    "msg-burst-005",
		TraceID:      "trace-burst-005",
		Channel:      "whatsapp",
		From:         customerPhone,
		To:           "+551100009999",
		Body:         "Message 5: Hello AI, are you back online?",
		SenderName:   customerName,
		OccurredAt:   time.Now().UTC(),
	}

	if err := inboundProc.Process(ctx, ev5); err != nil {
		t.Fatalf("process ev5: %v", err)
	}

	// Wait for debounce window to expire
	var coalescedResumed *inbound.CoalescedTurnEvent
	select {
	case coalescedResumed = <-coalescedEvents:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: expected coalesced turn event after resuming AI")
	}

	if coalescedResumed.MessageCount != 1 {
		t.Fatalf("expected 1 message in resumed turn, got %d", coalescedResumed.MessageCount)
	}
	if coalescedResumed.Body != "Message 5: Hello AI, are you back online?" {
		t.Errorf("unexpected resumed body: %q", coalescedResumed.Body)
	}

	t.Log("TestDebounceHandoffIntegration passed completely!")
}
