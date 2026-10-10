package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

func TestChatTracerBulletIntegration(t *testing.T) {
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

	// 1. Create Workspace & Connection
	ws, err := wsRepo.Create(ctx, "Tracer Bullet Integration Workspace")
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
		Name:           "Tracer WhatsApp",
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

	// 2. Setup InboundProcessor
	inboundProc := inbound.NewInboundProcessor(nil, wsRepo, nil, nil, nil, nil, contactRepo, nil, nil)
	inboundProc.SetChatRepository(chatRepo)

	// 3. Setup MCP Server
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

	// 4. Setup Admin Inbox Handler
	inboxHandler := &admin.InboxHandler{
		Repo:        auditRepo,
		ContactRepo: contactRepo,
		ChatRepo:    chatRepo,
		Connections: connRepo,
		Workspaces:  wsRepo,
	}

	// Step A: Simulate Inbound Customer Message
	inboundMsgID := "tracer-wamid-1001"
	customerPhone := "+5511977771234"
	customerName := "Bruce Wayne"
	messageText := "Hello! This is a tracer bullet test message from customer."

	inboundEv := &inbound.InboundEvent{
		WorkspaceID:  ws.ID,
		ConnectionID: connID,
		MessageID:    inboundMsgID,
		TraceID:      "trace-tracer-1001",
		Channel:      "whatsapp",
		From:         customerPhone,
		To:           "+551100009999",
		Body:         messageText,
		SenderName:   customerName,
		OccurredAt:   time.Now().UTC(),
	}

	if err := inboundProc.Process(ctx, inboundEv); err != nil {
		t.Fatalf("failed to process inbound event: %v", err)
	}

	// Verify Contact and Chat were created in Database
	contact, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp", customerPhone, "", "", "")
	if err != nil {
		t.Fatalf("failed to resolve contact: %v", err)
	}
	if contact.Name != customerName {
		t.Errorf("expected contact name %s, got %s", customerName, contact.Name)
	}

	chat, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &connID, contact.ID)
	if err != nil {
		t.Fatalf("failed to find chat: %v", err)
	}
	if chat.UnreadCount != 1 {
		t.Errorf("expected unread count 1, got %d", chat.UnreadCount)
	}

	// Step B: Verify Message is Retrievable via MCP `list_chats`
	listArgs := map[string]any{
		"workspace_id": ws.ID.String(),
		"unread":       true,
		"phone":        "977771234",
	}

	listRes, err := mcpServer.CallTool(ctx, "list_chats", listArgs)
	if err != nil {
		t.Fatalf("mcp list_chats tool call error: %v", err)
	}
	if listRes.IsError {
		t.Fatalf("mcp list_chats returned tool error: %+v", listRes.Content)
	}
	listText := listRes.Content[0].(mcp.TextContent).Text
	var listParsed struct {
		Chats []mcppkg.ChatSummaryDTO `json:"chats"`
		Count int                     `json:"count"`
	}
	if err := json.Unmarshal([]byte(listText), &listParsed); err != nil {
		t.Fatalf("failed to parse list_chats response: %v", err)
	}
	if listParsed.Count != 1 {
		t.Fatalf("expected 1 chat returned via MCP list_chats, got %d", listParsed.Count)
	}
	if listParsed.Chats[0].ContactName != customerName {
		t.Errorf("expected contact name %s in MCP list_chats, got %s", customerName, listParsed.Chats[0].ContactName)
	}

	// Step C: Verify Message is Retrievable via MCP `chat_history`
	histArgs := map[string]any{
		"workspace_id": ws.ID.String(),
		"chat_id":      chat.ID.String(),
	}

	histRes, err := mcpServer.CallTool(ctx, "chat_history", histArgs)
	if err != nil {
		t.Fatalf("mcp chat_history tool call error: %v", err)
	}
	if histRes.IsError {
		t.Fatalf("mcp chat_history returned tool error: %+v", histRes.Content)
	}
	histText := histRes.Content[0].(mcp.TextContent).Text
	var histParsed struct {
		Messages []domain.ChatMessage `json:"messages"`
		Count    int                  `json:"count"`
	}
	if err := json.Unmarshal([]byte(histText), &histParsed); err != nil {
		t.Fatalf("failed to parse chat_history response: %v", err)
	}
	if histParsed.Count != 1 {
		t.Fatalf("expected 1 message in MCP chat_history, got %d", histParsed.Count)
	}
	if histParsed.Messages[0].UID != inboundMsgID {
		t.Errorf("expected UID %s, got %s", inboundMsgID, histParsed.Messages[0].UID)
	}
	if histParsed.Messages[0].Body != messageText {
		t.Errorf("expected body %s, got %s", messageText, histParsed.Messages[0].Body)
	}
	if histParsed.Messages[0].Direction != domain.DirectionInbound {
		t.Errorf("expected direction inbound, got %s", histParsed.Messages[0].Direction)
	}

	// Step D: Verify HTML Web Console `/admin/inbox/chat`
	auditPayload, _ := json.Marshal(map[string]any{
		"channel": "whatsapp",
		"from":    customerPhone,
		"body":    messageText,
	})
	_, _ = pool.Exec(ctx, `
		INSERT INTO audit_logs (id, workspace_id, trace_id, event_type, payload, created_at)
		VALUES (gen_random_uuid(), $1, 'trace-tracer-1001', 'inbound_message', $2, NOW())
	`, ws.ID, auditPayload)

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

	htmlBody := rec.Body.String()
	// Assert contact name appears in header and CRM metadata
	if !strings.Contains(htmlBody, customerName) {
		t.Errorf("expected HTML to contain contact name %s", customerName)
	}
	// Assert customer phone appears in CRM metadata
	if !strings.Contains(htmlBody, customerPhone) {
		t.Errorf("expected HTML to contain customer phone %s", customerPhone)
	}
	// Assert 3-column Paper-Calm layout classes are present
	if !strings.Contains(htmlBody, "chat-thread") || !strings.Contains(htmlBody, "contact-crm-sidebar") {
		t.Errorf("expected HTML to contain 3-column layout classes ('chat-thread' and 'contact-crm-sidebar')")
	}

	// Step E: Verify Outbound Agent Dispatch & AI Handoff
	inboxHandler.Publisher = &mockMsgPublisher{}
	outboundBody := "Hello Bruce, your request is being processed."
	formVals := url.Values{}
	formVals.Set("contact", customerPhone)
	formVals.Set("channel", "whatsapp")
	formVals.Set("recipient_identity", "+551100009999")
	formVals.Set("body", outboundBody)
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

	// Verify chat updated: AI is disabled on chat (human handoff triggered)
	updatedChat, err := chatRepo.GetChat(ctx, ws.ID, chat.ID)
	if err != nil {
		t.Fatalf("failed to get updated chat: %v", err)
	}
	if !updatedChat.AIDisabled {
		t.Errorf("expected AI to be disabled after human agent dispatch")
	}

	// Verify MCP chat_history now returns 2 messages (inbound + outbound)
	histRes2, err := mcpServer.CallTool(ctx, "chat_history", histArgs)
	if err != nil {
		t.Fatalf("mcp chat_history tool call error: %v", err)
	}
	var histParsed2 struct {
		Messages []domain.ChatMessage `json:"messages"`
		Count    int                  `json:"count"`
	}
	_ = json.Unmarshal([]byte(histRes2.Content[0].(mcp.TextContent).Text), &histParsed2)
	if histParsed2.Count != 2 {
		t.Fatalf("expected 2 messages in MCP chat_history after outbound reply, got %d", histParsed2.Count)
	}
	if histParsed2.Messages[1].Direction != domain.DirectionOutbound {
		t.Errorf("expected direction outbound, got %s", histParsed2.Messages[1].Direction)
	}
	if histParsed2.Messages[1].Body != outboundBody {
		t.Errorf("expected body %s, got %s", outboundBody, histParsed2.Messages[1].Body)
	}
}

type mockMsgPublisher struct{}

func (m *mockMsgPublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	return nil
}

