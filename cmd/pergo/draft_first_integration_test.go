package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/pablojhp.pergo/internal/api/handler/admin"
	mcppkg "github.com/pablojhp.pergo/internal/api/mcp"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/i18n"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
)

type trackingMsgPublisher struct {
	mu       sync.Mutex
	messages []struct {
		Subject string
		Data    []byte
		TraceID string
	}
}

func (p *trackingMsgPublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.messages = append(p.messages, struct {
		Subject string
		Data    []byte
		TraceID string
	}{
		Subject: subject,
		Data:    data,
		TraceID: traceID,
	})
	return nil
}

func (p *trackingMsgPublisher) CountBySubject(subject string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	count := 0
	for _, m := range p.messages {
		if m.Subject == subject {
			count++
		}
	}
	return count
}

func TestDraftFirstInternalNotesIntegration(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("skipping: PostgreSQL not available")
	}

	ctx := context.Background()

	// Clean up
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

	// 1. Create Workspace & WhatsApp Cloud Connection
	ws, err := wsRepo.Create(ctx, "Draft First Safety Workspace")
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
		Name:           "WhatsApp Cloud Line",
		Slug:           "whatsapp-cloud",
		Channel:        "whatsapp_cloud",
		SenderIdentity: "+551140001000",
		Status:         "active",
		IsDefault:      true,
		Credentials:    []byte(`{"token":"dummy_waba_token"}`),
	}
	if err := connRepo.Create(ctx, conn); err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}
	connID := conn.ID

	// 2. Setup InboundProcessor
	inboundProc := inbound.NewInboundProcessor(nil, wsRepo, nil, nil, nil, nil, contactRepo, nil, nil)
	inboundProc.SetChatRepository(chatRepo)

	// 3. Setup Tracking Publisher
	pub := &trackingMsgPublisher{}

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
	inboxHandler := &admin.InboxHandler{
		Repo:        auditRepo,
		ContactRepo: contactRepo,
		ChatRepo:    chatRepo,
		Connections: connRepo,
		Workspaces:  wsRepo,
		Publisher:   pub,
	}

	// Step A: Inbound Customer Inquiry
	customerPhone := "+5511988887777"
	customerName := "Tony Stark"
	inquiryText := "What is the status of my shipment?"

	inboundEv := &inbound.InboundEvent{
		WorkspaceID:  ws.ID,
		ConnectionID: connID,
		MessageID:    "wamid-inquiry-9001",
		TraceID:      "trace-inquiry-9001",
		Channel:      "whatsapp_cloud",
		From:         customerPhone,
		To:           "+551140001000",
		Body:         inquiryText,
		SenderName:   customerName,
		OccurredAt:   time.Now().UTC(),
	}

	if err := inboundProc.Process(ctx, inboundEv); err != nil {
		t.Fatalf("failed to process customer inbound: %v", err)
	}

	// Resolve Contact & Chat
	contact, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp_cloud", customerPhone, "", "", "")
	if err != nil {
		t.Fatalf("failed to resolve contact: %v", err)
	}

	chat, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &connID, contact.ID)
	if err != nil {
		t.Fatalf("failed to find chat: %v", err)
	}

	// Populate audit log for inbound message
	auditPayload, _ := json.Marshal(map[string]any{
		"channel": "whatsapp_cloud",
		"from":    customerPhone,
		"body":    inquiryText,
	})
	_, _ = pool.Exec(ctx, `
		INSERT INTO audit_logs (id, workspace_id, trace_id, event_type, payload, created_at)
		VALUES (gen_random_uuid(), $1, 'trace-inquiry-9001', 'inbound_message', $2, NOW())
	`, ws.ID, auditPayload)

	// Step B: AI Agent calls MCP Tool `chat_create_draft_note`
	aiDraftText := "Suggested response: Hello Tony! Your Arc Reactor package is in transit and arriving in 25 minutes."
	aiAuthor := "Jarvis AI Assistant"

	draftArgs := map[string]any{
		"workspace_id": ws.ID.String(),
		"chat_id":      chat.ID.String(),
		"body":         aiDraftText,
		"author_name":  aiAuthor,
	}

	draftRes, err := mcpServer.CallTool(ctx, "chat_create_draft_note", draftArgs)
	if err != nil {
		t.Fatalf("mcp chat_create_draft_note error: %v", err)
	}
	if draftRes.IsError {
		t.Fatalf("mcp chat_create_draft_note returned tool error: %+v", draftRes.Content)
	}

	draftText := draftRes.Content[0].(mcp.TextContent).Text
	var createdNote domain.ChatMessage
	if err := json.Unmarshal([]byte(draftText), &createdNote); err != nil {
		t.Fatalf("failed to parse chat_create_draft_note output: %v", err)
	}

	if createdNote.ChatID != chat.ID {
		t.Errorf("expected chat ID %s, got %s", chat.ID, createdNote.ChatID)
	}
	if createdNote.Direction != domain.DirectionInternalNote {
		t.Errorf("expected direction 'internal_note', got %s", createdNote.Direction)
	}
	if !createdNote.IsPrivate {
		t.Errorf("expected is_private=true on draft note")
	}
	if createdNote.Body != aiDraftText {
		t.Errorf("expected body %q, got %q", aiDraftText, createdNote.Body)
	}
	if createdNote.SenderName != aiAuthor {
		t.Errorf("expected author %q, got %q", aiAuthor, createdNote.SenderName)
	}

	// Step C: Verify 0 Channel Outbound Dispatches Triggered by AI Draft Note
	outboundCount := pub.CountBySubject("messages.outbound")
	if outboundCount != 0 {
		t.Fatalf("SAFETY VIOLATION: AI draft note triggered %d channel outbound dispatches (expected 0)", outboundCount)
	}

	// Step D: Verify Note is Visible in MCP `chat_history`
	histArgs := map[string]any{
		"workspace_id": ws.ID.String(),
		"chat_id":      chat.ID.String(),
		"limit":        10,
	}
	histRes, err := mcpServer.CallTool(ctx, "chat_history", histArgs)
	if err != nil {
		t.Fatalf("mcp chat_history error: %v", err)
	}
	var histParsed struct {
		Messages []domain.ChatMessage `json:"messages"`
		Count    int                  `json:"count"`
	}
	_ = json.Unmarshal([]byte(histRes.Content[0].(mcp.TextContent).Text), &histParsed)

	if histParsed.Count != 2 {
		t.Fatalf("expected 2 messages in chat_history (inbound + internal_note), got %d", histParsed.Count)
	}
	if histParsed.Messages[1].Direction != domain.DirectionInternalNote {
		t.Errorf("expected second message to be internal_note, got %s", histParsed.Messages[1].Direction)
	}
	if !histParsed.Messages[1].IsPrivate {
		t.Errorf("expected is_private=true for second message in chat_history")
	}
	if histParsed.Messages[1].Body != aiDraftText {
		t.Errorf("expected body %q, got %q", aiDraftText, histParsed.Messages[1].Body)
	}

	// Step E: Verify Web Console HTML (`/admin/inbox/chat?contact_id=...`)
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
		t.Fatalf("inboxHandler.ChatPanel failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 from ChatPanel, got %d", rec.Code)
	}

	htmlBody := rec.Body.String()

	// 1. Verify draft text is displayed in Web Console
	if !strings.Contains(htmlBody, aiDraftText) {
		t.Errorf("expected Web Console HTML to contain draft note text %q", aiDraftText)
	}
	// 2. Verify author attribution is displayed
	if !strings.Contains(htmlBody, aiAuthor) {
		t.Errorf("expected Web Console HTML to contain author attribution %q", aiAuthor)
	}
	// 3. Verify amber tinting and border styling
	if !strings.Contains(htmlBody, "bg-amber-500/10") || !strings.Contains(htmlBody, "border-amber-300") {
		t.Errorf("expected Web Console HTML to contain amber styling ('bg-amber-500/10' and 'border-amber-300')")
	}
	// 4. Verify lock icon / internal note label
	if !strings.Contains(htmlBody, "Nota Interna") {
		t.Errorf("expected Web Console HTML to contain 'Nota Interna' label with lock icon")
	}
	// 5. Verify "Approve & Send" button with data-draft attribute
	if !strings.Contains(htmlBody, "Approve & Send") || !strings.Contains(htmlBody, "btn-approve-send") {
		t.Errorf("expected Web Console HTML to contain 'Approve & Send' button")
	}
	if !strings.Contains(htmlBody, "data-draft=") {
		t.Errorf("expected 'Approve & Send' button to contain data-draft attribute for composer copying")
	}

	// Step F: Human Operator clicks "Approve & Send" -> Outbound dispatch occurs
	formVals := url.Values{}
	formVals.Set("contact", customerPhone)
	formVals.Set("channel", "whatsapp_cloud")
	formVals.Set("recipient_identity", "+551140001000")
	formVals.Set("body", aiDraftText)

	sendReq := httptest.NewRequest(http.MethodPost, "/admin/inbox/send", strings.NewReader(formVals.Encode()))
	sendReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sendReq = sendReq.WithContext(reqCtx)
	sendRec := httptest.NewRecorder()
	sendCtx := e.NewContext(sendReq, sendRec)

	if err := inboxHandler.SendMessage(sendCtx); err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	if sendRec.Code != http.StatusNoContent {
		t.Fatalf("expected HTTP 204 from SendMessage, got %d", sendRec.Code)
	}

	// Outbound message was now dispatched via NATS JetStream
	if pub.CountBySubject("messages.outbound") != 1 {
		t.Errorf("expected exactly 1 outbound dispatch after human approval, got %d", pub.CountBySubject("messages.outbound"))
	}

	// Human handoff triggered: AI disabled
	updatedChat, err := chatRepo.GetChat(ctx, ws.ID, chat.ID)
	if err != nil {
		t.Fatalf("failed to get updated chat: %v", err)
	}
	if !updatedChat.AIDisabled {
		t.Errorf("expected AI to be disabled (human handoff triggered) after human operator approves and sends")
	}

	// Chat history now contains 3 messages (inbound, draft note, outbound reply)
	histRes3, err := mcpServer.CallTool(ctx, "chat_history", histArgs)
	if err != nil {
		t.Fatalf("chat_history error: %v", err)
	}
	var histParsed3 struct {
		Messages []domain.ChatMessage `json:"messages"`
		Count    int                  `json:"count"`
	}
	_ = json.Unmarshal([]byte(histRes3.Content[0].(mcp.TextContent).Text), &histParsed3)
	if histParsed3.Count != 3 {
		t.Fatalf("expected 3 messages in chat_history after approval and dispatch, got %d", histParsed3.Count)
	}
}
