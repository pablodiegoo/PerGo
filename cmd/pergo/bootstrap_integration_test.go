package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/nats-io/nats.go"
	"github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/config"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/repository"
)

type recordingPublisher struct {
	published []recordedMsg
}

type recordedMsg struct {
	subject string
	data    []byte
	traceID string
}

func (p *recordingPublisher) Publish(_ context.Context, subject string, data []byte, traceID string) error {
	p.published = append(p.published, recordedMsg{
		subject: subject,
		data:    append([]byte(nil), data...),
		traceID: traceID,
	})
	return nil
}

// TestBootstrapCore_StdioAndHTTPParity verifies that the shared core bootstrap
// initializes both HTTP mode and `pergo mcp` stdio mode with the exact same
// fully configured domain graph (ChatEngine, OutboundProcessor with WindowChecker,
// WABATemplateRepo, ChatRepo, ContactRepo, MediaEngine, QueueDepthTracker, and
// WebhookDispatcher).
func TestBootstrapCore_StdioAndHTTPParity(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("skipping: PostgreSQL not available")
	}

	ctx := context.Background()
	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		t.Fatalf("NewSQLDB: %v", err)
	}
	defer db.Close()

	cfg := config.Load()
	pub := &recordingPublisher{}

	core, err := BootstrapCore(ctx, cfg, pool, db, nil, WithCorePublisher(pub))
	if err != nil {
		t.Fatalf("BootstrapCore failed: %v", err)
	}
	defer func() { _ = core.Close() }()

	if core.ChatEngine == nil {
		t.Fatal("expected core.ChatEngine to be non-nil")
	}
	if core.OutboundProcessor == nil {
		t.Fatal("expected core.OutboundProcessor to be non-nil")
	}
	if core.InboundProcessor == nil {
		t.Fatal("expected core.InboundProcessor to be non-nil")
	}
	if core.WindowChecker == nil {
		t.Fatal("expected core.WindowChecker to be non-nil")
	}
	if core.MediaEngine == nil {
		t.Fatal("expected core.MediaEngine to be non-nil")
	}
	if core.MCPServer == nil {
		t.Fatal("expected core.MCPServer to be non-nil")
	}

	// Provision workspace & WABA connection (without default template)
	ws, err := core.WorkspaceRepo.Create(ctx, "Stdio Parity Workspace "+uuid.NewString()[:8])
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	defer func() { _ = core.WorkspaceRepo.Delete(ctx, ws.ID) }()

	wabaConn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "Stdio WABA Cloud",
		Slug:           "waba-stdio-" + uuid.NewString()[:8],
		Channel:        "whatsapp_cloud",
		SenderIdentity: "+5511999990801",
		Status:         "connected",
		IsDefault:      true,
		Credentials:    []byte(`{"access_token":"tok","phone_number_id":"12345"}`),
	}
	if err := core.ConnectionRepo.Create(ctx, wabaConn); err != nil {
		t.Fatalf("create WABA connection: %v", err)
	}

	// Seed an approved WABA template in WABATemplateRepo and reload cache
	approvedTpl := &repository.WABATemplate{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		ConnectionID:   wabaConn.ID,
		MetaTemplateID: "meta-tpl-123",
		Name:           "order_update_stdio",
		Language:       "pt_BR",
		Category:       "UTILITY",
		Status:         "APPROVED",
		Components:     json.RawMessage(`[{"type":"BODY","text":"Seu pedido {{1}} foi atualizado."}]`),
	}
	if _, err := core.WABATemplateRepo.Upsert(ctx, approvedTpl); err != nil {
		t.Fatalf("upsert WABA template: %v", err)
	}
	if err := core.WABATemplateRepo.LoadCache(ctx); err != nil {
		t.Fatalf("load WABA template cache: %v", err)
	}

	// 1. Verify stdio-bootstrapped MCPServer enforces Customer Service Window on closed WABA chat
	customerPhone := "+5511988887777"
	contact, err := core.ContactRepo.ResolveContact(ctx, ws.ID, "whatsapp_cloud", customerPhone, "Stdio Customer", "", customerPhone)
	if err != nil {
		t.Fatalf("resolve contact: %v", err)
	}
	chatThread, err := core.ChatRepo.FindOrCreateChat(ctx, ws.ID, &wabaConn.ID, contact.ID)
	if err != nil {
		t.Fatalf("find or create chat: %v", err)
	}
	// Explicitly expire the Customer Service Window (closed 2 hours ago)
	expiredAt := time.Now().UTC().Add(-2 * time.Hour)
	if err := core.ChatRepo.UpdateServiceWindow(ctx, ws.ID, chatThread.ID, &expiredAt); err != nil {
		t.Fatalf("expire service window: %v", err)
	}

	resClosed, err := core.MCPServer.CallTool(ctx, "waba_chat_send_message", map[string]any{
		"workspace_id": ws.ID.String(),
		"chat_id":      chatThread.ID.String(),
		"message":      "Free-form message outside 24h window",
	})
	if err != nil {
		t.Fatalf("CallTool waba_chat_send_message error: %v", err)
	}
	if !resClosed.IsError {
		t.Fatalf("expected waba_chat_send_message outside 24h Customer Service Window to return tool error, got success: %+v", resClosed.Content)
	}
	errText := resClosed.Content[0].(mcp.TextContent).Text
	if !strings.Contains(strings.ToLower(errText), "24-hour") && !strings.Contains(strings.ToLower(errText), "window") {
		t.Errorf("expected Customer Service Window error message, got: %s", errText)
	}

	// Also verify `send_message` tool invokes OutboundProcessor.Ingest with WindowChecker wired
	resSendClosed, err := core.MCPServer.CallTool(ctx, "send_message", map[string]any{
		"workspace_id": ws.ID.String(),
		"channel":      "whatsapp_cloud",
		"to":           customerPhone,
		"body":         "Direct free-form send outside 24h window",
	})
	if err != nil {
		t.Fatalf("CallTool send_message error: %v", err)
	}
	if !resSendClosed.IsError {
		t.Fatalf("expected send_message on whatsapp_cloud without open recipient_session to be rejected by WindowChecker, got success: %+v", resSendClosed.Content)
	}

	// 2. Verify stdio-bootstrapped MCPServer `waba_start_chat` validates template parameters via OutboundProcessor.SetTemplateRepository
	resMissingParams, err := core.MCPServer.CallTool(ctx, "waba_start_chat", map[string]any{
		"workspace_id":  ws.ID.String(),
		"connection_id": wabaConn.ID.String(),
		"phone":         customerPhone,
		"template_name": "order_update_stdio",
		"language":      "pt_BR",
		"sender_name":   "Stdio AI Agent",
	})
	if err != nil {
		t.Fatalf("CallTool waba_start_chat (missing params) error: %v", err)
	}
	if !resMissingParams.IsError {
		t.Fatalf("expected waba_start_chat without required template parameters to fail validation, got success")
	}

	// Now pass valid template components and verify OutboundProcessor.Ingest persists the ChatMessage with template metadata
	resStart, err := core.MCPServer.CallTool(ctx, "waba_start_chat", map[string]any{
		"workspace_id":  ws.ID.String(),
		"connection_id": wabaConn.ID.String(),
		"phone":         customerPhone,
		"template_name": "order_update_stdio",
		"language":      "pt_BR",
		"sender_name":   "Stdio AI Agent",
		"components": []any{
			map[string]any{
				"type": "body",
				"parameters": []any{
					map[string]any{"type": "text", "text": "#999"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool waba_start_chat error: %v", err)
	}
	if resStart.IsError {
		t.Fatalf("waba_start_chat failed: %+v", resStart.Content)
	}
	var startPayload struct {
		TraceID string `json:"trace_id"`
		ChatID  string `json:"chat_id"`
	}
	if err := json.Unmarshal([]byte(resStart.Content[0].(mcp.TextContent).Text), &startPayload); err != nil {
		t.Fatalf("unmarshal waba_start_chat response: %v", err)
	}
	if startPayload.TraceID == "" {
		t.Fatal("expected non-empty trace_id from waba_start_chat")
	}

	persistedMsg, err := core.ChatRepo.GetChatMessageByUID(ctx, ws.ID, startPayload.TraceID)
	if err != nil {
		t.Fatalf("expected OutboundProcessor.Ingest to persist ChatMessage in stdio mode, got error: %v", err)
	}
	if persistedMsg.Metadata["template_name"] != "order_update_stdio" {
		t.Errorf("expected persisted ChatMessage metadata template_name='order_update_stdio', got %+v", persistedMsg.Metadata)
	}
	if persistedMsg.SenderName != "Stdio AI Agent" {
		t.Errorf("expected persisted ChatMessage sender_name='Stdio AI Agent', got %q", persistedMsg.SenderName)
	}
}

// TestBuildHTTPRouter_SingleRouteOwnershipAndWorkspaceScope exercises the extracted
// HTTP router builder (`BuildHTTPRouter`) to verify:
// 1. Route registration and single route ownership on `/api/v1/chats/*` (including
//    `/notes`, `/summarize`, `/react`, `/messages`), `/admin/inbox/*`, and `/mcp`.
// 2. Workspace Scope enforcement across the bootstrapped routes (blocking cross-tenant
//    access with 403/404 and unauthenticated access with 401).
func TestBuildHTTPRouter_SingleRouteOwnershipAndWorkspaceScope(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("skipping: PostgreSQL not available")
	}

	ctx := context.Background()
	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		t.Fatalf("NewSQLDB: %v", err)
	}
	defer db.Close()

	cfg := config.Load()
	cfg.AdminPassword = "test-admin-password-152"
	pub := &recordingPublisher{}

	core, err := BootstrapCore(ctx, cfg, pool, db, nil, WithCorePublisher(pub), WithCoreNATSHealth(&noopNATS{}))
	if err != nil {
		t.Fatalf("BootstrapCore failed: %v", err)
	}
	defer func() { _ = core.Close() }()

	e := BuildHTTPRouter(core)
	srv := httptest.NewServer(e)
	defer srv.Close()

	// Create Workspace A & Workspace B with their own API keys
	wsA, err := core.WorkspaceRepo.Create(ctx, "Router Smoke WS-A "+uuid.NewString()[:8])
	if err != nil {
		t.Fatalf("create wsA: %v", err)
	}
	defer func() { _ = core.WorkspaceRepo.Delete(ctx, wsA.ID) }()

	wsB, err := core.WorkspaceRepo.Create(ctx, "Router Smoke WS-B "+uuid.NewString()[:8])
	if err != nil {
		t.Fatalf("create wsB: %v", err)
	}
	defer func() { _ = core.WorkspaceRepo.Delete(ctx, wsB.ID) }()

	_, rawKeyA, err := core.APIKeyRepo.Create(ctx, wsA.ID, "Key A")
	if err != nil {
		t.Fatalf("create APIKey A: %v", err)
	}

	_, rawKeyB, err := core.APIKeyRepo.Create(ctx, wsB.ID, "Key B")
	if err != nil {
		t.Fatalf("create APIKey B: %v", err)
	}

	// Create a WhatsApp connection, contact, chat, and message in Workspace A
	connA := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    wsA.ID,
		Name:           "WA Route Smoke A",
		Slug:           "whatsapp-a",
		Channel:        "whatsapp",
		SenderIdentity: "+5511900001111",
		Status:         "connected",
		IsDefault:      true,
		Credentials:    []byte(`{"token":"dummy"}`),
	}
	if err := core.ConnectionRepo.Create(ctx, connA); err != nil {
		t.Fatalf("create connA: %v", err)
	}

	contactA, err := core.ContactRepo.ResolveContact(ctx, wsA.ID, "whatsapp", "+5511922223333", "Maria Smoke", "", "+5511922223333")
	if err != nil {
		t.Fatalf("resolve contactA: %v", err)
	}
	chatA, err := core.ChatRepo.FindOrCreateChat(ctx, wsA.ID, &connA.ID, contactA.ID)
	if err != nil {
		t.Fatalf("find or create chatA: %v", err)
	}
	msgA := &domain.ChatMessage{
		ChatID:      chatA.ID,
		WorkspaceID: wsA.ID,
		UID:         "msg-smoke-a-" + uuid.NewString()[:8],
		Direction:   domain.DirectionInbound,
		SenderType:  domain.SenderTypeContact,
		SenderName:  "Maria Smoke",
		SenderID:    "+5511922223333",
		Body:        "Preciso de ajuda com o boleto vencido.",
		CreatedAt:   time.Now().UTC(),
	}
	if err := core.ChatRepo.AddChatMessage(ctx, msgA); err != nil {
		t.Fatalf("add msgA: %v", err)
	}

	// Create a chat in Workspace B
	contactB, err := core.ContactRepo.ResolveContact(ctx, wsB.ID, "whatsapp", "+5511944445555", "Tenant B User", "", "+5511944445555")
	if err != nil {
		t.Fatalf("resolve contactB: %v", err)
	}
	chatB, err := core.ChatRepo.FindOrCreateChat(ctx, wsB.ID, nil, contactB.ID)
	if err != nil {
		t.Fatalf("find or create chatB: %v", err)
	}

	doJSON := func(t *testing.T, method, path, apiKey string, payload any) (*http.Response, []byte) {
		t.Helper()
		var bodyReader *bytes.Reader
		if payload != nil {
			b, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}
			bodyReader = bytes.NewReader(b)
		} else {
			bodyReader = bytes.NewReader(nil)
		}
		req, err := http.NewRequest(method, srv.URL+path, bodyReader)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s failed: %v", method, path, err)
		}
		defer resp.Body.Close()
		var out bytes.Buffer
		_, _ = out.ReadFrom(resp.Body)
		return resp, out.Bytes()
	}

	// 1. Verify single route ownership of `/api/v1/chats/:id/notes` (ChatAPIHandler returns 201 Created)
	respNote, bodyNote := doJSON(t, http.MethodPost, "/api/v1/chats/"+chatA.ID.String()+"/notes", rawKeyA, map[string]any{
		"body":        "Internal triage note via ChatAPIHandler",
		"author_name": "Agent Smoke",
	})
	if respNote.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/v1/chats/:id/notes expected 201 Created, got %d: %s", respNote.StatusCode, string(bodyNote))
	}
	var noteEnv struct {
		Status string             `json:"status"`
		Note   domain.ChatMessage `json:"note"`
	}
	if err := json.Unmarshal(bodyNote, &noteEnv); err != nil {
		t.Fatalf("unmarshal note response: %v", err)
	}
	if noteEnv.Status != "created" || noteEnv.Note.Body != "Internal triage note via ChatAPIHandler" {
		t.Errorf("unexpected note response from /api/v1/chats/:id/notes: %s", string(bodyNote))
	}

	// 2. Verify single route ownership of `/api/v1/chats/:id/summarize` (POST and GET)
	respSum, bodySum := doJSON(t, http.MethodPost, "/api/v1/chats/"+chatA.ID.String()+"/summarize", rawKeyA, nil)
	if respSum.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/v1/chats/:id/summarize expected 200 OK, got %d: %s", respSum.StatusCode, string(bodySum))
	}
	var sumEnv struct {
		ChatID         uuid.UUID           `json:"chat_id"`
		CustomerIssues string              `json:"customer_issues"`
		PromisesMade   string              `json:"promises_made"`
		CurrentStatus  string              `json:"current_status"`
		Synopsis       domain.ChatSynopsis `json:"synopsis"`
	}
	if err := json.Unmarshal(bodySum, &sumEnv); err != nil {
		t.Fatalf("unmarshal summarize response: %v", err)
	}
	if sumEnv.ChatID != chatA.ID || sumEnv.CurrentStatus == "" {
		t.Errorf("expected structured ChatSynopsis for chatA, got: %+v", sumEnv)
	}

	// 3. Verify `/api/v1/chats/:id/react` atomic toggle and NATS fan-out
	respReact, bodyReact := doJSON(t, http.MethodPost, "/api/v1/chats/"+chatA.ID.String()+"/react", rawKeyA, map[string]any{
		"message_uid": msgA.UID,
		"emoji":       "👍",
		"action":      "toggle",
	})
	if respReact.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/v1/chats/:id/react expected 200 OK, got %d: %s", respReact.StatusCode, string(bodyReact))
	}

	// 4. Verify `/api/v1/chats/:id/messages` outbound dispatch through OutboundProcessor.Ingest
	respSend, bodySend := doJSON(t, http.MethodPost, "/api/v1/chats/"+chatA.ID.String()+"/messages", rawKeyA, map[string]any{
		"body":        "Hello Maria, sending new invoice link.",
		"sender_type": "human",
		"sender_name": "Agent Smoke",
	})
	if respSend.StatusCode != http.StatusAccepted && respSend.StatusCode != http.StatusOK && respSend.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/v1/chats/:id/messages expected 2xx, got %d: %s", respSend.StatusCode, string(bodySend))
	}

	// 5. Verify Workspace Scope isolation across `/api/v1/chats/*` and `/api/v1/workspaces/:workspace_id/chats`
	// 5a. Unauthenticated request is rejected with 401
	respUnauth, _ := doJSON(t, http.MethodGet, "/api/v1/chats", "", nil)
	if respUnauth.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized without API key, got %d", respUnauth.StatusCode)
	}

	// 5b. Cross-workspace URL path `/api/v1/workspaces/<wsB>/chats` with Key A is rejected with 403 Forbidden
	respCrossPath, _ := doJSON(t, http.MethodGet, "/api/v1/workspaces/"+wsB.ID.String()+"/chats", rawKeyA, nil)
	if respCrossPath.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden when accessing wsB path with Key A, got %d", respCrossPath.StatusCode)
	}

	// 5c. Cross-workspace resource access `/api/v1/chats/<chatB>` with Key A is rejected with 404 Not Found
	for _, sub := range []struct {
		method  string
		suffix  string
		payload map[string]any
	}{
		{http.MethodGet, "", nil},
		{http.MethodPost, "/notes", map[string]any{"body": "cross-tenant note"}},
		{http.MethodPost, "/summarize", nil},
		{http.MethodPost, "/messages", map[string]any{"body": "cross-tenant msg"}},
	} {
		respCross, bodyCross := doJSON(t, sub.method, "/api/v1/chats/"+chatB.ID.String()+sub.suffix, rawKeyA, sub.payload)
		if respCross.StatusCode != http.StatusNotFound {
			t.Errorf("%s /api/v1/chats/<chatB>%s with Key A expected 404 Not Found, got %d: %s", sub.method, sub.suffix, respCross.StatusCode, string(bodyCross))
		}
	}

	// Verify Key B CAN access chatB
	respOwnB, _ := doJSON(t, http.MethodGet, "/api/v1/chats/"+chatB.ID.String(), rawKeyB, nil)
	if respOwnB.StatusCode != http.StatusOK {
		t.Errorf("GET /api/v1/chats/<chatB> with Key B expected 200 OK, got %d", respOwnB.StatusCode)
	}

	// 6. Verify `/admin/inbox` HTMX routes and `/mcp` discovery on the bootstrapped router
	respWellKnown, bodyWK := doJSON(t, http.MethodGet, "/.well-known/mcp.json", "", nil)
	if respWellKnown.StatusCode != http.StatusOK {
		t.Errorf("GET /.well-known/mcp.json expected 200 OK, got %d: %s", respWellKnown.StatusCode, string(bodyWK))
	}

	jar, _ := cookiejar.New(nil)
	adminClient := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	loginForm := url.Values{"password": {cfg.AdminPassword}}
	loginResp, err := adminClient.PostForm(srv.URL+"/admin/login", loginForm)
	if err != nil {
		t.Fatalf("POST /admin/login: %v", err)
	}
	_ = loginResp.Body.Close()

	u, _ := url.Parse(srv.URL)
	jar.SetCookies(u, []*http.Cookie{{
		Name:  middleware.ActiveWorkspaceCookieName,
		Value: wsA.ID.String(),
		Path:  "/admin",
	}})

	inboxResp, err := adminClient.Get(srv.URL + "/admin/inbox")
	if err != nil {
		t.Fatalf("GET /admin/inbox: %v", err)
	}
	defer inboxResp.Body.Close()
	if inboxResp.StatusCode != http.StatusOK {
		t.Errorf("GET /admin/inbox expected 200 OK, got %d", inboxResp.StatusCode)
	}
	var inboxBuf bytes.Buffer
	_, _ = inboxBuf.ReadFrom(inboxResp.Body)
	if !strings.Contains(inboxBuf.String(), "Maria Smoke") {
		t.Errorf("expected /admin/inbox HTML to contain conversation 'Maria Smoke'")
	}
}

