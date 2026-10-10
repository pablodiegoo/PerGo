package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	mcppkg "github.com/pablojhp.pergo/internal/api/mcp"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/outbound"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/session"
)

type mockCompliancePublisher struct {
	published [][]byte
}

func (p *mockCompliancePublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	p.published = append(p.published, data)
	return nil
}

func TestWABAComplianceIntegration(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("skipping: PostgreSQL not available")
	}

	ctx := context.Background()

	// Clean up database tables
	_, _ = pool.Exec(ctx, "DELETE FROM chat_messages")
	_, _ = pool.Exec(ctx, "DELETE FROM chats")
	_, _ = pool.Exec(ctx, "DELETE FROM contact_identities")
	_, _ = pool.Exec(ctx, "DELETE FROM contacts")
	_, _ = pool.Exec(ctx, "DELETE FROM waba_templates")
	_, _ = pool.Exec(ctx, "DELETE FROM connections")
	_, _ = pool.Exec(ctx, "DELETE FROM recipient_sessions")
	_, _ = pool.Exec(ctx, "DELETE FROM workspaces")

	wsRepo := repository.NewWorkspaceRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	chatRepo := repository.NewChatRepository(pool)
	tmplRepo := repository.NewWABATemplateRepository(pool)
	sessRepo := repository.NewRecipientSessionRepository(pool)

	kek := make([]byte, 32)
	copy(kek, []byte("dev-development-key-32-bytes-kek"))
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	connRepo := repository.NewConnectionRepository(pool, enc)

	// 1. Create Tenant Workspace
	ws, err := wsRepo.Create(ctx, "WABA Compliance Workspace")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	// 2. Create Official WABA Connection
	wabaSender := "+15550001111"
	wabaConn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "Official Meta WABA",
		Slug:           "whatsapp_cloud",
		Channel:        "whatsapp_cloud",
		SenderIdentity: wabaSender,
		Status:         "active",
		Credentials:    []byte(`{"phone_number_id":"1122334455","waba_account_id":"9988776655","token":"test_token"}`),
		IsDefault:      true,
	}
	if err := connRepo.Create(ctx, wabaConn); err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}
	connID := wabaConn.ID

	// 3. Register Approved Meta Template
	templateName := "order_status_update"
	templateLanguage := "pt_BR"
	tmplComponents := `[
		{
			"type": "HEADER",
			"format": "TEXT",
			"text": "Atualização do seu pedido {{1}}"
		},
		{
			"type": "BODY",
			"text": "Olá {{1}}, seu pedido {{2}} foi despachado com sucesso!"
		}
	]`
	_, err = tmplRepo.Create(ctx, &repository.WABATemplate{
		WorkspaceID:    ws.ID,
		ConnectionID:   connID,
		MetaTemplateID: "meta-order-status-001",
		Name:           templateName,
		Language:       templateLanguage,
		Status:         "APPROVED",
		Category:       "UTILITY",
		Components:     json.RawMessage(tmplComponents),
	})
	if err != nil {
		t.Fatalf("failed to create template: %v", err)
	}

	// 4. Wire Processors & MCP Server
	mockPub := &mockCompliancePublisher{}
	windowChecker := session.NewWindowChecker(sessRepo)

	outboundProc := outbound.NewProcessor(nil, nil, connRepo, mockPub)
	outboundProc.SetWindowChecker(windowChecker)
	outboundProc.SetTemplateRepository(tmplRepo)
	outboundProc.SetChatRepository(chatRepo)
	outboundProc.SetContactRepository(contactRepo)

	inboundProc := inbound.NewInboundProcessor(nil, wsRepo, nil, mockPub, nil, sessRepo, contactRepo, nil, nil)
	inboundProc.SetChatRepository(chatRepo)

	mcpServer := mcppkg.NewServer(
		wsRepo,
		connRepo,
		contactRepo,
		nil,
		outboundProc,
		nil,
		nil,
		nil,
		nil,
		[]byte("secret"),
		"http://localhost:8080",
		mcppkg.WithChatRepo(chatRepo),
		mcppkg.WithWABATemplateRepo(tmplRepo),
	)

	// 5. Create Contact and Initial Chat with EXPIRED 24h Customer Service Window
	customerPhone := "+5511999995555"
	contact, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp_cloud", customerPhone, "Customer Maria", "", customerPhone)
	if err != nil {
		t.Fatalf("failed to resolve contact: %v", err)
	}

	chat, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &connID, contact.ID)
	if err != nil {
		t.Fatalf("failed to find or create chat: %v", err)
	}

	// Simulate expired service window (expired 2 hours ago)
	expiredAt := time.Now().UTC().Add(-2 * time.Hour)
	if err := chatRepo.UpdateServiceWindow(ctx, ws.ID, chat.ID, &expiredAt); err != nil {
		t.Fatalf("failed to update service window: %v", err)
	}

	// Also record expired session in sessRepo
	sessKey := domain.NewSessionKey(ws.ID, customerPhone, "whatsapp_cloud", wabaSender)
	_ = sessRepo.Upsert(ctx, sessKey, expiredAt.Add(-24*time.Hour), "standard")

	// Verify initial state: Window is closed
	chatCheck, err := chatRepo.GetChat(ctx, ws.ID, chat.ID)
	if err != nil {
		t.Fatalf("failed to get chat: %v", err)
	}
	if chatCheck.IsServiceWindowOpen() {
		t.Fatalf("expected chat service window to be closed initially")
	}

	// =========================================================================
	// SCENARIO 1: Sending Free-Text Message Outside 24h Window FAILS
	// =========================================================================
	t.Run("Freeform text rejected with compliance guidance outside 24h window", func(t *testing.T) {
		sendArgs := map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
			"message":      "Oi Maria, temos uma promoção para você!",
			"sender_name":  "Consultor",
		}

		res, err := mcpServer.CallTool(ctx, "waba_chat_send_message", sendArgs)
		if err != nil {
			t.Fatalf("CallTool waba_chat_send_message failed: %v", err)
		}
		if !res.IsError {
			t.Fatalf("expected waba_chat_send_message to fail when window is closed, but got success: %+v", res.Content)
		}

		errMsg := res.Content[0].(mcp.TextContent).Text
		if !strings.Contains(errMsg, "24-hour customer service window is closed") {
			t.Errorf("expected error to explain 24h window is closed, got: %s", errMsg)
		}
		if !strings.Contains(errMsg, "waba_chat_send_template") {
			t.Errorf("expected error to direct caller to 'waba_chat_send_template', got: %s", errMsg)
		}

		// Verify no message was added to chat
		msgs, _ := chatRepo.ListChatMessages(ctx, ws.ID, chat.ID, "", "", 10)
		if len(msgs) != 0 {
			t.Errorf("expected 0 messages in chat after rejection, got %d", len(msgs))
		}
	})

	// =========================================================================
	// SCENARIO 2: Sending Approved Template Outside 24h Window SUCCEEDS
	// =========================================================================
	t.Run("Approved HSM template message succeeds outside 24h window", func(t *testing.T) {
		templateArgs := map[string]any{
			"workspace_id":  ws.ID.String(),
			"chat_id":       chat.ID.String(),
			"template_name": templateName,
			"language":      templateLanguage,
			"parameters": map[string]any{
				"header": []any{"#98765"},
				"body":   []any{"Maria", "#98765"},
			},
			"sender_name": "Sistema Automatizado",
		}

		res, err := mcpServer.CallTool(ctx, "waba_chat_send_template", templateArgs)
		if err != nil {
			t.Fatalf("CallTool waba_chat_send_template failed: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected waba_chat_send_template to succeed, got error: %+v", res.Content)
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &parsed); err != nil {
			t.Fatalf("failed to unmarshal result: %v", err)
		}
		if parsed["success"] != true {
			t.Errorf("expected success true, got %v", parsed["success"])
		}

		// Verify outbound message was added to chat history
		msgs, err := chatRepo.ListChatMessages(ctx, ws.ID, chat.ID, "", "", 10)
		if err != nil {
			t.Fatalf("failed to list chat messages: %v", err)
		}
		if len(msgs) != 1 {
			t.Fatalf("expected 1 outbound message recorded, got %d", len(msgs))
		}
		if msgs[0].Direction != string(domain.DirectionOutbound) {
			t.Errorf("expected outbound direction, got %s", msgs[0].Direction)
		}
		if !strings.Contains(msgs[0].Body, "order_status_update") {
			t.Errorf("expected body to reference template name, got: %s", msgs[0].Body)
		}
	})

	// =========================================================================
	// SCENARIO 3: Customer Replies -> Re-opens 24-Hour Customer Service Window
	// =========================================================================
	t.Run("Customer inbound reply re-opens 24h customer service window", func(t *testing.T) {
		now := time.Now().UTC()
		inboundEv := &inbound.InboundEvent{
			WorkspaceID:  ws.ID,
			ConnectionID: connID,
			MessageID:    "wamid-customer-reply-101",
			TraceID:      "trace-reply-101",
			Channel:      "whatsapp_cloud",
			From:         customerPhone,
			To:           wabaSender,
			Body:         "Muito obrigada! Quando chega?",
			SenderName:   "Maria",
			OccurredAt:   now,
		}

		if err := inboundProc.Process(ctx, inboundEv); err != nil {
			t.Fatalf("inbound processor failed: %v", err)
		}

		// Reload chat and verify window is now open (+24h)
		chatUpdated, err := chatRepo.GetChat(ctx, ws.ID, chat.ID)
		if err != nil {
			t.Fatalf("failed to reload chat: %v", err)
		}

		if chatUpdated.ServiceWindowExpiresAt == nil {
			t.Fatalf("expected service window expires at to be set")
		}
		if !chatUpdated.IsServiceWindowOpen() {
			t.Errorf("expected chat service window to be open after customer reply")
		}

		remaining := chatUpdated.ServiceWindowExpiresAt.Sub(now)
		if remaining < 23*time.Hour || remaining > 25*time.Hour {
			t.Errorf("expected ~24h window expiration, got remaining: %v", remaining)
		}

		// Verify inbound message added to chat
		msgs, _ := chatRepo.ListChatMessages(ctx, ws.ID, chat.ID, "", "", 10)
		if len(msgs) != 2 { // 1 template outbound + 1 customer reply inbound
			t.Errorf("expected 2 messages in chat, got %d", len(msgs))
		}
	})

	// =========================================================================
	// SCENARIO 4: Freeform Text Message NOW SUCCEEDS inside Open Window
	// =========================================================================
	t.Run("Freeform text succeeds inside open 24h customer service window", func(t *testing.T) {
		sendArgs := map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
			"message":      "Chega amanhã até às 18h!",
			"sender_name":  "Consultor João",
		}

		res, err := mcpServer.CallTool(ctx, "waba_chat_send_message", sendArgs)
		if err != nil {
			t.Fatalf("CallTool waba_chat_send_message failed: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected freeform message to succeed inside open window, got error: %+v", res.Content)
		}

		var parsed map[string]interface{}
		_ = json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &parsed)
		if parsed["success"] != true {
			t.Errorf("expected success true, got %v", parsed["success"])
		}

		// Verify 3rd message added to chat history
		msgs, _ := chatRepo.ListChatMessages(ctx, ws.ID, chat.ID, "", "", 10)
		if len(msgs) != 3 {
			t.Errorf("expected 3 total messages in chat, got %d", len(msgs))
		}
		if msgs[len(msgs)-1].Body != "Chega amanhã até às 18h!" {
			t.Errorf("expected last message body to match, got %s", msgs[len(msgs)-1].Body)
		}
	})

	// =========================================================================
	// SCENARIO 5: CTWA Referral Extends Window to 72 Hours
	// =========================================================================
	t.Run("Inbound CTWA referral sets 72h service window", func(t *testing.T) {
		ctwaNow := time.Now().UTC()
		ctwaEv := &inbound.InboundEvent{
			WorkspaceID:  ws.ID,
			ConnectionID: connID,
			MessageID:    "wamid-ctwa-referral-102",
			TraceID:      "trace-ctwa-102",
			Channel:      "whatsapp_cloud",
			From:         customerPhone,
			To:           wabaSender,
			Body:         "Vi seu anúncio no Instagram!",
			SenderName:   "Maria",
			OccurredAt:   ctwaNow,
			Metadata: map[string]string{
				"entry_point_type": "ctwa",
			},
		}

		if err := inboundProc.Process(ctx, ctwaEv); err != nil {
			t.Fatalf("inbound processor failed for CTWA: %v", err)
		}

		chatCTWA, err := chatRepo.GetChat(ctx, ws.ID, chat.ID)
		if err != nil {
			t.Fatalf("failed to reload chat: %v", err)
		}

		diff := chatCTWA.ServiceWindowExpiresAt.Sub(ctwaNow)
		if diff < 71*time.Hour || diff > 73*time.Hour {
			t.Errorf("expected ~72h window for CTWA, got %v", diff)
		}
		if !chatCTWA.IsServiceWindowOpen() {
			t.Errorf("expected IsServiceWindowOpen() to be true for CTWA")
		}
	})
}
