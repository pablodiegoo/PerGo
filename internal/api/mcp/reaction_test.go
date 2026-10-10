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
	"github.com/pablojhp.pergo/internal/webhook"
)

type mockReactionPublisher struct {
	subject string
	data    []byte
	traceID string
}

func (m *mockReactionPublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	m.subject = subject
	m.data = data
	m.traceID = traceID
	return nil
}

type mockReactionDispatcher struct {
	dispatchedTasks []webhook.WebhookDeliveryTask
}

func (m *mockReactionDispatcher) Dispatch(ctx context.Context, task webhook.WebhookDeliveryTask) error {
	m.dispatchedTasks = append(m.dispatchedTasks, task)
	return nil
}

func (m *mockReactionDispatcher) WriteToDLQ(ctx context.Context, workspaceID uuid.UUID, subscriptionID uuid.UUID, traceID, messageID, event string, rawEvent []byte, attempts int, failReason string) error {
	return nil
}

func TestMCPMessageReact(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	// Clean up
	_, _ = pool.Exec(ctx, "DELETE FROM webhook_subscriptions")
	_, _ = pool.Exec(ctx, "DELETE FROM chat_messages")
	_, _ = pool.Exec(ctx, "DELETE FROM chats")
	_, _ = pool.Exec(ctx, "DELETE FROM contact_identities")
	_, _ = pool.Exec(ctx, "DELETE FROM contacts")
	_, _ = pool.Exec(ctx, "DELETE FROM connections")
	_, _ = pool.Exec(ctx, "DELETE FROM workspaces")

	kek := make([]byte, 32)
	copy(kek, []byte("dev-development-key-32-bytes-kek"))
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	connRepo := repository.NewConnectionRepository(pool, enc)
	wsRepo := repository.NewWorkspaceRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	chatRepo := repository.NewChatRepository(pool)
	webhookSubRepo := repository.NewWebhookSubscriptionRepository(pool, enc)

	ws, err := wsRepo.Create(ctx, "MCP Reaction Workspace")
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

	msgUID := "test-msg-uid-" + uuid.New().String()
	testMsg := &domain.ChatMessage{
		ChatID:      chat.ID,
		WorkspaceID: ws.ID,
		UID:         msgUID,
		Direction:   string(domain.DirectionInbound),
		SenderType:  string(domain.SenderTypeContact),
		Body:        "React to me!",
		CreatedAt:   time.Now().UTC(),
	}
	if err := chatRepo.AddChatMessage(ctx, testMsg); err != nil {
		t.Fatalf("failed to add chat message: %v", err)
	}

	// Create webhook subscription
	sub, err := webhookSubRepo.Create(ctx, ws.ID, "https://example.com/webhook", []string{"message.reaction.updated"}, []byte("webhooksecret"))
	if err != nil {
		t.Fatalf("failed to create webhook subscription: %v", err)
	}

	pub := &mockReactionPublisher{}
	dispatcher := &mockReactionDispatcher{}

	server := NewServer(
		wsRepo,
		connRepo,
		contactRepo,
		nil,
		nil,
		nil,
		webhookSubRepo,
		nil,
		dispatcher,
		[]byte("secret"),
		"http://localhost:8080",
		WithChatRepo(chatRepo),
		WithPublisher(pub),
	)

	t.Run("AddReaction_Success", func(t *testing.T) {
		args := map[string]any{
			"workspace_id": ws.ID.String(),
			"message_uid":  msgUID,
			"emoji":        "👍",
			"action":       "add",
			"sender":       "ai_agent_1",
		}

		res, err := server.CallTool(ctx, "message_react", args)
		if err != nil {
			t.Fatalf("message_react failed: %v", err)
		}
		if res.IsError {
			t.Fatalf("message_react returned error: %+v", res.Content)
		}

		var parsed struct {
			MessageUID string            `json:"message_uid"`
			Action     string            `json:"action"`
			Emoji      string            `json:"emoji"`
			Reactions  []domain.Reaction `json:"reactions"`
			Count      int               `json:"count"`
		}
		resText := res.Content[0].(mcp.TextContent).Text
		if err := json.Unmarshal([]byte(resText), &parsed); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if parsed.Count != 1 || parsed.Reactions[0].Emoji != "👍" {
			t.Errorf("expected 1 reaction with 👍, got: %+v", parsed.Reactions)
		}

		// Verify database
		dbMsg, err := chatRepo.GetChatMessageByUID(ctx, ws.ID, msgUID)
		if err != nil {
			t.Fatalf("failed to get message from DB: %v", err)
		}
		if len(dbMsg.Reactions) != 1 || dbMsg.Reactions[0].Emoji != "👍" {
			t.Errorf("expected 1 reaction in DB, got: %+v", dbMsg.Reactions)
		}

		// Verify NATS broadcast
		if pub.subject != "messages.events.reaction_updated" {
			t.Errorf("expected NATS subject messages.events.reaction_updated, got %s", pub.subject)
		}

		// Verify Webhook dispatched
		if len(dispatcher.dispatchedTasks) != 1 {
			t.Fatalf("expected 1 webhook task dispatched, got %d", len(dispatcher.dispatchedTasks))
		}
		task := dispatcher.dispatchedTasks[0]
		if task.SubscriptionID != sub.ID {
			t.Errorf("expected subscription ID %s, got %s", sub.ID, task.SubscriptionID)
		}
		if task.Event != "message.reaction.updated" {
			t.Errorf("expected event message.reaction.updated, got %s", task.Event)
		}
	})

	t.Run("AddReaction_Idempotent", func(t *testing.T) {
		args := map[string]any{
			"message_uid": msgUID, // test resolving workspace_id automatically
			"emoji":       "👍",
			"action":      "add",
			"sender":      "ai_agent_1",
		}

		res, err := server.CallTool(ctx, "message_react", args)
		if err != nil {
			t.Fatalf("message_react failed: %v", err)
		}
		if res.IsError {
			t.Fatalf("message_react returned error: %+v", res.Content)
		}

		var parsed struct {
			Count int `json:"count"`
		}
		_ = json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &parsed)
		if parsed.Count != 1 {
			t.Errorf("expected count to remain 1 on duplicate add, got %d", parsed.Count)
		}
	})

	t.Run("RemoveReaction_Success", func(t *testing.T) {
		args := map[string]any{
			"workspace_id": ws.ID.String(),
			"message_uid":  msgUID,
			"emoji":        "👍",
			"action":       "remove",
			"sender":       "ai_agent_1",
		}

		res, err := server.CallTool(ctx, "message_react", args)
		if err != nil {
			t.Fatalf("message_react remove failed: %v", err)
		}
		if res.IsError {
			t.Fatalf("message_react returned error: %+v", res.Content)
		}

		var parsed struct {
			Count int `json:"count"`
		}
		_ = json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &parsed)
		if parsed.Count != 0 {
			t.Errorf("expected count 0 after remove, got %d", parsed.Count)
		}

		// Verify database
		dbMsg, err := chatRepo.GetChatMessageByUID(ctx, ws.ID, msgUID)
		if err != nil {
			t.Fatalf("failed to get message from DB: %v", err)
		}
		if len(dbMsg.Reactions) != 0 {
			t.Errorf("expected 0 reactions in DB after remove, got: %+v", dbMsg.Reactions)
		}
	})

	t.Run("Validation_Errors", func(t *testing.T) {
		// Missing message_uid
		res, _ := server.CallTool(ctx, "message_react", map[string]any{"emoji": "👍"})
		if !res.IsError {
			t.Error("expected error for missing message_uid")
		}

		// Missing emoji
		res, _ = server.CallTool(ctx, "message_react", map[string]any{"message_uid": msgUID})
		if !res.IsError {
			t.Error("expected error for missing emoji")
		}

		// Invalid action
		res, _ = server.CallTool(ctx, "message_react", map[string]any{
			"message_uid": msgUID,
			"emoji":       "👍",
			"action":      "invalid_action",
		})
		if !res.IsError {
			t.Error("expected error for invalid action")
		}
	})
}
