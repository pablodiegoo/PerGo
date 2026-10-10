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
		Direction:   string(domain.DirectionInbound),
		SenderType:  string(domain.SenderTypeContact),
		SenderName:  "Diana",
		SenderID:    "+5511999998888",
		Body:        "Hello MCP!",
		CreatedAt:   time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC),
	}
	msg2 := &domain.ChatMessage{
		ChatID:      chat.ID,
		WorkspaceID: ws.ID,
		UID:         "mcp-msg-2",
		Direction:   string(domain.DirectionOutbound),
		SenderType:  string(domain.SenderTypeHumanAgent),
		SenderName:  "Agent Smith",
		SenderID:    "+551100001111",
		Body:        "Greetings Diana! How can I assist?",
		CreatedAt:   time.Date(2026, 10, 10, 12, 1, 0, 0, time.UTC),
	}
	msg3 := &domain.ChatMessage{
		ChatID:      chat.ID,
		WorkspaceID: ws.ID,
		UID:         "mcp-msg-3",
		Direction:   string(domain.DirectionInbound),
		SenderType:  string(domain.SenderTypeContact),
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
		nil,
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
}

