package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/repository"
)

type mockOutboundIngestor struct {
	lastReq     *domain.CreateMessageRequest
	lastTraceID string
	lastWsID    uuid.UUID
}

func (m *mockOutboundIngestor) Ingest(ctx context.Context, workspaceID uuid.UUID, traceID string, req *domain.CreateMessageRequest) (*domain.QueueMessage, error) {
	m.lastWsID = workspaceID
	m.lastTraceID = traceID
	m.lastReq = req
	return &domain.QueueMessage{
		WorkspaceID:  workspaceID,
		TraceID:      traceID,
		To:           req.To,
		Channel:      req.Channel,
		Body:         req.Body,
		TemplateName: req.TemplateName,
		QueuedAt:     time.Now().UTC(),
	}, nil
}

func TestMCPWABATools(t *testing.T) {
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
	tmplRepo := repository.NewWABATemplateRepository(pool)

	kek := make([]byte, 32)
	copy(kek, []byte("dev-development-key-32-bytes-kek"))
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	connRepo := repository.NewConnectionRepository(pool, enc)

	ws, err := wsRepo.Create(ctx, "MCP WABA Test Workspace")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	// 1. Create a WABA connection with phone_number_id & waba_account_id
	wabaConn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "Official WABA Gateway",
		Slug:           "official-waba",
		Channel:        "whatsapp_cloud",
		SenderIdentity: "+15551234567",
		Status:         "active",
		Credentials:    []byte(`{"phone_number_id":"1029384756","waba_account_id":"9876543210","token":"meta_secret_token"}`),
		IsDefault:      true,
	}
	if err := connRepo.Create(ctx, wabaConn); err != nil {
		t.Fatalf("failed to create WABA connection: %v", err)
	}
	wabaConnID := wabaConn.ID

	// 2. Create a non-WABA connection (WhatsApp Web)
	webConn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "WhatsMeow Web",
		Slug:           "whatsmeow-web",
		Channel:        "whatsapp",
		SenderIdentity: "+15559876543",
		Status:         "active",
		Credentials:    []byte(`{}`),
		IsDefault:      false,
	}
	if err := connRepo.Create(ctx, webConn); err != nil {
		t.Fatalf("failed to create Web connection: %v", err)
	}
	_ = webConn.ID

	// 3. Create contacts
	contact1, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp_cloud", "+5511999991111", "Alice Active", "", "+5511999991111")
	if err != nil {
		t.Fatalf("failed to create contact1: %v", err)
	}
	contact2, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp_cloud", "+5511999992222", "Bob Expired", "", "+5511999992222")
	if err != nil {
		t.Fatalf("failed to create contact2: %v", err)
	}

	// 4. Create chats: one with active service window (+20h), one expired (-2h)
	chatActive, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &wabaConnID, contact1.ID)
	if err != nil {
		t.Fatalf("failed to create chatActive: %v", err)
	}
	activeExp := time.Now().UTC().Add(20 * time.Hour)
	_ = chatRepo.UpdateServiceWindow(ctx, ws.ID, chatActive.ID, &activeExp)

	chatExpired, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &wabaConnID, contact2.ID)
	if err != nil {
		t.Fatalf("failed to create chatExpired: %v", err)
	}
	expiredExp := time.Now().UTC().Add(-2 * time.Hour)
	_ = chatRepo.UpdateServiceWindow(ctx, ws.ID, chatExpired.ID, &expiredExp)

	// 5. Create approved WABA template with variable placeholders
	tmplComponents := `[
		{
			"type": "HEADER",
			"format": "TEXT",
			"text": "Order Update for {{1}}"
		},
		{
			"type": "BODY",
			"text": "Hello {{1}}, your order {{2}} has been confirmed!"
		}
	]`
	tmpl, err := tmplRepo.Create(ctx, &repository.WABATemplate{
		WorkspaceID:    ws.ID,
		ConnectionID:   wabaConnID,
		MetaTemplateID: "meta-tmpl-001",
		Name:           "order_status_update",
		Language:       "pt_BR",
		Status:         "APPROVED",
		Category:       "UTILITY",
		Components:     json.RawMessage(tmplComponents),
	})
	if err != nil {
		t.Fatalf("failed to create template: %v", err)
	}

	mockIng := &mockOutboundIngestor{}

	srv := NewServer(
		wsRepo,
		connRepo,
		contactRepo,
		nil,
		mockIng,
		nil,
		nil,
		nil,
		nil,
		[]byte("secret"),
		"http://localhost:8080",
		WithChatRepo(chatRepo),
		WithWABATemplateRepo(tmplRepo),
	)

	// =========================================================================
	// Tool 1: waba_accounts
	// =========================================================================
	t.Run("waba_accounts returns only official Meta connections", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
		}

		res, err := srv.handleWABAAccounts(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsError {
			t.Fatalf("tool returned error: %+v", res.Content)
		}

		text := res.Content[0].(mcp.TextContent).Text
		var parsed struct {
			Accounts []WABAAccountDTO `json:"accounts"`
			Count    int              `json:"count"`
		}
		if err := json.Unmarshal([]byte(text), &parsed); err != nil {
			t.Fatalf("failed to unmarshal output: %v", err)
		}

		if parsed.Count != 1 {
			t.Fatalf("expected count 1, got %d", parsed.Count)
		}
		acc := parsed.Accounts[0]
		if acc.Channel != "whatsapp_cloud" {
			t.Errorf("expected channel whatsapp_cloud, got %s", acc.Channel)
		}
		if acc.PhoneNumberID != "1029384756" {
			t.Errorf("expected phone_number_id 1029384756, got %s", acc.PhoneNumberID)
		}
		if acc.WABAAccountID != "9876543210" {
			t.Errorf("expected waba_account_id 9876543210, got %s", acc.WABAAccountID)
		}
		if acc.SenderIdentity != "+15551234567" {
			t.Errorf("expected sender identity +15551234567, got %s", acc.SenderIdentity)
		}
	})

	// =========================================================================
	// Tool 2: waba_list_chats
	// =========================================================================
	t.Run("waba_list_chats returns WABA chats with service_window_is_open flag", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
		}

		res, err := srv.handleWABAListChats(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsError {
			t.Fatalf("tool returned error: %+v", res.Content)
		}

		text := res.Content[0].(mcp.TextContent).Text
		var parsed struct {
			Chats []WABAChatSummaryDTO `json:"chats"`
			Count int                  `json:"count"`
		}
		if err := json.Unmarshal([]byte(text), &parsed); err != nil {
			t.Fatalf("failed to unmarshal output: %v", err)
		}

		if parsed.Count != 2 {
			t.Fatalf("expected 2 WABA chats, got %d", parsed.Count)
		}

		var activeFound, expiredFound bool
		for _, c := range parsed.Chats {
			if c.ID == chatActive.ID {
				activeFound = true
				if !c.ServiceWindowIsOpen {
					t.Errorf("expected chatActive to have service_window_is_open = true")
				}
				if !strings.Contains(c.ServiceWindowStatusBadge, "remaining") {
					t.Errorf("expected active badge, got %s", c.ServiceWindowStatusBadge)
				}
			}
			if c.ID == chatExpired.ID {
				expiredFound = true
				if c.ServiceWindowIsOpen {
					t.Errorf("expected chatExpired to have service_window_is_open = false")
				}
				if !strings.Contains(c.ServiceWindowStatusBadge, "Expired") {
					t.Errorf("expected expired badge, got %s", c.ServiceWindowStatusBadge)
				}
			}
		}

		if !activeFound || !expiredFound {
			t.Errorf("expected to find both active and expired chats")
		}
	})

	// =========================================================================
	// Tool 3: waba_templates
	// =========================================================================
	t.Run("waba_templates returns approved templates with expected variables", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
		}

		res, err := srv.handleWABATemplates(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsError {
			t.Fatalf("tool returned error: %+v", res.Content)
		}

		text := res.Content[0].(mcp.TextContent).Text
		var parsed struct {
			Templates []WABATemplateSummaryDTO `json:"templates"`
			Count     int                      `json:"count"`
		}
		if err := json.Unmarshal([]byte(text), &parsed); err != nil {
			t.Fatalf("failed to unmarshal output: %v", err)
		}

		if parsed.Count != 1 {
			t.Fatalf("expected 1 template, got %d", parsed.Count)
		}
		tDTO := parsed.Templates[0]
		if tDTO.Name != "order_status_update" {
			t.Errorf("expected template name order_status_update, got %s", tDTO.Name)
		}
		if tDTO.Status != "APPROVED" {
			t.Errorf("expected APPROVED, got %s", tDTO.Status)
		}
		if tDTO.ExpectedVariables["body"] != 2 {
			t.Errorf("expected 2 body variables, got %d", tDTO.ExpectedVariables["body"])
		}
		if tDTO.ExpectedVariables["header"] != 1 {
			t.Errorf("expected 1 header variable, got %d", tDTO.ExpectedVariables["header"])
		}
	})

	// =========================================================================
	// Tool 4: waba_template_details
	// =========================================================================
	t.Run("waba_template_details retrieves details by ID and by name/language", func(t *testing.T) {
		// By ID
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"template_id":  tmpl.ID.String(),
		}

		res, err := srv.handleWABATemplateDetails(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsError {
			t.Fatalf("tool returned error: %+v", res.Content)
		}

		var parsed struct {
			Template          repository.WABATemplate `json:"template"`
			ExpectedVariables map[string]int          `json:"expected_variables"`
		}
		if err := json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &parsed); err != nil {
			t.Fatalf("failed to unmarshal details: %v", err)
		}
		if parsed.Template.Name != "order_status_update" {
			t.Errorf("expected order_status_update, got %s", parsed.Template.Name)
		}

		// By Connection + Name + Language
		req2 := mcp.CallToolRequest{}
		req2.Params.Arguments = map[string]any{
			"workspace_id":  ws.ID.String(),
			"connection_id": wabaConnID.String(),
			"name":          "order_status_update",
			"language":      "pt_BR",
		}
		res2, err := srv.handleWABATemplateDetails(ctx, req2)
		if err != nil || res2.IsError {
			t.Fatalf("failed to get template by name: %v, %+v", err, res2)
		}
	})

	// =========================================================================
	// Tool 5: waba_chat_send_message
	// =========================================================================
	t.Run("waba_chat_send_message rejects expired chat with compliance guidance", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chatExpired.ID.String(),
			"message":      "Hello after 24 hours!",
		}

		res, err := srv.handleWABAChatSendMessage(ctx, req)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		if !res.IsError {
			t.Fatalf("expected tool error for expired chat, got success: %+v", res.Content)
		}

		errMsg := res.Content[0].(mcp.TextContent).Text
		if !strings.Contains(errMsg, "24-hour customer service window is closed") {
			t.Errorf("expected window closed warning, got %q", errMsg)
		}
		if !strings.Contains(errMsg, "waba_chat_send_template") {
			t.Errorf("expected guidance to use waba_chat_send_template, got %q", errMsg)
		}
	})

	t.Run("waba_chat_send_message succeeds when service window is open", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chatActive.ID.String(),
			"message":      "Valid message inside window!",
			"sender_name":  "Support Bot",
		}

		res, err := srv.handleWABAChatSendMessage(ctx, req)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		if res.IsError {
			t.Fatalf("unexpected tool error: %+v", res.Content)
		}

		var parsed map[string]interface{}
		_ = json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &parsed)
		if parsed["success"] != true {
			t.Errorf("expected success true, got %v", parsed["success"])
		}

		if mockIng.lastReq == nil || mockIng.lastReq.Body != "Valid message inside window!" {
			t.Errorf("expected message dispatched to ingestor, got %+v", mockIng.lastReq)
		}
	})

	// =========================================================================
	// Tool 6: waba_chat_send_template
	// =========================================================================
	t.Run("waba_chat_send_template succeeds even when service window is closed", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id":  ws.ID.String(),
			"chat_id":       chatExpired.ID.String(),
			"template_name": "order_status_update",
			"language":      "pt_BR",
			"parameters": map[string]any{
				"header": []any{"Bob"},
				"body":   []any{"Bob", "ORD-999"},
			},
			"sender_name": "Outbound Engine",
		}

		res, err := srv.handleWABAChatSendTemplate(ctx, req)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		if res.IsError {
			t.Fatalf("unexpected tool error: %+v", res.Content)
		}

		var parsed map[string]interface{}
		_ = json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &parsed)
		if parsed["success"] != true {
			t.Errorf("expected success true, got %v", parsed["success"])
		}

		if mockIng.lastReq == nil || mockIng.lastReq.TemplateName != "order_status_update" {
			t.Errorf("expected template dispatched to ingestor, got %+v", mockIng.lastReq)
		}
		if mockIng.lastReq.Type != "template" {
			t.Errorf("expected 'template', got %s", mockIng.lastReq.Type)
		}
		if len(mockIng.lastReq.Components) != 2 {
			t.Errorf("expected 2 components (header & body), got %d", len(mockIng.lastReq.Components))
		}
	})
}
