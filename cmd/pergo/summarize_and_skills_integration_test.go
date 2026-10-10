package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	mcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/pablojhp.pergo/internal/api/handler/admin"
	mcppkg "github.com/pablojhp.pergo/internal/api/mcp"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/presence"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/components"
)

func TestSummarizeAndSkillsIntegration(t *testing.T) {
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

	kek := make([]byte, 32)
	copy(kek, []byte("dev-development-key-32-bytes-kek"))
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	connRepo := repository.NewConnectionRepository(pool, enc)

	// 1. Create Workspace
	ws, err := wsRepo.Create(ctx, "Summarize & Skills Workspace")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	// 2. Create Connection
	conn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "Support WhatsApp",
		Slug:           "whatsapp-support",
		Channel:        "whatsapp",
		SenderIdentity: "+5511988880000",
		Status:         "active",
		IsDefault:      true,
		Credentials:    []byte(`{"token":"test-token"}`),
	}
	if err := connRepo.Create(ctx, conn); err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}

	// 3. Create Workspace Member (for assignment)
	memberAlice := &repository.WorkspaceMember{
		ID:                    uuid.New(),
		WorkspaceID:           ws.ID,
		Name:                  "Alice Specialist",
		Email:                 "alice.specialist@example.com",
		Role:                  "operator",
		AssignedConnectionIDs: []uuid.UUID{conn.ID},
		CreatedAt:             time.Now().UTC(),
		UpdatedAt:             time.Now().UTC(),
	}
	if err := wsRepo.AddMember(ctx, memberAlice); err != nil {
		t.Fatalf("failed to add workspace member: %v", err)
	}

	// 4. Create Contact and Chat
	customerPhone := "+5511977771234"
	customerName := "Joana Cliente"
	contact, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp", customerPhone, customerName, "", "")
	if err != nil {
		t.Fatalf("failed to resolve contact: %v", err)
	}

	chat, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &conn.ID, contact.ID)
	if err != nil {
		t.Fatalf("failed to find or create chat: %v", err)
	}

	// 5. Populate Chat Messages with customer issues and promises
	now := time.Now().UTC()
	msg1 := &domain.ChatMessage{
		ChatID:      chat.ID,
		WorkspaceID: ws.ID,
		UID:         "msg-inbound-1",
		Direction:   domain.DirectionInbound,
		SenderType:  domain.SenderTypeContact,
		SenderName:  customerName,
		SenderID:    customerPhone,
		Body:        "Olá! Estou com problema na minha fatura. O pagamento falhou e não consigo acessar o serviço.",
		CreatedAt:   now.Add(-10 * time.Minute),
	}
	if err := chatRepo.AddChatMessage(ctx, msg1); err != nil {
		t.Fatalf("failed to add chat message 1: %v", err)
	}

	msg2 := &domain.ChatMessage{
		ChatID:      chat.ID,
		WorkspaceID: ws.ID,
		UID:         "msg-outbound-2",
		Direction:   domain.DirectionOutbound,
		SenderType:  domain.SenderTypeHumanAgent,
		SenderName:  "Support Agent",
		SenderID:    conn.SenderIdentity,
		Body:        "Olá Joana! Verifiquei seu chamado. Vou enviar o comprovante de estorno e retorno o contato com o novo link.",
		CreatedAt:   now.Add(-5 * time.Minute),
	}
	if err := chatRepo.AddChatMessage(ctx, msg2); err != nil {
		t.Fatalf("failed to add chat message 2: %v", err)
	}

	msg3 := &domain.ChatMessage{
		ChatID:      chat.ID,
		WorkspaceID: ws.ID,
		UID:         "msg-inbound-3",
		Direction:   domain.DirectionInbound,
		SenderType:  domain.SenderTypeContact,
		SenderName:  customerName,
		SenderID:    customerPhone,
		Body:        "Muito obrigada! Fico no aguardo da reativação.",
		CreatedAt:   now.Add(-2 * time.Minute),
	}
	if err := chatRepo.AddChatMessage(ctx, msg3); err != nil {
		t.Fatalf("failed to add chat message 3: %v", err)
	}

	// 6. Setup Handlers & MCP Server
	mockPublisher := &reactionsMockPublisher{}
	presenceTracker := presence.NewTracker(15 * time.Second)

	inboxHandler := &admin.InboxHandler{
		Repo:        auditRepo,
		ContactRepo: contactRepo,
		ChatRepo:    chatRepo,
		Connections: connRepo,
		Workspaces:  wsRepo,
		Presence:    presenceTracker,
		Publisher:   mockPublisher,
	}

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

	e := echo.New()

	// -------------------------------------------------------------------------
	// Acceptance Criterion 1 & 2:
	// - Web Console conversation header renders 1-Click AI Summarize button
	// - 1-Click AI Summarize endpoint produces 3-bullet synopsis (Customer Issues, Promises Made, Current Status)
	// - MCP tool chat_summarize produces matching synopsis
	// -------------------------------------------------------------------------
	t.Run("Web Console Header Button Render", func(t *testing.T) {
		var buf bytes.Buffer
		err := components.ChatPanel(contact, chat, nil, nil, false).Render(ctx, &buf)
		if err != nil {
			t.Fatalf("failed to render ChatPanel: %v", err)
		}
		renderedHTML := buf.String()

		if !strings.Contains(renderedHTML, "1-Click AI Summarize") {
			t.Errorf("expected ChatPanel to render '1-Click AI Summarize' button, got:\n%s", renderedHTML)
		}
		expectedPostURL := "/admin/inbox/summarize?chat_id=" + chat.ID.String()
		if !strings.Contains(renderedHTML, expectedPostURL) {
			t.Errorf("expected ChatPanel to contain hx-post=%q", expectedPostURL)
		}
	})

	t.Run("Summarize Endpoint HTMX Popover", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/admin/inbox/summarize?chat_id="+chat.ID.String()+"&workspace_id="+ws.ID.String(), nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := inboxHandler.SummarizeChat(c)
		if err != nil {
			t.Fatalf("SummarizeChat HTMX returned error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		body := rec.Body.String()
		if !strings.Contains(body, "chat-summary-popover") {
			t.Errorf("expected popover container in response: %s", body)
		}
		if !strings.Contains(body, "Customer Issues") {
			t.Errorf("expected 'Customer Issues' heading in popover: %s", body)
		}
		if !strings.Contains(body, "Promises Made") {
			t.Errorf("expected 'Promises Made' heading in popover: %s", body)
		}
		if !strings.Contains(body, "Current Status") {
			t.Errorf("expected 'Current Status' heading in popover: %s", body)
		}
	})

	t.Run("Summarize Endpoint JSON Synopsis", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/admin/inbox/summarize?chat_id="+chat.ID.String()+"&workspace_id="+ws.ID.String(), nil)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := inboxHandler.SummarizeChat(c)
		if err != nil {
			t.Fatalf("SummarizeChat JSON returned error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var synopsis domain.ChatSynopsis
		if err := json.Unmarshal(rec.Body.Bytes(), &synopsis); err != nil {
			t.Fatalf("failed to unmarshal synopsis JSON: %v", err)
		}

		if synopsis.CustomerIssues == "" {
			t.Errorf("expected non-empty customer_issues")
		}
		if synopsis.PromisesMade == "" {
			t.Errorf("expected non-empty promises_made")
		}
		if synopsis.CurrentStatus == "" {
			t.Errorf("expected non-empty current_status")
		}
	})

	t.Run("MCP chat_summarize Tool", func(t *testing.T) {
		res, err := mcpServer.CallTool(ctx, "chat_summarize", map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
		})
		if err != nil {
			t.Fatalf("MCP chat_summarize error: %v", err)
		}
		if res.IsError {
			t.Fatalf("MCP chat_summarize returned tool error: %+v", res.Content)
		}

		textContent := res.Content[0].(mcp.TextContent).Text
		var parsed domain.ChatSynopsis
		if err := json.Unmarshal([]byte(textContent), &parsed); err != nil {
			t.Fatalf("failed to unmarshal MCP chat_summarize content: %v. Raw: %s", err, textContent)
		}

		if parsed.CustomerIssues == "" || parsed.PromisesMade == "" || parsed.CurrentStatus == "" {
			t.Errorf("expected all 3 synopsis fields to be populated in MCP response: %+v", parsed)
		}
	})

	// -------------------------------------------------------------------------
	// Acceptance Criterion 3:
	// - Every message send, note creation, assignment, and status mutation records
	//   an immutable entry in audit_logs.
	// -------------------------------------------------------------------------
	t.Run("Immutable Audit Logging for All Chat Actions", func(t *testing.T) {
		tenantCtx := tenant.WithWorkspaceID(domain.ContextWithWorkspaceID(context.Background(), ws.ID), ws.ID)

		// A. Message send via SendMessage
		sendForm := url.Values{
			"contact":            {customerPhone},
			"channel":            {"whatsapp"},
			"recipient_identity": {conn.SenderIdentity},
			"body":               {"Test audit message dispatched to customer."},
		}
		reqA := httptest.NewRequest(http.MethodPost, "/admin/inbox/send", strings.NewReader(sendForm.Encode()))
		reqA = reqA.WithContext(tenantCtx)
		reqA.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		recA := httptest.NewRecorder()
		cA := e.NewContext(reqA, recA)
		if err := inboxHandler.SendMessage(cA); err != nil {
			t.Fatalf("SendMessage returned error: %v", err)
		}
		if recA.Code != http.StatusNoContent && recA.Code != http.StatusOK {
			t.Fatalf("SendMessage returned code %d, body: %s", recA.Code, recA.Body.String())
		}

		// B. Note creation via CreateNote
		noteForm := url.Values{
			"chat_id":     {chat.ID.String()},
			"body":        {"Internal customer support note for audit testing."},
			"author_name": {"Auditor"},
		}
		reqB := httptest.NewRequest(http.MethodPost, "/admin/inbox/notes", strings.NewReader(noteForm.Encode()))
		reqB = reqB.WithContext(tenantCtx)
		reqB.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		recB := httptest.NewRecorder()
		cB := e.NewContext(reqB, recB)
		if err := inboxHandler.CreateNote(cB); err != nil {
			t.Fatalf("CreateNote returned error: %v", err)
		}
		if recB.Code != http.StatusCreated && recB.Code != http.StatusOK {
			t.Fatalf("CreateNote returned code %d, body: %s", recB.Code, recB.Body.String())
		}

		// C. Assignment via AssignChat
		assignForm := url.Values{
			"contact_id": {contact.ID.String()},
			"email":      {"alice.specialist@example.com"},
		}
		reqC := httptest.NewRequest(http.MethodPost, "/admin/inbox/assign", strings.NewReader(assignForm.Encode()))
		reqC = reqC.WithContext(tenantCtx)
		reqC.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		recC := httptest.NewRecorder()
		cC := e.NewContext(reqC, recC)
		if err := inboxHandler.AssignChat(cC); err != nil {
			t.Fatalf("AssignChat returned error: %v", err)
		}
		if recC.Code != http.StatusOK {
			t.Fatalf("AssignChat returned code %d, body: %s", recC.Code, recC.Body.String())
		}

		// D. Status change via UpdateChatStatus
		statusForm := url.Values{
			"contact_id": {contact.ID.String()},
			"status":     {"closed"},
		}
		reqD := httptest.NewRequest(http.MethodPost, "/admin/inbox/status", strings.NewReader(statusForm.Encode()))
		reqD = reqD.WithContext(tenantCtx)
		reqD.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		recD := httptest.NewRecorder()
		cD := e.NewContext(reqD, recD)
		if err := inboxHandler.UpdateChatStatus(cD); err != nil {
			t.Fatalf("UpdateChatStatus returned error: %v", err)
		}
		if recD.Code != http.StatusOK {
			t.Fatalf("UpdateChatStatus returned code %d, body: %s", recD.Code, recD.Body.String())
		}

		// E. Tag mutation via AddChatTag
		tagForm := url.Values{
			"contact_id": {contact.ID.String()},
			"tag":        {"urgent-vip"},
		}
		reqE := httptest.NewRequest(http.MethodPost, "/admin/inbox/tags/add", strings.NewReader(tagForm.Encode()))
		reqE = reqE.WithContext(tenantCtx)
		reqE.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		recE := httptest.NewRecorder()
		cE := e.NewContext(reqE, recE)
		if err := inboxHandler.AddChatTag(cE); err != nil {
			t.Fatalf("AddChatTag returned error: %v", err)
		}
		if recE.Code != http.StatusOK {
			t.Fatalf("AddChatTag returned code %d, body: %s", recE.Code, recE.Body.String())
		}

		// F. MCP Action Auditing: chat_create_draft_note & chat_send_message
		resMCPNote, err := mcpServer.CallTool(ctx, "chat_create_draft_note", map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
			"body":         "MCP agent audit note",
		})
		if err != nil || resMCPNote.IsError {
			t.Fatalf("MCP chat_create_draft_note failed: err=%v res=%+v", err, resMCPNote)
		}

		// Verify audit_logs table contains records for all 5 event types
		entries, err := auditRepo.ListAll(ctx, repository.AuditFilters{WorkspaceID: &ws.ID})
		if err != nil {
			t.Fatalf("failed to query audit logs: %v", err)
		}

		eventCounts := make(map[string]int)
		for _, entry := range entries {
			eventCounts[entry.EventType]++
			if entry.TraceID == "" {
				t.Errorf("audit entry %s has empty trace_id", entry.ID)
			}
			if len(entry.Payload) == 0 {
				t.Errorf("audit entry %s has empty payload", entry.ID)
			}
		}

		requiredEvents := []string{
			"chat.message.sent",
			"chat.note.created",
			"chat.assigned",
			"chat.status.updated",
			"chat.tag.updated",
		}

		for _, expectedType := range requiredEvents {
			if count := eventCounts[expectedType]; count == 0 {
				t.Errorf("expected at least 1 audit entry for %q, found %d. Total entries: %d", expectedType, count, len(entries))
			}
		}
	})

	// -------------------------------------------------------------------------
	// Acceptance Criterion 4:
	// - The repository includes 14 validated skills under .agents/skills/pergo-*
	//   with SKILL.md frontmatter, required sections, and execution recipes.
	// -------------------------------------------------------------------------
	t.Run("Skills Catalog Validation", func(t *testing.T) {
		// Find repository root by searching upward for .agents/skills
		skillsDir := findSkillsDir(t)
		if skillsDir == "" {
			t.Fatalf("could not locate .agents/skills directory")
		}

		expectedSkills := []string{
			"pergo-inbox-triage",
			"pergo-bulk-label",
			"pergo-account-brief",
			"pergo-draft-replies",
			"pergo-autoresponder",
			"pergo-send-message",
			"pergo-lead-qualifier",
			"pergo-followup-nudge",
			"pergo-inbox-analytics",
			"pergo-weekly-digest",
			"pergo-delivery-check",
			"pergo-team-rebalance",
			"pergo-connection-monitor",
			"pergo-setup-check",
		}

		requiredSections := []string{
			"## Overview",
			"## When to Use",
			"## Required MCP Tools",
			"## Step-by-step Execution Recipes",
			"## Safety Guardrails",
		}

		for _, skillName := range expectedSkills {
			skillPath := filepath.Join(skillsDir, skillName, "SKILL.md")
			data, err := os.ReadFile(skillPath)
			if err != nil {
				t.Errorf("missing SKILL.md for skill %q at %s: %v", skillName, skillPath, err)
				continue
			}

			content := string(data)

			// 1. Verify YAML frontmatter
			if !strings.HasPrefix(strings.TrimSpace(content), "---") {
				t.Errorf("skill %q missing leading '---' frontmatter delimiter", skillName)
			}
			parts := strings.SplitN(content, "---", 3)
			if len(parts) < 3 {
				t.Errorf("skill %q malformed YAML frontmatter (expected opening and closing ---)", skillName)
				continue
			}
			frontmatter := parts[1]
			if !strings.Contains(frontmatter, "name: "+skillName) {
				t.Errorf("skill %q frontmatter missing expected name: %s", skillName, frontmatter)
			}
			if !strings.Contains(frontmatter, "description:") {
				t.Errorf("skill %q frontmatter missing description: %s", skillName, frontmatter)
			}

			// 2. Verify all required markdown sections
			body := parts[2]
			for _, section := range requiredSections {
				if !strings.Contains(body, section) {
					t.Errorf("skill %q missing required section %q", skillName, section)
				}
			}

			// 3. Ensure recipe content is substantive (> 500 bytes)
			if len(body) < 500 {
				t.Errorf("skill %q body content too brief (%d bytes)", skillName, len(body))
			}
		}
	})
}

// findSkillsDir searches current and parent directories for .agents/skills.
func findSkillsDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get wd: %v", err)
	}

	for i := 0; i < 6; i++ {
		candidate := filepath.Join(dir, ".agents", "skills")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