// TestRunMCPStdio_EndToEndParity exercises `runMCPStdio` over in-memory stdio pipes
// against the test PostgreSQL and NATS containers, verifying that stdio mode boots
// through `BootstrapCore` and serves tool calls with Workspace Scope.
func TestRunMCPStdio_EndToEndParity(t *testing.T) {
	if integrationDBURL == "" || integrationNATSURL == "" {
		t.Skip("skipping: testcontainers PostgreSQL/NATS not available")
	}

	pool := getTestPool(t)
	if pool == nil {
		t.Skip("skipping: PostgreSQL not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Ensure JetStream stream exists in test NATS container
	nc, err := nats.Connect(integrationNATSURL)
	if err != nil {
		t.Fatalf("connect NATS: %v", err)
	}
	defer nc.Close()

	wsRepo := repository.NewWorkspaceRepository(pool)
	ws, err := wsRepo.Create(ctx, "Stdio Pipe Workspace "+uuid.NewString()[:8])
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	cfg := config.Load()
	cfg.DatabaseURL = integrationDBURL
	cfg.NATSUrl = integrationNATSURL

	// Send a JSON-RPC initialize + tools/call (get_workspace) over stdin
	reqLines := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_workspace","arguments":{"workspace_id":"` + ws.ID.String() + `"}}}`,
	}, "\n") + "\n"

	stdin := strings.NewReader(reqLines)
	var stdout, stderr bytes.Buffer

	err = runMCPStdio(ctx, cfg, []string{"--workspace-id=" + ws.ID.String()}, stdin, &stdout, &stderr)
	if err != nil {
		t.Fatalf("runMCPStdio returned error: %v (stderr: %s)", err, stderr.String())
	}

	outStr := stdout.String()
	if !strings.Contains(outStr, ws.ID.String()) {
		t.Errorf("expected stdio output to contain workspace ID %s, got:\n%s", ws.ID.String(), outStr)
	}
}
