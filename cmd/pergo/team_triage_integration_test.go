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
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/presence"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestTeamTriageIntegration(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("skipping: PostgreSQL not available")
	}

	ctx := context.Background()

	// Clean up previous test state
	_, _ = pool.Exec(ctx, "DELETE FROM chat_messages")
	_, _ = pool.Exec(ctx, "DELETE FROM chats")
	_, _ = pool.Exec(ctx, "DELETE FROM contact_identities")
	_, _ = pool.Exec(ctx, "DELETE FROM contacts")
	_, _ = pool.Exec(ctx, "DELETE FROM connections")
	_, _ = pool.Exec(ctx, "DELETE FROM audit_logs")
	_, _ = pool.Exec(ctx, "DELETE FROM workspace_members")
	_, _ = pool.Exec(ctx, "DELETE FROM workspaces")

	wsRepo := repository.NewWorkspaceRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	chatRepo := repository.NewChatRepository(pool)
	auditRepo := repository.NewAuditRepository(pool)

	// 1. Create Workspace
	ws, err := wsRepo.Create(ctx, "Team Triage Integration Workspace")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	// 2. Setup Connection
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
		Name:           "Triage WhatsApp",
		Slug:           "whatsapp-triage",
		Channel:        "whatsapp",
		SenderIdentity: "+551199990000",
		Status:         "active",
		IsDefault:      true,
		Credentials:    []byte(`{"token":"dummy"}`),
	}
	if err := connRepo.Create(ctx, conn); err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}

	// 3. Create Teammates in workspace_members
	operatorAlice := &repository.WorkspaceMember{
		ID:                    uuid.New(),
		WorkspaceID:           ws.ID,
		Name:                  "Alice Operator",
		Email:                 "alice@example.com",
		Role:                  "operator",
		AssignedConnectionIDs: []uuid.UUID{conn.ID},
		CreatedAt:             time.Now().UTC(),
		UpdatedAt:             time.Now().UTC(),
	}
	if err := wsRepo.AddMember(ctx, operatorAlice); err != nil {
		t.Fatalf("failed to add workspace member alice: %v", err)
	}

	operatorBob := &repository.WorkspaceMember{
		ID:                    uuid.New(),
		WorkspaceID:           ws.ID,
		Name:                  "Bob Support",
		Email:                 "bob@example.com",
		Role:                  "agent",
		AssignedConnectionIDs: []uuid.UUID{},
		CreatedAt:             time.Now().UTC(),
		UpdatedAt:             time.Now().UTC(),
	}
	if err := wsRepo.AddMember(ctx, operatorBob); err != nil {
		t.Fatalf("failed to add workspace member bob: %v", err)
	}

	// 4. Create Contact and Chat
	customerPhone := "+5511988887777"
	customerName := "Carlos Customer"
	contact, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp", customerPhone, customerName, "", "")
	if err != nil {
		t.Fatalf("failed to resolve contact: %v", err)
	}

	chat, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &conn.ID, contact.ID)
	if err != nil {
		t.Fatalf("failed to find or create chat: %v", err)
	}

	// Insert audit message so ListConversations finds the thread
	auditPayload, _ := json.Marshal(map[string]any{
		"channel": "whatsapp",
		"from":    customerPhone,
		"body":    "Preciso de atendimento urgente",
	})
	_, _ = pool.Exec(ctx, `
		INSERT INTO audit_logs (id, workspace_id, trace_id, event_type, payload, created_at)
		VALUES (gen_random_uuid(), $1, 'trace-triage-1001', 'inbound_message', $2, NOW())
	`, ws.ID, auditPayload)

	// 5. Setup Presence Tracker & MCP Server
	presenceTracker := presence.NewTracker(15 * time.Second)

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

	// 6. Setup Admin Inbox Handler
	inboxHandler := &admin.InboxHandler{
		Repo:        auditRepo,
		ContactRepo: contactRepo,
		ChatRepo:    chatRepo,
		Connections: connRepo,
		Workspaces:  wsRepo,
		Presence:    presenceTracker,
	}

	// ---------------------------------------------------------
	// Step A: MCP workspace_team tool
	// ---------------------------------------------------------
	t.Run("MCP workspace_team", func(t *testing.T) {
		res, err := mcpServer.CallTool(ctx, "workspace_team", map[string]any{
			"workspace_id": ws.ID.String(),
		})
		if err != nil {
			t.Fatalf("workspace_team tool call error: %v", err)
		}
		if res.IsError {
			t.Fatalf("workspace_team returned tool error: %+v", res.Content)
		}

		text := res.Content[0].(mcp.TextContent).Text
		var parsed struct {
			Teammates []mcppkg.TeammateDTO `json:"teammates"`
			Count     int                  `json:"count"`
		}
		if err := json.Unmarshal([]byte(text), &parsed); err != nil {
			t.Fatalf("failed to unmarshal workspace_team response: %v", err)
		}

		if parsed.Count != 2 {
			t.Fatalf("expected 2 teammates, got %d", parsed.Count)
		}

		foundAlice := false
		for _, tm := range parsed.Teammates {
			if tm.Email == "alice@example.com" {
				foundAlice = true
				if len(tm.AccessibleAccounts) != 1 {
					t.Errorf("expected 1 accessible account for Alice, got %d", len(tm.AccessibleAccounts))
				}
				if tm.AccessibleAccounts[0].SenderIdentity != conn.SenderIdentity {
					t.Errorf("expected sender identity %s, got %s", conn.SenderIdentity, tm.AccessibleAccounts[0].SenderIdentity)
				}
			}
		}
		if !foundAlice {
			t.Errorf("expected alice@example.com in teammates list")
		}
	})

	// ---------------------------------------------------------
	// Step B: MCP chat_assign validation on unknown email
	// ---------------------------------------------------------
	t.Run("MCP chat_assign unknown email rejected", func(t *testing.T) {
		res, err := mcpServer.CallTool(ctx, "chat_assign", map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
			"email":        "unknown@ghost.io",
		})
		if err != nil {
			t.Fatalf("unexpected call error: %v", err)
		}
		if !res.IsError {
			t.Fatalf("expected error when assigning unknown email, got success")
		}

		errText := res.Content[0].(mcp.TextContent).Text
		expectedPhrase := `teammate with email "unknown@ghost.io" not found in workspace`
		if !strings.Contains(errText, expectedPhrase) {
			t.Errorf("expected error to contain %q, got: %s", expectedPhrase, errText)
		}
		if !strings.Contains(errText, "Call workspace_team to list valid teammates.") {
			t.Errorf("expected error to suggest calling workspace_team, got: %s", errText)
		}
	})

	// ---------------------------------------------------------
	// Step C: MCP chat_assign success & DB verification
	// ---------------------------------------------------------
	t.Run("MCP chat_assign success", func(t *testing.T) {
		res, err := mcpServer.CallTool(ctx, "chat_assign", map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
			"email":        "alice@example.com",
		})
		if err != nil {
			t.Fatalf("unexpected call error: %v", err)
		}
		if res.IsError {
			t.Fatalf("chat_assign returned tool error: %+v", res.Content)
		}

		dbChat, err := chatRepo.GetChat(ctx, ws.ID, chat.ID)
		if err != nil {
			t.Fatalf("failed to get chat: %v", err)
		}
		if dbChat.AssignedEmail == nil || *dbChat.AssignedEmail != "alice@example.com" {
			t.Errorf("expected assigned_email to be alice@example.com, got %v", dbChat.AssignedEmail)
		}
		if dbChat.AssignedUserID == nil || *dbChat.AssignedUserID != operatorAlice.ID {
			t.Errorf("expected assigned_user_id to be %s, got %v", operatorAlice.ID, dbChat.AssignedUserID)
		}
	})

	// ---------------------------------------------------------
	// Step D: MCP chat_unassign
	// ---------------------------------------------------------
	t.Run("MCP chat_unassign", func(t *testing.T) {
		res, err := mcpServer.CallTool(ctx, "chat_unassign", map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
		})
		if err != nil {
			t.Fatalf("unexpected call error: %v", err)
		}
		if res.IsError {
			t.Fatalf("chat_unassign returned tool error: %+v", res.Content)
		}

		dbChat, err := chatRepo.GetChat(ctx, ws.ID, chat.ID)
		if err != nil {
			t.Fatalf("failed to get chat: %v", err)
		}
		if dbChat.AssignedEmail != nil {
			t.Errorf("expected assigned_email to be nil, got %v", *dbChat.AssignedEmail)
		}
	})

	// ---------------------------------------------------------
	// Step E: MCP chat_set_label & chat_remove_label
	// ---------------------------------------------------------
	t.Run("MCP chat_set_label & chat_remove_label", func(t *testing.T) {
		// Set label VIP
		res1, err := mcpServer.CallTool(ctx, "chat_set_label", map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
			"label":        "VIP",
		})
		if err != nil || res1.IsError {
			t.Fatalf("failed to set label VIP: err=%v res=%+v", err, res1)
		}

		// Set label VIP again (idempotent)
		res2, err := mcpServer.CallTool(ctx, "chat_set_label", map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
			"label":        "VIP",
		})
		if err != nil || res2.IsError {
			t.Fatalf("failed to set duplicate label VIP: err=%v res=%+v", err, res2)
		}

		// Set label Support
		res3, err := mcpServer.CallTool(ctx, "chat_set_label", map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
			"label":        "Support",
		})
		if err != nil || res3.IsError {
			t.Fatalf("failed to set label Support: err=%v res=%+v", err, res3)
		}

		dbChat, err := chatRepo.GetChat(ctx, ws.ID, chat.ID)
		if err != nil {
			t.Fatalf("failed to get chat: %v", err)
		}
		if len(dbChat.Tags) != 2 {
			t.Fatalf("expected 2 tags (VIP, Support), got %v", dbChat.Tags)
		}

		// Remove label VIP
		res4, err := mcpServer.CallTool(ctx, "chat_remove_label", map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
			"label":        "VIP",
		})
		if err != nil || res4.IsError {
			t.Fatalf("failed to remove label VIP: err=%v res=%+v", err, res4)
		}

		dbChat2, err := chatRepo.GetChat(ctx, ws.ID, chat.ID)
		if err != nil {
			t.Fatalf("failed to get chat: %v", err)
		}
		if len(dbChat2.Tags) != 1 || dbChat2.Tags[0] != "Support" {
			t.Fatalf("expected 1 tag [Support], got %v", dbChat2.Tags)
		}
	})

	// ---------------------------------------------------------
	// Step F: MCP chat_close & chat_open
	// ---------------------------------------------------------
	t.Run("MCP chat_close and chat_open", func(t *testing.T) {
		resClose, err := mcpServer.CallTool(ctx, "chat_close", map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
		})
		if err != nil || resClose.IsError {
			t.Fatalf("failed to close chat: err=%v res=%+v", err, resClose)
		}

		dbChat, err := chatRepo.GetChat(ctx, ws.ID, chat.ID)
		if err != nil {
			t.Fatalf("failed to get chat: %v", err)
		}
		if dbChat.Status != "closed" {
			t.Errorf("expected status closed, got %s", dbChat.Status)
		}

		resOpen, err := mcpServer.CallTool(ctx, "chat_open", map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
		})
		if err != nil || resOpen.IsError {
			t.Fatalf("failed to open chat: err=%v res=%+v", err, resOpen)
		}

		dbChat2, err := chatRepo.GetChat(ctx, ws.ID, chat.ID)
		if err != nil {
			t.Fatalf("failed to get chat: %v", err)
		}
		if dbChat2.Status != "open" {
			t.Errorf("expected status open, got %s", dbChat2.Status)
		}
	})

	// ---------------------------------------------------------
	// Step G: Real-time Presence & Collision Tracking
	// ---------------------------------------------------------
	t.Run("Presence heartbeat and collision detection", func(t *testing.T) {
		e := echo.New()
		reqCtx := tenant.WithWorkspaceID(context.Background(), ws.ID)
		reqCtx = domain.ContextWithWorkspaceID(reqCtx, ws.ID)

		// Operator 1 heartbeat
		f1 := url.Values{}
		f1.Set("contact_id", contact.ID.String())
		f1.Set("user_id", "op-alice")
		f1.Set("name", "Alice")
		f1.Set("email", "alice@example.com")
		f1.Set("status", "viewing")

		req1 := httptest.NewRequest(http.MethodPost, "/admin/inbox/presence", strings.NewReader(f1.Encode()))
		req1.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req1 = req1.WithContext(reqCtx)
		rec1 := httptest.NewRecorder()
		c1 := e.NewContext(req1, rec1)

		if err := inboxHandler.PresenceHeartbeat(c1); err != nil {
			t.Fatalf("PresenceHeartbeat op1 failed: %v", err)
		}
		if rec1.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec1.Code)
		}

		viewers1 := presenceTracker.GetActiveViewers(ws.ID, contact.ID.String())
		if len(viewers1) != 1 {
			t.Fatalf("expected 1 active viewer, got %d", len(viewers1))
		}

		// Operator 2 heartbeat (Collision condition)
		f2 := url.Values{}
		f2.Set("contact_id", contact.ID.String())
		f2.Set("user_id", "op-bob")
		f2.Set("name", "Bob")
		f2.Set("email", "bob@example.com")
		f2.Set("status", "typing")

		req2 := httptest.NewRequest(http.MethodPost, "/admin/inbox/presence", strings.NewReader(f2.Encode()))
		req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req2 = req2.WithContext(reqCtx)
		rec2 := httptest.NewRecorder()
		c2 := e.NewContext(req2, rec2)

		if err := inboxHandler.PresenceHeartbeat(c2); err != nil {
			t.Fatalf("PresenceHeartbeat op2 failed: %v", err)
		}

		viewers2 := presenceTracker.GetActiveViewers(ws.ID, contact.ID.String())
		if len(viewers2) != 2 {
			t.Fatalf("expected 2 active viewers (collision alert trigger), got %d", len(viewers2))
		}
	})

	// ---------------------------------------------------------
	// Step H: Web Console Triage HTTP Endpoints
	// ---------------------------------------------------------
	t.Run("Web Console Triage Endpoints", func(t *testing.T) {
		e := echo.New()
		reqCtx := tenant.WithWorkspaceID(context.Background(), ws.ID)
		reqCtx = domain.ContextWithWorkspaceID(reqCtx, ws.ID)
		reqCtx = i18n.WithLocale(reqCtx, "pt-BR")

		// 1. Assign via Web Console POST /admin/inbox/assign
		assignForm := url.Values{}
		assignForm.Set("email", "alice@example.com")
		assignReq := httptest.NewRequest(http.MethodPost, "/admin/inbox/assign?contact_id="+contact.ID.String(), strings.NewReader(assignForm.Encode()))
		assignReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		assignReq.Header.Set("HX-Request", "true")
		assignReq = assignReq.WithContext(reqCtx)
		assignRec := httptest.NewRecorder()
		assignCtx := e.NewContext(assignReq, assignRec)

		if err := inboxHandler.AssignChat(assignCtx); err != nil {
			t.Fatalf("AssignChat failed: %v", err)
		}
		if assignRec.Code != http.StatusOK {
			t.Fatalf("expected 200 from AssignChat, got %d", assignRec.Code)
		}
		if assignRec.Header().Get("HX-Trigger") != "refreshChats" {
			t.Errorf("expected HX-Trigger header to be refreshChats, got %s", assignRec.Header().Get("HX-Trigger"))
		}
		if !strings.Contains(assignRec.Body.String(), "alice@example.com") {
			t.Errorf("expected response HTML to contain assigned teammate alice@example.com")
		}

		// 2. Add Tag via Web Console POST /admin/inbox/tags/add
		tagForm := url.Values{}
		tagForm.Set("tag", "Comercial")
		tagReq := httptest.NewRequest(http.MethodPost, "/admin/inbox/tags/add?contact_id="+contact.ID.String(), strings.NewReader(tagForm.Encode()))
		tagReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		tagReq.Header.Set("HX-Request", "true")
		tagReq = tagReq.WithContext(reqCtx)
		tagRec := httptest.NewRecorder()
		tagCtx := e.NewContext(tagReq, tagRec)

		if err := inboxHandler.AddChatTag(tagCtx); err != nil {
			t.Fatalf("AddChatTag failed: %v", err)
		}
		if tagRec.Code != http.StatusOK {
			t.Fatalf("expected 200 from AddChatTag, got %d", tagRec.Code)
		}
		if !strings.Contains(tagRec.Body.String(), "Comercial") {
			t.Errorf("expected response HTML to contain added tag Comercial")
		}

		// 3. Close conversation via Web Console POST /admin/inbox/status?contact_id=...&status=closed
		closeReq := httptest.NewRequest(http.MethodPost, "/admin/inbox/status?contact_id="+contact.ID.String()+"&status=closed", nil)
		closeReq.Header.Set("HX-Request", "true")
		closeReq = closeReq.WithContext(reqCtx)
		closeRec := httptest.NewRecorder()
		closeCtx := e.NewContext(closeReq, closeRec)

		if err := inboxHandler.UpdateChatStatus(closeCtx); err != nil {
			t.Fatalf("UpdateChatStatus (closed) failed: %v", err)
		}
		if closeRec.Code != http.StatusOK {
			t.Fatalf("expected 200 from UpdateChatStatus, got %d", closeRec.Code)
		}
		if closeRec.Header().Get("HX-Trigger") != "refreshChats" {
			t.Errorf("expected HX-Trigger to be refreshChats")
		}
		// When closed, UI renders the "Reabrir" button
		if !strings.Contains(closeRec.Body.String(), "Reabrir") {
			t.Errorf("expected HTML to contain Reabrir button for closed chat")
		}

		// 4. Poll conversations with status=closed vs status=open
		// A. Poll status=closed -> conversation should appear
		pollClosedReq := httptest.NewRequest(http.MethodGet, "/admin/inbox/conversations/poll?status=closed", nil)
		pollClosedReq.Header.Set("HX-Request", "true")
		pollClosedReq = pollClosedReq.WithContext(reqCtx)
		pollClosedRec := httptest.NewRecorder()
		pollClosedCtx := e.NewContext(pollClosedReq, pollClosedRec)

		if err := inboxHandler.PollConversations(pollClosedCtx); err != nil {
			t.Fatalf("PollConversations (closed) failed: %v", err)
		}
		if pollClosedRec.Code != http.StatusOK {
			t.Fatalf("expected 200 from PollConversations, got %d", pollClosedRec.Code)
		}
		if !strings.Contains(pollClosedRec.Body.String(), customerName) {
			t.Errorf("expected PollConversations (closed) to include %s", customerName)
		}

		// B. Poll status=open -> conversation should NOT appear (as it is closed)
		pollOpenReq := httptest.NewRequest(http.MethodGet, "/admin/inbox/conversations/poll?status=open", nil)
		pollOpenReq.Header.Set("HX-Request", "true")
		pollOpenReq = pollOpenReq.WithContext(reqCtx)
		pollOpenRec := httptest.NewRecorder()
		pollOpenCtx := e.NewContext(pollOpenReq, pollOpenRec)

		if err := inboxHandler.PollConversations(pollOpenCtx); err != nil {
			t.Fatalf("PollConversations (open) failed: %v", err)
		}
		if strings.Contains(pollOpenRec.Body.String(), customerName) {
			t.Errorf("expected PollConversations (open) to filter out closed conversation %s", customerName)
		}

		// 5. Reopen conversation via Web Console POST /admin/inbox/status?contact_id=...&status=open
		openReq := httptest.NewRequest(http.MethodPost, "/admin/inbox/status?contact_id="+contact.ID.String()+"&status=open", nil)
		openReq.Header.Set("HX-Request", "true")
		openReq = openReq.WithContext(reqCtx)
		openRec := httptest.NewRecorder()
		openCtx := e.NewContext(openReq, openRec)

		if err := inboxHandler.UpdateChatStatus(openCtx); err != nil {
			t.Fatalf("UpdateChatStatus (open) failed: %v", err)
		}
		if openRec.Code != http.StatusOK {
			t.Fatalf("expected 200 from UpdateChatStatus, got %d", openRec.Code)
		}
		// When reopened, UI renders the "Resolver" button
		if !strings.Contains(openRec.Body.String(), "Resolver") {
			t.Errorf("expected HTML to contain Resolver button for open chat")
		}
	})
}
