package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/pablojhp.pergo/internal/chat/activity"
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
	subRepo             *repository.WebhookSubscriptionRepository
	dispatchedTasks     []webhook.WebhookDeliveryTask
	dispatchedReactions []struct {
		WorkspaceID uuid.UUID
		MessageUID  string
		Payload     []byte
		TraceID     string
	}
}

func (m *mockReactionDispatcher) Dispatch(ctx context.Context, task webhook.WebhookDeliveryTask) error {
	m.dispatchedTasks = append(m.dispatchedTasks, task)
	return nil
}

func (m *mockReactionDispatcher) DispatchReaction(ctx context.Context, workspaceID uuid.UUID, messageUID string, payload []byte, traceID string) error {
	m.dispatchedReactions = append(m.dispatchedReactions, struct {
		WorkspaceID uuid.UUID
		MessageUID  string
		Payload     []byte
		TraceID     string
	}{workspaceID, messageUID, payload, traceID})
	if m.subRepo != nil {
		subs, err := m.subRepo.ListByWorkspace(ctx, workspaceID)
		if err != nil {
			return err
		}
		for _, sub := range subs {
			if !sub.Active {
				continue
			}
			if webhook.MatchesAny(sub.EventTypes, "message.reaction.updated") {
				_ = m.Dispatch(ctx, webhook.WebhookDeliveryTask{
					ID:             uuid.New(),
					SubscriptionID: sub.ID,
					WorkspaceID:    workspaceID,
					Event:          "message.reaction.updated",
					TraceID:        traceID,
					MessageID:      messageUID,
					Payload:        payload,
					Mode:           "reaction",
				})
			}
		}
	}
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
	auditRepo := repository.NewAuditRepository(pool)
	webhookSubRepo := repository.NewWebhookSubscriptionRepository(pool, enc)
	act := activity.New(pool, nil, nil, nil)

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
	if _, err := act.Inbound(ctx, ws.ID, activity.InboundTurn{
		Chat:     activity.ChatID(chat.ID).WithChannel("whatsapp_cloud"),
		UID:      msgUID,
		SenderID: "+5511999998888",
		Body:     "React to me!",
		Channel:  "whatsapp_cloud",
		At:       time.Now().UTC(),
	}); err != nil {
		t.Fatalf("failed to add chat message: %v", err)
	}

	// Create webhook subscription
	sub, err := webhookSubRepo.Create(ctx, ws.ID, "https://example.com/webhook", []string{"message.reaction.updated"}, []byte("webhooksecret"))
	if err != nil {
		t.Fatalf("failed to create webhook subscription: %v", err)
	}

	pub := &mockReactionPublisher{}
	dispatcher := &mockReactionDispatcher{subRepo: webhookSubRepo}

	server := NewServer(
		wsRepo,
		connRepo,
		contactRepo,
		auditRepo,
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

		// Verify Webhook dispatched via DispatchReaction
		if len(dispatcher.dispatchedReactions) != 1 {
			t.Fatalf("expected 1 DispatchReaction call, got %d", len(dispatcher.dispatchedReactions))
		}
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

	t.Run("ToggleReaction_Success", func(t *testing.T) {
		args := map[string]any{
			"workspace_id": ws.ID.String(),
			"message_uid":  msgUID,
			"emoji":        "🎉",
			"action":       "toggle",
			"sender":       "ai_agent_1",
		}

		// First toggle adds
		res, err := server.CallTool(ctx, "message_react", args)
		if err != nil || res.IsError {
			t.Fatalf("message_react toggle add failed: err=%v, res=%+v", err, res)
		}
		var parsed struct {
			Action string `json:"action"`
			Count  int    `json:"count"`
		}
		_ = json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &parsed)
		if parsed.Action != "add" || parsed.Count != 1 {
			t.Fatalf("expected toggle to resolve to add with count=1, got action=%q count=%d", parsed.Action, parsed.Count)
		}

		// Second toggle removes
		res2, err := server.CallTool(ctx, "message_react", args)
		if err != nil || res2.IsError {
			t.Fatalf("message_react toggle remove failed: err=%v, res=%+v", err, res2)
		}
		_ = json.Unmarshal([]byte(res2.Content[0].(mcp.TextContent).Text), &parsed)
		if parsed.Action != "remove" || parsed.Count != 0 {
			t.Fatalf("expected second toggle to resolve to remove with count=0, got action=%q count=%d", parsed.Action, parsed.Count)
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
