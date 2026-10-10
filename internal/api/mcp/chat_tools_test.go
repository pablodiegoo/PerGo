package mcp

import (
	"context"
	"encoding/json"
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
		if createdNote.Direction != string(domain.DirectionInternalNote) {
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
}

