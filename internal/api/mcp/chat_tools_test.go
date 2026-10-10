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

func TestMCPChatTools(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	// Clean up
	_, _ = pool.Exec(ctx, "DELETE FROM chat_messages")
	_, _ = pool.Exec(ctx, "DELETE FROM chats")
	_, _ = pool.Exec(ctx, "DELETE FROM contact_identities")
	_, _ = pool.Exec(ctx, "DELETE FROM contacts")
	_, _ = pool.Exec(ctx, "DELETE FROM connections")
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

	ws, err := wsRepo.Create(ctx, "MCP Chat Workspace")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	connID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO connections (id, workspace_id, name, slug, channel, sender_identity, status, credentials, is_default)
		VALUES ($1, $2, 'WhatsApp Gateway', 'whatsapp-gateway', 'whatsapp_cloud', '+551100001111', 'active', '{}', true)
	`, connID, ws.ID)
	if err != nil {
		t.Fatalf("failed to insert connection: %v", err)
	}

	contact, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp_cloud", "+5511999998888", "Diana Prince", "", "+5511999998888")
	if err != nil {
		t.Fatalf("failed to create contact: %v", err)
	}

	chat, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &connID, contact.ID)
	if err != nil {
		t.Fatalf("failed to create chat: %v", err)
	}
	_ = chatRepo.IncrementUnreadCount(ctx, ws.ID, chat.ID)
	_ = chatRepo.SetChatTags(ctx, ws.ID, chat.ID, []string{"vip", "priority"})

	// Add messages to chat
	msg1 := &domain.ChatMessage{
		ChatID:      chat.ID,
		WorkspaceID: ws.ID,
		UID:         "mcp-msg-1",
		Direction:   domain.DirectionInbound,
		SenderType:  domain.SenderTypeContact,
		SenderName:  "Diana",
		SenderID:    "+5511999998888",
		Body:        "Hello MCP!",
		CreatedAt:   time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC),
	}
	msg2 := &domain.ChatMessage{
		ChatID:      chat.ID,
		WorkspaceID: ws.ID,
		UID:         "mcp-msg-2",
		Direction:   domain.DirectionOutbound,
		SenderType:  domain.SenderTypeHumanAgent,
		SenderName:  "Agent Smith",
		SenderID:    "+551100001111",
		Body:        "Greetings Diana! How can I assist?",
		CreatedAt:   time.Date(2026, 10, 10, 12, 1, 0, 0, time.UTC),
	}
	msg3 := &domain.ChatMessage{
		ChatID:      chat.ID,
		WorkspaceID: ws.ID,
		UID:         "mcp-msg-3",
		Direction:   domain.DirectionInbound,
		SenderType:  domain.SenderTypeContact,
		SenderName:  "Diana",
		SenderID:    "+5511999998888",
		Body:        "I need help with my account.",
		CreatedAt:   time.Date(2026, 10, 10, 12, 2, 0, 0, time.UTC),
	}
	_ = chatRepo.AddChatMessage(ctx, msg1)
	_ = chatRepo.AddChatMessage(ctx, msg2)
	_ = chatRepo.AddChatMessage(ctx, msg3)

	srv := NewServer(
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
		WithChatRepo(chatRepo),
	)

	t.Run("list_chats", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"unread":       true,
		}

		res, err := srv.handleListChats(ctx, req)
		if err != nil {
			t.Fatalf("handleListChats error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handleListChats returned tool error: %+v", res.Content)
		}

		text := res.Content[0].(mcp.TextContent).Text
		var parsed struct {
			Chats []ChatSummaryDTO `json:"chats"`
			Count int              `json:"count"`
		}
		if err := json.Unmarshal([]byte(text), &parsed); err != nil {
			t.Fatalf("failed to unmarshal output: %v", err)
		}

		if parsed.Count != 1 {
			t.Fatalf("expected count 1, got %d", parsed.Count)
		}
		if parsed.Chats[0].ContactName != "Diana Prince" {
			t.Errorf("expected contact Diana Prince, got %s", parsed.Chats[0].ContactName)
		}
	})

	t.Run("chat_details", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
		}

		res, err := srv.handleChatDetails(ctx, req)
		if err != nil {
			t.Fatalf("handleChatDetails error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handleChatDetails returned tool error: %+v", res.Content)
		}

		text := res.Content[0].(mcp.TextContent).Text
		var parsed struct {
			Chat    domain.Chat    `json:"chat"`
			Contact domain.Contact `json:"contact"`
		}
		if err := json.Unmarshal([]byte(text), &parsed); err != nil {
			t.Fatalf("failed to unmarshal output: %v", err)
		}

		if parsed.Chat.ID != chat.ID {
			t.Errorf("expected chat ID %s, got %s", chat.ID, parsed.Chat.ID)
		}
		if parsed.Contact.Name != "Diana Prince" {
			t.Errorf("expected contact name Diana Prince, got %s", parsed.Contact.Name)
		}
	})

	t.Run("chat_history_full_and_cursor", func(t *testing.T) {
		// 1. Full history
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
			"limit":        50,
		}

		res, err := srv.handleChatHistory(ctx, req)
		if err != nil {
			t.Fatalf("handleChatHistory error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handleChatHistory returned tool error: %+v", res.Content)
		}

		text := res.Content[0].(mcp.TextContent).Text
		var parsed struct {
			Messages []domain.ChatMessage `json:"messages"`
			Count    int                  `json:"count"`
		}
		if err := json.Unmarshal([]byte(text), &parsed); err != nil {
			t.Fatalf("failed to unmarshal output: %v", err)
		}

		if parsed.Count != 3 {
			t.Fatalf("expected 3 messages, got %d", parsed.Count)
		}
		if parsed.Messages[0].UID != "mcp-msg-1" || parsed.Messages[2].UID != "mcp-msg-3" {
			t.Errorf("expected chronological order [mcp-msg-1, mcp-msg-2, mcp-msg-3], got %s, %s", parsed.Messages[0].UID, parsed.Messages[2].UID)
		}

		// 2. Cursor pagination: before mcp-msg-3
		reqBefore := mcp.CallToolRequest{}
		reqBefore.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
			"before_uid":   "mcp-msg-3",
		}
		resBefore, err := srv.handleChatHistory(ctx, reqBefore)
		if err != nil {
			t.Fatalf("handleChatHistory before error: %v", err)
		}
		textBefore := resBefore.Content[0].(mcp.TextContent).Text
		var parsedBefore struct {
			Messages []domain.ChatMessage `json:"messages"`
			Count    int                  `json:"count"`
		}
		_ = json.Unmarshal([]byte(textBefore), &parsedBefore)
		if parsedBefore.Count != 2 {
			t.Fatalf("expected 2 messages before mcp-msg-3, got %d", parsedBefore.Count)
		}
		if parsedBefore.Messages[0].UID != "mcp-msg-1" || parsedBefore.Messages[1].UID != "mcp-msg-2" {
			t.Errorf("expected [mcp-msg-1, mcp-msg-2], got %s, %s", parsedBefore.Messages[0].UID, parsedBefore.Messages[1].UID)
		}
	})

	t.Run("chat_enable_ai", func(t *testing.T) {
		// 1. Disable AI
		res, err := srv.CallTool(ctx, "chat_enable_ai", map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
			"enabled":      false,
		})
		if err != nil {
			t.Fatalf("chat_enable_ai call error: %v", err)
		}
		if res.IsError {
			t.Fatalf("chat_enable_ai returned error: %+v", res.Content)
		}

		updatedChat, err := chatRepo.GetChat(ctx, ws.ID, chat.ID)
		if err != nil {
			t.Fatalf("get chat error: %v", err)
		}
		if !updatedChat.AIDisabled {
			t.Errorf("expected ai_disabled to be true after enabled=false")
		}

		// 2. Re-enable AI
		res2, err := srv.CallTool(ctx, "chat_enable_ai", map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
			"enabled":      true,
		})
		if err != nil {
			t.Fatalf("chat_enable_ai call error: %v", err)
		}
		if res2.IsError {
			t.Fatalf("chat_enable_ai returned error: %+v", res2.Content)
		}

		updatedChat2, err := chatRepo.GetChat(ctx, ws.ID, chat.ID)
		if err != nil {
			t.Fatalf("get chat error: %v", err)
		}
		if updatedChat2.AIDisabled {
			t.Errorf("expected ai_disabled to be false after enabled=true")
		}
	})

	t.Run("chat_create_draft_note", func(t *testing.T) {
		// 1. Success with author_name and workspace_id
		draftBody := "Proposed draft response from AI Agent for human review."
		author := "Antigravity Assistant"
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
			"body":         draftBody,
			"author_name":  author,
		}

		res, err := srv.handleChatCreateDraftNote(ctx, req)
		if err != nil {
			t.Fatalf("handleChatCreateDraftNote error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handleChatCreateDraftNote returned tool error: %+v", res.Content)
		}

		text := res.Content[0].(mcp.TextContent).Text
		var createdNote domain.ChatMessage
		if err := json.Unmarshal([]byte(text), &createdNote); err != nil {
			t.Fatalf("failed to unmarshal note: %v", err)
		}

		if createdNote.ChatID != chat.ID {
			t.Errorf("expected chat_id %s, got %s", chat.ID, createdNote.ChatID)
		}
		if createdNote.Direction != domain.DirectionInternalNote {
			t.Errorf("expected direction 'internal_note', got %s", createdNote.Direction)
		}
		if !createdNote.IsPrivate {
			t.Errorf("expected is_private=true")
		}
		if createdNote.Body != draftBody {
			t.Errorf("expected body %q, got %q", draftBody, createdNote.Body)
		}
		if createdNote.SenderName != author {
			t.Errorf("expected sender_name %q, got %q", author, createdNote.SenderName)
		}

		// 2. Success with workspace_id omitted (auto-resolution by chat_id)
		reqOmitted := mcp.CallToolRequest{}
		reqOmitted.Params.Arguments = map[string]any{
			"chat_id": chat.ID.String(),
			"body":    "Another draft note without ws_id.",
		}
		resOmitted, err := srv.handleChatCreateDraftNote(ctx, reqOmitted)
		if err != nil {
			t.Fatalf("handleChatCreateDraftNote omitted ws error: %v", err)
		}
		if resOmitted.IsError {
			t.Fatalf("handleChatCreateDraftNote returned error: %+v", resOmitted.Content)
		}
		var createdNote2 domain.ChatMessage
		_ = json.Unmarshal([]byte(resOmitted.Content[0].(mcp.TextContent).Text), &createdNote2)
		if createdNote2.SenderName != "AI Assistant" {
			t.Errorf("expected default sender_name 'AI Assistant', got %s", createdNote2.SenderName)
		}

		// 3. Verify notes appear in chat_history
		reqHist := mcp.CallToolRequest{}
		reqHist.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
			"limit":        10,
		}
		resHist, err := srv.handleChatHistory(ctx, reqHist)
		if err != nil {
			t.Fatalf("handleChatHistory error: %v", err)
		}
		var histParsed struct {
			Messages []domain.ChatMessage `json:"messages"`
			Count    int                  `json:"count"`
		}
		_ = json.Unmarshal([]byte(resHist.Content[0].(mcp.TextContent).Text), &histParsed)
		// 3 original messages + 2 notes = 5 messages
		if histParsed.Count != 5 {
			t.Fatalf("expected 5 messages in chat_history, got %d", histParsed.Count)
		}

		// 4. Error: chat not found
		reqNotFound := mcp.CallToolRequest{}
		reqNotFound.Params.Arguments = map[string]any{
			"chat_id": uuid.New().String(),
			"body":    "test",
		}
		resNotFound, _ := srv.handleChatCreateDraftNote(ctx, reqNotFound)
		if !resNotFound.IsError {
			t.Errorf("expected error for non-existent chat_id")
		}

		// 5. Error: empty body
		reqEmptyBody := mcp.CallToolRequest{}
		reqEmptyBody.Params.Arguments = map[string]any{
			"chat_id": chat.ID.String(),
			"body":    "   ",
		}
		resEmpty, _ := srv.handleChatCreateDraftNote(ctx, reqEmptyBody)
		if !resEmpty.IsError {
			t.Errorf("expected error for empty body")
		}
	})

	t.Run("workspace_team", func(t *testing.T) {
		// Add explicit member
		memberID := uuid.New()
		err := wsRepo.AddMember(ctx, &repository.WorkspaceMember{
			ID:                    memberID,
			WorkspaceID:           ws.ID,
			Name:                  "Bob Support",
			Email:                 "bob@example.com",
			Role:                  "operator",
			AssignedConnectionIDs: []uuid.UUID{connID},
		})
		if err != nil {
			t.Fatalf("failed to add member: %v", err)
		}

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
		}

		res, err := srv.handleWorkspaceTeam(ctx, req)
		if err != nil {
			t.Fatalf("handleWorkspaceTeam error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handleWorkspaceTeam returned error: %+v", res.Content)
		}

		text := res.Content[0].(mcp.TextContent).Text
		var parsed struct {
			WorkspaceID uuid.UUID     `json:"workspace_id"`
			Teammates   []TeammateDTO `json:"teammates"`
			Count       int           `json:"count"`
		}
		if err := json.Unmarshal([]byte(text), &parsed); err != nil {
			t.Fatalf("failed to unmarshal output: %v", err)
		}

		foundBob := false
		for _, tm := range parsed.Teammates {
			if tm.Email == "bob@example.com" {
				foundBob = true
				if tm.Role != "operator" {
					t.Errorf("expected role operator, got %s", tm.Role)
				}
				if len(tm.AccessibleAccounts) != 1 || tm.AccessibleAccounts[0].ID != connID {
					t.Errorf("expected 1 accessible connection matching connID, got %+v", tm.AccessibleAccounts)
				}
			}
		}
		if !foundBob {
			t.Fatalf("expected to find Bob Support in workspace team")
		}
	})

	t.Run("chat_assign_valid_and_unknown_validation_error", func(t *testing.T) {
		// 1. Unknown email must be rejected with explicit actionable validation error
		reqInvalid := mcp.CallToolRequest{}
		reqInvalid.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
			"email":        "ghost@nowhere.invalid",
		}

		resInvalid, err := srv.handleChatAssign(ctx, reqInvalid)
		if err != nil {
			t.Fatalf("handleChatAssign error: %v", err)
		}
		if !resInvalid.IsError {
			t.Fatalf("expected tool error for unknown email, got success: %+v", resInvalid.Content)
		}
		errText := resInvalid.Content[0].(mcp.TextContent).Text
		if !strings.Contains(errText, "ghost@nowhere.invalid") || !strings.Contains(errText, "workspace_team") {
			t.Errorf("expected actionable error mentioning email and workspace_team, got %q", errText)
		}

		// 2. Valid email assignment
		reqValid := mcp.CallToolRequest{}
		reqValid.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
			"email":        "bob@example.com",
		}

		resValid, err := srv.handleChatAssign(ctx, reqValid)
		if err != nil {
			t.Fatalf("handleChatAssign valid error: %v", err)
		}
		if resValid.IsError {
			t.Fatalf("expected success for bob@example.com, got error: %+v", resValid.Content)
		}

		// Verify chat state in DB
		updatedChat, err := chatRepo.GetChat(ctx, ws.ID, chat.ID)
		if err != nil {
			t.Fatalf("failed to fetch updated chat: %v", err)
		}
		if updatedChat.AssignedEmail == nil || *updatedChat.AssignedEmail != "bob@example.com" {
			t.Errorf("expected assigned email bob@example.com, got %v", updatedChat.AssignedEmail)
		}
	})

	t.Run("chat_unassign", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
		}

		res, err := srv.handleChatUnassign(ctx, req)
		if err != nil {
			t.Fatalf("handleChatUnassign error: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success, got error: %+v", res.Content)
		}

		updatedChat, _ := chatRepo.GetChat(ctx, ws.ID, chat.ID)
		if updatedChat.AssignedEmail != nil {
			t.Errorf("expected nil assigned email after unassign, got %v", *updatedChat.AssignedEmail)
		}
	})

	t.Run("chat_set_label_idempotent_and_remove_label", func(t *testing.T) {
		// Set label 'urgent'
		reqSet := mcp.CallToolRequest{}
		reqSet.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
			"label":        "urgent",
		}

		resSet, err := srv.handleChatSetLabel(ctx, reqSet)
		if err != nil || resSet.IsError {
			t.Fatalf("handleChatSetLabel failed: %v, %+v", err, resSet)
		}

		// Idempotency: set label 'urgent' again
		resSet2, err := srv.handleChatSetLabel(ctx, reqSet)
		if err != nil || resSet2.IsError {
			t.Fatalf("handleChatSetLabel 2 failed: %v, %+v", err, resSet2)
		}

		chatAfterSet, _ := chatRepo.GetChat(ctx, ws.ID, chat.ID)
		urgentCount := 0
		for _, tag := range chatAfterSet.Tags {
			if tag == "urgent" {
				urgentCount++
			}
		}
		if urgentCount != 1 {
			t.Errorf("expected exactly 1 instance of 'urgent' tag, got %d in %+v", urgentCount, chatAfterSet.Tags)
		}

		// Remove label 'urgent'
		reqRemove := mcp.CallToolRequest{}
		reqRemove.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
			"label":        "urgent",
		}

		resRemove, err := srv.handleChatRemoveLabel(ctx, reqRemove)
		if err != nil || resRemove.IsError {
			t.Fatalf("handleChatRemoveLabel failed: %v, %+v", err, resRemove)
		}

		chatAfterRemove, _ := chatRepo.GetChat(ctx, ws.ID, chat.ID)
		for _, tag := range chatAfterRemove.Tags {
			if tag == "urgent" {
				t.Errorf("expected 'urgent' tag to be removed, but still present in %+v", chatAfterRemove.Tags)
			}
		}
	})

	t.Run("chat_close_and_chat_open", func(t *testing.T) {
		// Close
		reqClose := mcp.CallToolRequest{}
		reqClose.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
		}

		resClose, err := srv.handleChatClose(ctx, reqClose)
		if err != nil || resClose.IsError {
			t.Fatalf("handleChatClose failed: %v, %+v", err, resClose)
		}

		chatClosed, _ := chatRepo.GetChat(ctx, ws.ID, chat.ID)
		if chatClosed.Status != "closed" {
			t.Errorf("expected closed status, got %s", chatClosed.Status)
		}

		// Reopen
		reqOpen := mcp.CallToolRequest{}
		reqOpen.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
		}

		resOpen, err := srv.handleChatOpen(ctx, reqOpen)
		if err != nil || resOpen.IsError {
			t.Fatalf("handleChatOpen failed: %v, %+v", err, resOpen)
		}

		chatOpened, _ := chatRepo.GetChat(ctx, ws.ID, chat.ID)
		if chatOpened.Status != "open" {
			t.Errorf("expected open status, got %s", chatOpened.Status)
		}
	})

	t.Run("list_chats_with_triage_filters", func(t *testing.T) {
		// Assign chat to bob@example.com
		_ = chatRepo.AssignChat(ctx, ws.ID, chat.ID, nil, &[]string{"bob@example.com"}[0])

		// 1. Filter by assigned_email
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id":   ws.ID.String(),
			"assigned_email": "bob@example.com",
		}
		res, err := srv.handleListChats(ctx, req)
		if err != nil || res.IsError {
			t.Fatalf("handleListChats assigned error: %v, %+v", err, res)
		}
		var parsed struct {
			Chats []ChatSummaryDTO `json:"chats"`
			Count int              `json:"count"`
		}
		_ = json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &parsed)
		if parsed.Count != 1 {
			t.Errorf("expected 1 chat for bob@example.com, got %d", parsed.Count)
		}

		// 2. Filter by unassigned
		reqUnassigned := mcp.CallToolRequest{}
		reqUnassigned.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"unassigned":   true,
		}
		resUnassigned, _ := srv.handleListChats(ctx, reqUnassigned)
		var parsedUnassigned struct {
			Chats []ChatSummaryDTO `json:"chats"`
			Count int              `json:"count"`
		}
		_ = json.Unmarshal([]byte(resUnassigned.Content[0].(mcp.TextContent).Text), &parsedUnassigned)
		if parsedUnassigned.Count != 0 {
			t.Errorf("expected 0 unassigned chats, got %d", parsedUnassigned.Count)
		}

		// 3. Filter by tag
		reqTag := mcp.CallToolRequest{}
		reqTag.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"tag":          "vip",
		}
		resTag, _ := srv.handleListChats(ctx, reqTag)
		var parsedTag struct {
			Chats []ChatSummaryDTO `json:"chats"`
			Count int              `json:"count"`
		}
		_ = json.Unmarshal([]byte(resTag.Content[0].(mcp.TextContent).Text), &parsedTag)
		if parsedTag.Count != 1 {
			t.Errorf("expected 1 chat with tag 'vip', got %d", parsedTag.Count)
		}
	})

	t.Run("workspace_quotas", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
		}
		res, err := srv.handleWorkspaceQuotas(ctx, req)
		if err != nil {
			t.Fatalf("handleWorkspaceQuotas error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handleWorkspaceQuotas returned tool error: %+v", res.Content)
		}
		var quotas WorkspaceQuotasDTO
		if err := json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &quotas); err != nil {
			t.Fatalf("failed to unmarshal quotas: %v", err)
		}
		if quotas.WorkspaceID != ws.ID {
			t.Errorf("expected workspace ID %s, got %s", ws.ID, quotas.WorkspaceID)
		}
		if quotas.Plan != "pro" {
			t.Errorf("expected plan pro, got %s", quotas.Plan)
		}
		if quotas.SeatsLimit <= 0 {
			t.Errorf("expected seats_limit > 0, got %d", quotas.SeatsLimit)
		}
	})

	t.Run("chat_summarize", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
		}
		res, err := srv.handleChatSummarize(ctx, req)
		if err != nil {
			t.Fatalf("handleChatSummarize returned error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handleChatSummarize returned tool error: %+v", res.Content)
		}

		var parsed struct {
			ChatID         uuid.UUID `json:"chat_id"`
			CustomerIssues string    `json:"customer_issues"`
			PromisesMade   string    `json:"promises_made"`
			CurrentStatus  string    `json:"current_status"`
		}
		if err := json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &parsed); err != nil {
			t.Fatalf("failed to unmarshal synopsis: %v", err)
		}

		if parsed.ChatID != chat.ID {
			t.Errorf("expected chat_id %s, got %s", chat.ID, parsed.ChatID)
		}
		if parsed.CustomerIssues == "" {
			t.Errorf("expected non-empty customer_issues")
		}
		if parsed.PromisesMade == "" {
			t.Errorf("expected non-empty promises_made")
		}
		if parsed.CurrentStatus == "" {
			t.Errorf("expected non-empty current_status")
		}

		// Also verify via CallTool
		resCall, err := srv.CallTool(ctx, "chat_summarize", map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
		})
		if err != nil || resCall.IsError {
			t.Fatalf("CallTool chat_summarize failed: %v, %+v", err, resCall)
		}
	})

	t.Run("chat_send_message_and_audit", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
			"message":      "Enviaremos a proposta comercial por email em breve.",
			"sender_name":  "Claude Bot",
		}
		res, err := srv.handleChatSendMessage(ctx, req)
		if err != nil {
			t.Fatalf("handleChatSendMessage returned error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handleChatSendMessage returned tool error: %+v", res.Content)
		}

		// Verify audit log entry
		entries, total, err := auditRepo.ListFiltered(ctx, repository.AuditFilters{
			WorkspaceID: &ws.ID,
			EventType:   "chat.message.sent",
		})
		if err != nil {
			t.Fatalf("ListFiltered audit logs failed: %v", err)
		}
		if total == 0 || len(entries) == 0 {
			t.Fatalf("expected audit log entry with event_type 'chat.message.sent', got 0")
		}
	})

	t.Run("workspace_whatsapp_accounts", func(t *testing.T) {
		res, err := srv.CallTool(ctx, "workspace_whatsapp_accounts", map[string]any{
			"workspace_id": ws.ID.String(),
		})
		if err != nil {
			t.Fatalf("CallTool workspace_whatsapp_accounts error: %v", err)
		}
		if res.IsError {
			t.Fatalf("CallTool workspace_whatsapp_accounts returned tool error: %+v", res.Content)
		}
		var parsed struct {
			Accounts []struct {
				ConnectionID   string `json:"connection_id"`
				Name           string `json:"name"`
				Channel        string `json:"channel"`
				SenderIdentity string `json:"sender_identity"`
				Status         string `json:"status"`
			} `json:"accounts"`
			Count int `json:"count"`
		}
		if err := json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &parsed); err != nil {
			t.Fatalf("failed to unmarshal accounts: %v", err)
		}
		if parsed.Count == 0 || len(parsed.Accounts) == 0 {
			t.Fatalf("expected at least 1 whatsapp account, got 0")
		}
		found := false
		for _, acc := range parsed.Accounts {
			if acc.ConnectionID == connID.String() {
				found = true
				if acc.Channel != "whatsapp_cloud" {
					t.Errorf("expected channel whatsapp_cloud, got %s", acc.Channel)
				}
				if acc.Status != "connected" {
					t.Errorf("expected status connected, got %s", acc.Status)
				}
			}
		}
		if !found {
			t.Errorf("expected to find connection %s in accounts", connID)
		}
	})

	t.Run("message_details", func(t *testing.T) {
		res, err := srv.CallTool(ctx, "message_details", map[string]any{
			"workspace_id": ws.ID.String(),
			"message_uid":  "mcp-msg-1",
			"chat_id":      chat.ID.String(),
		})
		if err != nil {
			t.Fatalf("CallTool message_details error: %v", err)
		}
		if res.IsError {
			t.Fatalf("CallTool message_details returned tool error: %+v", res.Content)
		}
		var parsed struct {
			UID            string `json:"uid"`
			Direction      string `json:"direction"`
			Status         string `json:"status"`
			DeliveryStatus string `json:"delivery_status"`
			Body           string `json:"body"`
		}
		if err := json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &parsed); err != nil {
			t.Fatalf("failed to unmarshal message details: %v", err)
		}
		if parsed.UID != "mcp-msg-1" {
			t.Errorf("expected UID mcp-msg-1, got %s", parsed.UID)
		}
		if parsed.Direction != "inbound" {
			t.Errorf("expected inbound, got %s", parsed.Direction)
		}
		if parsed.DeliveryStatus == "" {
			t.Errorf("expected non-empty delivery_status")
		}
	})

	t.Run("whatsapp_account_send_message", func(t *testing.T) {
		res, err := srv.CallTool(ctx, "whatsapp_account_send_message", map[string]any{
			"workspace_id":  ws.ID.String(),
			"connection_id": connID.String(),
			"to":            "+5511999990000",
			"message":       "Olá! Esta é uma notificação via WhatsApp account.",
			"sender_name":   "OrderBot",
		})
		if err != nil {
			t.Fatalf("CallTool whatsapp_account_send_message error: %v", err)
		}
		if res.IsError {
			t.Fatalf("CallTool whatsapp_account_send_message returned tool error: %+v", res.Content)
		}
		var parsed struct {
			Success      bool      `json:"success"`
			ConnectionID uuid.UUID `json:"connection_id"`
			TraceID      string    `json:"trace_id"`
			To           string    `json:"to"`
		}
		if err := json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &parsed); err != nil {
			t.Fatalf("failed to unmarshal send result: %v", err)
		}
		if !parsed.Success {
			t.Errorf("expected success true")
		}
		if parsed.ConnectionID != connID {
			t.Errorf("expected connection_id %s, got %s", connID, parsed.ConnectionID)
		}
		if parsed.To != "+5511999990000" {
			t.Errorf("expected to +5511999990000, got %s", parsed.To)
		}
	})

	t.Run("message_reply_and_chat_send_message_reply_to_uid", func(t *testing.T) {
		// 1. Send reply via message_reply
		replyRes, err := srv.CallTool(ctx, "message_reply", map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
			"reply_to_uid": "mcp-msg-1",
			"message":      "Respondendo diretamente à sua dúvida anterior.",
			"sender_name":  "Support Rep",
		})
		if err != nil {
			t.Fatalf("CallTool message_reply error: %v", err)
		}
		if replyRes.IsError {
			t.Fatalf("CallTool message_reply returned tool error: %+v", replyRes.Content)
		}
		var parsedReply struct {
			Success    bool    `json:"success"`
			TraceID    string  `json:"trace_id"`
			ReplyToUID *string `json:"reply_to_uid"`
		}
		if err := json.Unmarshal([]byte(replyRes.Content[0].(mcp.TextContent).Text), &parsedReply); err != nil {
			t.Fatalf("failed to unmarshal reply result: %v", err)
		}
		if !parsedReply.Success {
			t.Errorf("expected success true")
		}
		if parsedReply.ReplyToUID == nil || *parsedReply.ReplyToUID != "mcp-msg-1" {
			t.Errorf("expected reply_to_uid 'mcp-msg-1', got %+v", parsedReply.ReplyToUID)
		}

		// Verify stored in DB with reply_to_uid
		storedMsg, err := chatRepo.GetChatMessageByUID(ctx, ws.ID, parsedReply.TraceID)
		if err != nil {
			t.Fatalf("failed to query stored reply message: %v", err)
		}
		if storedMsg.ReplyToUID == nil || *storedMsg.ReplyToUID != "mcp-msg-1" {
			t.Errorf("expected stored msg reply_to_uid 'mcp-msg-1', got %+v", storedMsg.ReplyToUID)
		}

		// 2. Also verify chat_send_message with optional reply_to_uid
		sendRes, err := srv.CallTool(ctx, "chat_send_message", map[string]any{
			"workspace_id": ws.ID.String(),
			"chat_id":      chat.ID.String(),
			"message":      "Mais uma resposta encadeada.",
			"reply_to_uid": "mcp-msg-1",
		})
		if err != nil {
			t.Fatalf("CallTool chat_send_message with reply_to_uid error: %v", err)
		}
		if sendRes.IsError {
			t.Fatalf("CallTool chat_send_message returned tool error: %+v", sendRes.Content)
		}
		var parsedSend struct {
			Success    bool    `json:"success"`
			TraceID    string  `json:"trace_id"`
			ReplyToUID *string `json:"reply_to_uid"`
		}
		if err := json.Unmarshal([]byte(sendRes.Content[0].(mcp.TextContent).Text), &parsedSend); err != nil {
			t.Fatalf("failed to unmarshal send result: %v", err)
		}
		if parsedSend.ReplyToUID == nil || *parsedSend.ReplyToUID != "mcp-msg-1" {
			t.Errorf("expected chat_send_message reply_to_uid 'mcp-msg-1', got %+v", parsedSend.ReplyToUID)
		}
	})
}

