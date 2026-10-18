package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/chat/activity"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestChatRepository(t *testing.T) {
	pool := getTestPoolWithMigrations(t)
	defer pool.Close()

	ctx := context.Background()

	// Clean up
	_, _ = pool.Exec(ctx, "DELETE FROM chat_messages")
	_, _ = pool.Exec(ctx, "DELETE FROM chats")
	_, _ = pool.Exec(ctx, "DELETE FROM contact_identities")
	_, _ = pool.Exec(ctx, "DELETE FROM contacts")
	_, _ = pool.Exec(ctx, "DELETE FROM connections")
	_, _ = pool.Exec(ctx, "DELETE FROM workspaces")

	chatRepo := repository.NewChatRepository(pool)
	wsRepo := repository.NewWorkspaceRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	act := activity.New(pool, nil, nil, nil)

	// Create test workspace
	ws, err := wsRepo.Create(ctx, "chat_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	// Create test connection
	connID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO connections (id, workspace_id, name, slug, channel, sender_identity, status, credentials, is_default)
		VALUES ($1, $2, 'Test Conn', 'test-conn', 'whatsapp_cloud', '123456789', 'active', '{}', true)
	`, connID, ws.ID)
	if err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}

	// Create test contact
	contact, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp_cloud", "+5511999999999", "Alice Wonderland", "", "+5511999999999")
	if err != nil {
		t.Fatalf("failed to create contact: %v", err)
	}

	t.Run("FindOrCreateChat_NewAndExisting", func(t *testing.T) {
		chat1, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &connID, contact.ID)
		if err != nil {
			t.Fatalf("failed to find or create chat: %v", err)
		}
		if chat1.ID == uuid.Nil {
			t.Fatal("expected non-nil chat ID")
		}
		if chat1.Status != domain.ChatStatusOpen {
			t.Errorf("expected open status, got %s", chat1.Status)
		}
		if chat1.UnreadCount != 0 {
			t.Errorf("expected 0 unread count, got %d", chat1.UnreadCount)
		}

		// Second call should return the exact same chat
		chat2, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &connID, contact.ID)
		if err != nil {
			t.Fatalf("failed to get existing chat: %v", err)
		}
		if chat2.ID != chat1.ID {
			t.Fatalf("expected same chat ID %s, got %s", chat1.ID, chat2.ID)
		}
	})

	t.Run("GetChat_SuccessAndNotFound", func(t *testing.T) {
		chat, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &connID, contact.ID)
		if err != nil {
			t.Fatalf("failed to find or create chat: %v", err)
		}

		fetched, err := chatRepo.GetChat(ctx, ws.ID, chat.ID)
		if err != nil {
			t.Fatalf("failed to get chat: %v", err)
		}
		if fetched.ID != chat.ID {
			t.Errorf("expected ID %s, got %s", chat.ID, fetched.ID)
		}

		_, err = chatRepo.GetChat(ctx, ws.ID, uuid.New())
		if err != repository.ErrChatNotFound {
			t.Errorf("expected ErrChatNotFound, got %v", err)
		}
	})

	t.Run("UpdateChatStatus_Assign_ContactTagsProjection_AIDisabled", func(t *testing.T) {
		chat, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &connID, contact.ID)
		if err != nil {
			t.Fatalf("failed to get chat: %v", err)
		}

		// Status
		if err := chatRepo.UpdateChatStatus(ctx, ws.ID, chat.ID, string(domain.ChatStatusClosed)); err != nil {
			t.Fatalf("failed to update status: %v", err)
		}
		// Assign
		userUUID := uuid.New()
		email := "agent@example.com"
		if err := chatRepo.AssignChat(ctx, ws.ID, chat.ID, &userUUID, &email); err != nil {
			t.Fatalf("failed to assign: %v", err)
		}
		// Tags belong strictly to Contact (tags + contact_tags) and project onto Chat reads
		tagRepo := repository.NewTagRepository(pool)
		if _, err := tagRepo.AddTagsByName(ctx, ws.ID, contact.ID, []string{"vip", "support"}); err != nil {
			t.Fatalf("failed to add contact tags: %v", err)
		}

		updated, err := chatRepo.GetChat(ctx, ws.ID, chat.ID)
		if err != nil {
			t.Fatalf("failed to get updated chat: %v", err)
		}

		if updated.Status != domain.ChatStatusClosed {
			t.Errorf("expected status closed, got %s", updated.Status)
		}
		if updated.AssignedEmail == nil || *updated.AssignedEmail != email {
			t.Errorf("expected assigned email %s, got %v", email, updated.AssignedEmail)
		}
		if len(updated.Tags) != 2 || updated.Tags[0] != "support" || updated.Tags[1] != "vip" {
			t.Errorf("expected projected sorted tags [support vip], got %v", updated.Tags)
		}
		if updated.AIDisabled {
			t.Errorf("expected ai disabled false by default, got true")
		}

		// GetChatByContact also projects Contact's tags
		byContact, err := chatRepo.GetChatByContact(ctx, ws.ID, contact.ID)
		if err != nil {
			t.Fatalf("GetChatByContact failed: %v", err)
		}
		if len(byContact.Tags) != 2 || byContact.Tags[0] != "support" || byContact.Tags[1] != "vip" {
			t.Errorf("expected GetChatByContact tags [support vip], got %v", byContact.Tags)
		}

		// ListChats with case-insensitive filter.Tag matches via contact_tags + tags
		tagFiltered, err := chatRepo.ListChats(ctx, ws.ID, domain.ChatFilter{Tag: "VIP"}, 10, 0)
		if err != nil {
			t.Fatalf("ListChats(Tag=VIP) failed: %v", err)
		}
		if len(tagFiltered) != 1 || tagFiltered[0].ID != chat.ID || len(tagFiltered[0].Tags) != 2 {
			t.Errorf("expected 1 chat matching Tag=VIP with 2 tags, got %+v", tagFiltered)
		}
	})

	t.Run("ListChatMessages_CursorPagination", func(t *testing.T) {
		chat, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &connID, contact.ID)
		if err != nil {
			t.Fatalf("failed to get chat: %v", err)
		}

		baseTime := time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
		uids := []string{"msg-1", "msg-2", "msg-3", "msg-4", "msg-5"}

		for i, uid := range uids {
			if _, err := act.Inbound(ctx, ws.ID, activity.InboundTurn{
				Chat:       activity.ChatID(chat.ID).WithChannel("whatsapp_cloud"),
				UID:        uid,
				SenderName: "Alice",
				SenderID:   "+5511999999999",
				Body:       "Message " + uid,
				Channel:    "whatsapp_cloud",
				At:         baseTime.Add(time.Duration(i) * time.Minute),
			}); err != nil {
				t.Fatalf("failed to add message %s: %v", uid, err)
			}
		}

		// List all default
		allMsgs, err := chatRepo.ListChatMessages(ctx, ws.ID, chat.ID, "", "", 10)
		if err != nil {
			t.Fatalf("failed to list messages: %v", err)
		}
		if len(allMsgs) != 5 {
			t.Fatalf("expected 5 messages, got %d", len(allMsgs))
		}
		// Chronological check
		if allMsgs[0].UID != "msg-1" || allMsgs[4].UID != "msg-5" {
			t.Errorf("expected chronological order from msg-1 to msg-5, got %s -> %s", allMsgs[0].UID, allMsgs[4].UID)
		}

		// Cursor: before msg-4 (should return msg-1, msg-2, msg-3)
		beforeMsgs, err := chatRepo.ListChatMessages(ctx, ws.ID, chat.ID, "msg-4", "", 10)
		if err != nil {
			t.Fatalf("failed to list before msg-4: %v", err)
		}
		if len(beforeMsgs) != 3 {
			t.Fatalf("expected 3 messages before msg-4, got %d", len(beforeMsgs))
		}
		if beforeMsgs[0].UID != "msg-1" || beforeMsgs[2].UID != "msg-3" {
			t.Errorf("expected [msg-1, msg-2, msg-3], got %s -> %s", beforeMsgs[0].UID, beforeMsgs[2].UID)
		}

		// Cursor: after msg-2 (should return msg-3, msg-4, msg-5)
		afterMsgs, err := chatRepo.ListChatMessages(ctx, ws.ID, chat.ID, "", "msg-2", 10)
		if err != nil {
			t.Fatalf("failed to list after msg-2: %v", err)
		}
		if len(afterMsgs) != 3 {
			t.Fatalf("expected 3 messages after msg-2, got %d", len(afterMsgs))
		}
		if afterMsgs[0].UID != "msg-3" || afterMsgs[2].UID != "msg-5" {
			t.Errorf("expected [msg-3, msg-4, msg-5], got %s -> %s", afterMsgs[0].UID, afterMsgs[2].UID)
		}
	})

	t.Run("MessageReactions", func(t *testing.T) {
		chat, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &connID, contact.ID)
		if err != nil {
			t.Fatalf("failed to get chat: %v", err)
		}

		msgUID := "msg-rx-test"
		if _, err := act.Inbound(ctx, ws.ID, activity.InboundTurn{
			Chat:     activity.ChatID(chat.ID).WithChannel("whatsapp_cloud"),
			UID:      msgUID,
			SenderID: "+5511999999999",
			Body:     "Reactions testing",
			Channel:  "whatsapp_cloud",
			At:       time.Now().UTC(),
		}); err != nil {
			t.Fatalf("failed to add message: %v", err)
		}

		reactions := []domain.Reaction{
			{Emoji: "❤️", Sender: "agent1", CreatedAt: time.Now().UTC()},
			{Emoji: "🔥", Sender: "agent2", CreatedAt: time.Now().UTC()},
		}
		if err := chatRepo.UpdateMessageReactions(ctx, ws.ID, msgUID, reactions); err != nil {
			t.Fatalf("failed to update reactions: %v", err)
		}

		fetchedMsg, err := chatRepo.GetChatMessageByUID(ctx, ws.ID, msgUID)
		if err != nil {
			t.Fatalf("failed to get message by UID: %v", err)
		}
		if len(fetchedMsg.Reactions) != 2 || fetchedMsg.Reactions[0].Emoji != "❤️" {
			t.Errorf("expected reactions [❤️, 🔥], got %+v", fetchedMsg.Reactions)
		}

		// Test AddReaction idempotency
		rxList, err := chatRepo.AddReaction(ctx, ws.ID, msgUID, domain.Reaction{
			Emoji:  "👍",
			Sender: "user_a",
		})
		if err != nil {
			t.Fatalf("failed to add reaction: %v", err)
		}
		if len(rxList) != 3 {
			t.Fatalf("expected 3 reactions, got %d", len(rxList))
		}

		// Duplicate add should be idempotent
		rxListDup, err := chatRepo.AddReaction(ctx, ws.ID, msgUID, domain.Reaction{
			Emoji:  "👍",
			Sender: "user_a",
		})
		if err != nil {
			t.Fatalf("failed to add duplicate reaction: %v", err)
		}
		if len(rxListDup) != 3 {
			t.Fatalf("expected 3 reactions after duplicate add, got %d", len(rxListDup))
		}

		// Remove reaction
		rxListRem, err := chatRepo.RemoveReaction(ctx, ws.ID, msgUID, "👍", "user_a")
		if err != nil {
			t.Fatalf("failed to remove reaction: %v", err)
		}
		if len(rxListRem) != 2 {
			t.Fatalf("expected 2 reactions after remove, got %d", len(rxListRem))
		}
		for _, r := range rxListRem {
			if r.Emoji == "👍" && r.Sender == "user_a" {
				t.Errorf("expected reaction 👍 from user_a to be removed")
			}
		}

		// Test MutateReaction: add, toggle-remove, toggle-add, empty-emoji remove
		retChatID, resolvedAction, rxMut, err := chatRepo.MutateReaction(ctx, ws.ID, msgUID, "🎉", "user_b", "add")
		if err != nil {
			t.Fatalf("MutateReaction add failed: %v", err)
		}
		if retChatID != chat.ID {
			t.Errorf("expected chatID %s, got %s", chat.ID, retChatID)
		}
		if resolvedAction != "add" {
			t.Errorf("expected resolvedAction 'add', got %q", resolvedAction)
		}
		if len(rxMut) != 3 {
			t.Fatalf("expected 3 reactions after MutateReaction add, got %d", len(rxMut))
		}

		// Toggle existing reaction -> should resolve to "remove"
		_, resolvedAction, rxMut, err = chatRepo.MutateReaction(ctx, ws.ID, msgUID, "🎉", "user_b", "toggle")
		if err != nil {
			t.Fatalf("MutateReaction toggle (remove) failed: %v", err)
		}
		if resolvedAction != "remove" {
			t.Errorf("expected resolvedAction 'remove' on toggle of existing reaction, got %q", resolvedAction)
		}
		if len(rxMut) != 2 {
			t.Fatalf("expected 2 reactions after toggle remove, got %d", len(rxMut))
		}

		// Toggle missing reaction -> should resolve to "add"
		_, resolvedAction, rxMut, err = chatRepo.MutateReaction(ctx, ws.ID, msgUID, "🎉", "user_b", "toggle")
		if err != nil {
			t.Fatalf("MutateReaction toggle (add) failed: %v", err)
		}
		if resolvedAction != "add" {
			t.Errorf("expected resolvedAction 'add' on toggle of missing reaction, got %q", resolvedAction)
		}
		if len(rxMut) != 3 {
			t.Fatalf("expected 3 reactions after toggle add, got %d", len(rxMut))
		}

		// Empty emoji inbound removal -> clears user_b's reaction
		_, resolvedAction, rxMut, err = chatRepo.MutateReaction(ctx, ws.ID, msgUID, "", "user_b", "add")
		if err != nil {
			t.Fatalf("MutateReaction empty-emoji remove failed: %v", err)
		}
		if resolvedAction != "remove" {
			t.Errorf("expected resolvedAction 'remove' on empty emoji, got %q", resolvedAction)
		}
		if len(rxMut) != 2 {
			t.Fatalf("expected 2 reactions after empty emoji removal, got %d", len(rxMut))
		}

		// Invalid action returns error
		if _, _, _, err := chatRepo.MutateReaction(ctx, ws.ID, msgUID, "🎉", "user_b", "invalid"); err == nil {
			t.Errorf("expected error for invalid reaction action")
		}
	})

	t.Run("MutateReaction_ConcurrentToggleSerialization", func(t *testing.T) {
		chat, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &connID, contact.ID)
		if err != nil {
			t.Fatalf("failed to get chat: %v", err)
		}

		msgUID := "msg-rx-concurrent-" + uuid.New().String()
		if _, err := act.Inbound(ctx, ws.ID, activity.InboundTurn{
			Chat:     activity.ChatID(chat.ID).WithChannel("whatsapp_cloud"),
			UID:      msgUID,
			SenderID: "+5511999999999",
			Body:     "Concurrent toggle target",
			Channel:  "whatsapp_cloud",
			At:       time.Now().UTC(),
		}); err != nil {
			t.Fatalf("failed to add message: %v", err)
		}

		// Run 11 concurrent toggles on the same (messageUID, emoji, sender).
		// Under SELECT ... FOR UPDATE serialization, 6 must resolve to "add" and 5 to "remove",
		// leaving the reaction present at the end.
		const workers = 11
		type result struct {
			action string
			err    error
		}
		results := make(chan result, workers)
		start := make(chan struct{})

		for i := 0; i < workers; i++ {
			go func() {
				<-start
				_, act, _, mErr := chatRepo.MutateReaction(ctx, ws.ID, msgUID, "🚀", "operator", "toggle")
				results <- result{action: act, err: mErr}
			}()
		}
		close(start)

		adds, removes := 0, 0
		for i := 0; i < workers; i++ {
			r := <-results
			if r.err != nil {
				t.Fatalf("concurrent MutateReaction failed: %v", r.err)
			}
			switch r.action {
			case "add":
				adds++
			case "remove":
				removes++
			default:
				t.Fatalf("unexpected resolved action: %q", r.action)
			}
		}

		if adds != 6 || removes != 5 {
			t.Errorf("expected 6 adds and 5 removes under serialized row lock, got %d adds and %d removes", adds, removes)
		}

		finalReactions, err := chatRepo.GetReactions(ctx, ws.ID, msgUID)
		if err != nil {
			t.Fatalf("GetReactions failed: %v", err)
		}
		if len(finalReactions) != 1 || finalReactions[0].Emoji != "🚀" {
			t.Errorf("expected 1 final reaction 🚀, got %+v", finalReactions)
		}
	})

	t.Run("ListChats_Filters", func(t *testing.T) {
		unreadTrue := true
		unreadFalse := false

		// Filter unread
		unreadChats, err := chatRepo.ListChats(ctx, ws.ID, domain.ChatFilter{Unread: &unreadTrue}, 10, 0)
		if err != nil {
			t.Fatalf("failed to list unread chats: %v", err)
		}
		for _, c := range unreadChats {
			if c.UnreadCount <= 0 {
				t.Errorf("expected unread count > 0, got %d", c.UnreadCount)
			}
		}

		// Filter read
		readChats, err := chatRepo.ListChats(ctx, ws.ID, domain.ChatFilter{Unread: &unreadFalse}, 10, 0)
		if err != nil {
			t.Fatalf("failed to list read chats: %v", err)
		}
		for _, c := range readChats {
			if c.UnreadCount != 0 {
				t.Errorf("expected unread count == 0, got %d", c.UnreadCount)
			}
		}

		// Filter by phone
		phoneChats, err := chatRepo.ListChats(ctx, ws.ID, domain.ChatFilter{Phone: "99999999"}, 10, 0)
		if err != nil {
			t.Fatalf("failed to list phone chats: %v", err)
		}
		if len(phoneChats) == 0 {
			t.Errorf("expected at least 1 chat for phone filter, got 0")
		}
	})

	t.Run("CreateInternalNote_And_GetChatByID", func(t *testing.T) {
		chat, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &connID, contact.ID)
		if err != nil {
			t.Fatalf("failed to find or create chat: %v", err)
		}

		// 1. Test GetChatByID
		foundChat, err := chatRepo.GetChatByID(ctx, chat.ID)
		if err != nil {
			t.Fatalf("failed to get chat by id: %v", err)
		}
		if foundChat.ID != chat.ID || foundChat.WorkspaceID != ws.ID {
			t.Errorf("expected chat %s in ws %s, got %s in ws %s", chat.ID, ws.ID, foundChat.ID, foundChat.WorkspaceID)
		}

		// 2. Test CreateInternalNote
		noteBody := "This is a private AI draft note for human review."
		authorName := "Claude AI"
		note, err := chatRepo.CreateInternalNote(ctx, ws.ID, chat.ID, authorName, "ai_agent", noteBody)
		if err != nil {
			t.Fatalf("failed to create internal note: %v", err)
		}

		if note.Direction != domain.DirectionInternalNote {
			t.Errorf("expected direction 'internal_note', got %s", note.Direction)
		}
		if !note.IsPrivate {
			t.Errorf("expected is_private to be true")
		}
		if note.Body != noteBody {
			t.Errorf("expected body %q, got %q", noteBody, note.Body)
		}
		if note.SenderName != authorName {
			t.Errorf("expected sender_name %q, got %q", authorName, note.SenderName)
		}

		// 3. Verify it is fetched in chat messages
		msgs, err := chatRepo.ListChatMessages(ctx, ws.ID, chat.ID, "", "", 50)
		if err != nil {
			t.Fatalf("failed to list chat messages: %v", err)
		}
		found := false
		for _, m := range msgs {
			if m.UID == note.UID {
				found = true
				if m.Direction != "internal_note" || !m.IsPrivate {
					t.Errorf("expected message in list to have direction 'internal_note' and is_private=true")
				}
				break
			}
		}
		if !found {
			t.Errorf("internal note UID %s not found in ListChatMessages", note.UID)
		}
	})
}

func TestChatRepository_ConversationsAndThread(t *testing.T) {
	pool := getTestPoolWithMigrations(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	chatRepo := repository.NewChatRepository(pool)
	contactRepo := repository.NewContactRepository(pool)

	ws, err := wsRepo.Create(ctx, "chat_conv_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	identity1 := "+5511999990001"
	identity2 := "+5511999990002"
	identity3 := "+5511999990003"

	c1, err := contactRepo.ResolveContact(ctx, ws.ID, "telegram", "contact1", "Contact One", "", "")
	if err != nil {
		t.Fatalf("failed to resolve contact1: %v", err)
	}

	c2, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp", "contact2", "Contact Two", "", "")
	if err != nil {
		t.Fatalf("failed to resolve contact2: %v", err)
	}

	chat1, err := chatRepo.FindOrCreateChat(ctx, ws.ID, nil, c1.ID)
	if err != nil {
		t.Fatalf("failed to create chat1: %v", err)
	}

	chat2, err := chatRepo.FindOrCreateChat(ctx, ws.ID, nil, c2.ID)
	if err != nil {
		t.Fatalf("failed to create chat2: %v", err)
	}

	act := activity.New(pool, nil, nil, nil)

	insertInbound := func(chatID uuid.UUID, from, channel, to, body string, createdAt time.Time) *domain.ChatMessage {
		res, err := act.Inbound(ctx, ws.ID, activity.InboundTurn{
			Chat:       activity.ChatID(chatID).WithChannel(channel),
			UID:        uuid.NewString(),
			Channel:    channel,
			SenderName: from,
			SenderID:   from,
			Body:       body,
			Metadata: map[string]interface{}{
				"channel": channel,
				"to":      to,
			},
			At: createdAt,
		})
		if err != nil {
			t.Fatalf("failed to insert inbound chat message: %v", err)
		}
		return res.Message
	}

	insertOutbound := func(chatID uuid.UUID, to, channel, senderIdentity, body string, createdAt time.Time) *domain.ChatMessage {
		res, err := act.Outbound(ctx, ws.ID, activity.OutboundTurn{
			Chat:       activity.ChatID(chatID).WithChannel(channel),
			UID:        uuid.NewString(),
			Channel:    channel,
			To:         to,
			SenderType: domain.SenderTypeHumanAgent,
			SenderName: "Agent",
			SenderID:   senderIdentity,
			Body:       body,
			Metadata: map[string]string{
				"channel":         channel,
				"sender_identity": senderIdentity,
				"to":              to,
			},
			At: createdAt,
		})
		if err != nil {
			t.Fatalf("failed to insert outbound chat message: %v", err)
		}
		return res.Message
	}

	now := time.Now().UTC()

	m1 := insertInbound(chat1.ID, "contact1", "telegram", identity1, "Hello 1", now.Add(-10*time.Minute))
	insertInbound(chat1.ID, "contact1", "telegram", identity1, "Hello 2", now.Add(-8*time.Minute))
	insertOutbound(chat1.ID, "contact1", "telegram", identity1, "Reply 1", now.Add(-5*time.Minute))
	insertInbound(chat1.ID, "contact1", "telegram", identity2, "Hello bot 2", now.Add(-2*time.Minute))
	insertInbound(chat2.ID, "contact2", "whatsapp", identity3, "Hello from Contact Two", now.Add(-1*time.Minute))
	if err := chatRepo.ResetUnread(ctx, ws.ID, chat2.ID); err != nil {
		t.Fatalf("failed to reset unread on chat2: %v", err)
	}

	t.Run("ListConversations grouping, unread state, and filters", func(t *testing.T) {
		conversations, err := chatRepo.ListConversations(ctx, ws.ID, "")
		if err != nil {
			t.Fatalf("ListConversations failed: %v", err)
		}

		if len(conversations) != 2 {
			t.Fatalf("expected 2 conversations, got %d", len(conversations))
		}

		if conversations[0].ContactID != c2.ID {
			t.Errorf("expected first conversation to be contact2, got: %s", conversations[0].ContactID)
		}
		if conversations[0].ContactName != "Contact Two" {
			t.Errorf("expected name Contact Two, got: %s", conversations[0].ContactName)
		}
		if conversations[0].LastMessageBody != "Hello from Contact Two" {
			t.Errorf("expected last body 'Hello from Contact Two', got: %s", conversations[0].LastMessageBody)
		}
		if conversations[0].TotalMessageCount != 1 {
			t.Errorf("expected count 1, got: %d", conversations[0].TotalMessageCount)
		}
		if conversations[0].Unread || conversations[0].UnreadCount != 0 {
			t.Errorf("expected contact2 unread=false (0), got unread=%v count=%d", conversations[0].Unread, conversations[0].UnreadCount)
		}

		if conversations[1].ContactID != c1.ID {
			t.Errorf("expected second conversation to be contact1, got: %s", conversations[1].ContactID)
		}
		if conversations[1].ContactName != "Contact One" {
			t.Errorf("expected name Contact One, got: %s", conversations[1].ContactName)
		}
		if conversations[1].LastMessageBody != "Hello bot 2" {
			t.Errorf("expected last body 'Hello bot 2', got: %s", conversations[1].LastMessageBody)
		}
		if conversations[1].TotalMessageCount != 4 {
			t.Errorf("expected count 4 (3 inbound + 1 outbound), got: %d", conversations[1].TotalMessageCount)
		}
		if !conversations[1].Unread || conversations[1].UnreadCount != 3 {
			t.Errorf("expected contact1 unread=true (3), got unread=%v count=%d", conversations[1].Unread, conversations[1].UnreadCount)
		}

		// Reset unread by chat and verify
		if err := chatRepo.ResetUnread(ctx, ws.ID, chat1.ID); err != nil {
			t.Fatalf("ResetUnread failed: %v", err)
		}
		conversationsAfter, err := chatRepo.ListConversations(ctx, ws.ID, "")
		if err != nil {
			t.Fatalf("ListConversations failed: %v", err)
		}
		if conversationsAfter[1].Unread || conversationsAfter[1].UnreadCount != 0 {
			t.Errorf("expected contact1 unread=false after ResetUnread, got unread=%v count=%d", conversationsAfter[1].Unread, conversationsAfter[1].UnreadCount)
		}

		// Derived snooze on read: expired snooze returns open on GetChat, ListChats, and ListConversations without mutating DB row
		past := time.Now().UTC().Add(-10 * time.Minute)
		if err := chatRepo.SnoozeChat(ctx, ws.ID, chat1.ID, &past); err != nil {
			t.Fatalf("SnoozeChat failed: %v", err)
		}
		gotChat1, err := chatRepo.GetChat(ctx, ws.ID, chat1.ID)
		if err != nil {
			t.Fatalf("GetChat failed: %v", err)
		}
		if gotChat1.Status != domain.ChatStatusOpen || gotChat1.SnoozedUntil != nil {
			t.Errorf("expected derived open status and nil SnoozedUntil from GetChat, got status=%s snoozedUntil=%v", gotChat1.Status, gotChat1.SnoozedUntil)
		}
		var rawStatus string
		if err := pool.QueryRow(ctx, "SELECT status FROM chats WHERE id = $1", chat1.ID).Scan(&rawStatus); err != nil {
			t.Fatalf("query raw chat status failed: %v", err)
		}
		if rawStatus != "snoozed" {
			t.Errorf("expected stored DB status to remain 'snoozed' (no read-time write), got %q", rawStatus)
		}
	})

	t.Run("ListThreadByContact chronological order, cursor, and isolation", func(t *testing.T) {
		thread, err := chatRepo.ListThreadByContact(ctx, ws.ID, c1.ID, nil)
		if err != nil {
			t.Fatalf("ListThreadByContact failed: %v", err)
		}

		if len(thread) != 4 {
			t.Fatalf("expected 4 thread messages, got %d", len(thread))
		}

		if thread[0].Body != "Hello 1" || thread[0].Direction != "inbound" {
			t.Errorf("expected first msg to be inbound 'Hello 1', got: %s (%s)", thread[0].Body, thread[0].Direction)
		}
		if thread[1].Body != "Hello 2" || thread[1].Direction != "inbound" {
			t.Errorf("expected second msg to be inbound 'Hello 2', got: %s (%s)", thread[1].Body, thread[1].Direction)
		}
		if thread[2].Body != "Reply 1" || thread[2].Direction != "outbound" {
			t.Errorf("expected third msg to be outbound 'Reply 1', got: %s (%s)", thread[2].Body, thread[2].Direction)
		}
		if thread[3].Body != "Hello bot 2" || thread[3].Direction != "inbound" {
			t.Errorf("expected fourth msg to be inbound 'Hello bot 2', got: %s (%s)", thread[3].Body, thread[3].Direction)
		}

		// Test afterID cursor (after m1 -> should return 3 messages)
		afterThread, err := chatRepo.ListThreadByContact(ctx, ws.ID, c1.ID, &m1.ID)
		if err != nil {
			t.Fatalf("ListThreadByContact with afterID failed: %v", err)
		}
		if len(afterThread) != 3 {
			t.Fatalf("expected 3 messages after m1, got %d", len(afterThread))
		}
		if afterThread[0].Body != "Hello 2" {
			t.Errorf("expected first msg after m1 to be 'Hello 2', got %s", afterThread[0].Body)
		}

		thread2, err := chatRepo.ListThreadByContact(ctx, ws.ID, c2.ID, nil)
		if err != nil {
			t.Fatalf("ListThreadByContact failed: %v", err)
		}
		if len(thread2) != 1 {
			t.Fatalf("expected 1 thread message, got %d", len(thread2))
		}
		if thread2[0].Body != "Hello from Contact Two" {
			t.Errorf("expected body 'Hello from Contact Two', got: %s", thread2[0].Body)
		}
	})

	t.Run("ListThreadByContact includes internal notes from chat_messages", func(t *testing.T) {
		noteBody := "Private teammate note about VIP status."
		author := "Agent Mulder"
		note, err := chatRepo.CreateInternalNote(ctx, ws.ID, chat1.ID, author, "human", noteBody)
		if err != nil {
			t.Fatalf("failed to create internal note: %v", err)
		}

		thread, err := chatRepo.ListThreadByContact(ctx, ws.ID, c1.ID, nil)
		if err != nil {
			t.Fatalf("ListThreadByContact failed: %v", err)
		}

		var foundNote *repository.ThreadMessage
		for _, m := range thread {
			if m.ID == note.ID {
				foundNote = &m
				break
			}
		}

		if foundNote == nil {
			t.Fatalf("expected internal note %s in thread, not found", note.ID)
		}
		if foundNote.Direction != "internal_note" {
			t.Errorf("expected direction 'internal_note', got %s", foundNote.Direction)
		}
		if foundNote.Body != noteBody {
			t.Errorf("expected body %q, got %q", noteBody, foundNote.Body)
		}
		if foundNote.Metadata["author_name"] != author {
			t.Errorf("expected author_name %q, got %q", author, foundNote.Metadata["author_name"])
		}
		if foundNote.Metadata["is_private"] != "true" {
			t.Errorf("expected is_private 'true', got %q", foundNote.Metadata["is_private"])
		}
	})

	t.Run("ListThreadByContact returns dispatch status for outbound messages", func(t *testing.T) {
		dispatchRepo := repository.NewMessageDispatchRepository(pool)
		outboundTraceID := uuid.New().String()

		d, err := dispatchRepo.GetOrCreateDispatch(ctx, ws.ID, outboundTraceID, "telegram", nil, nil, nil)
		if err != nil {
			t.Fatalf("failed to create dispatch: %v", err)
		}

		err = dispatchRepo.UpdateDispatchStatus(ctx, d.ID, "read", "telegram", 0, nil)
		if err != nil {
			t.Fatalf("failed to update dispatch status: %v", err)
		}

		_, err = act.Outbound(ctx, ws.ID, activity.OutboundTurn{
			Chat:       activity.ChatID(chat1.ID).WithChannel("telegram"),
			Channel:    "telegram",
			SenderType: domain.SenderTypeHumanAgent,
			SenderName: "Agent",
			SenderID:   identity1,
			UID:        outboundTraceID,
			Body:       "Status Checked Reply",
			Metadata: map[string]string{
				"channel":         "telegram",
				"sender_identity": identity1,
			},
			At: now.Add(-3 * time.Minute),
		})
		if err != nil {
			t.Fatalf("failed to insert outbound chat message: %v", err)
		}

		thread, err := chatRepo.ListThreadByContact(ctx, ws.ID, c1.ID, nil)
		if err != nil {
			t.Fatalf("ListThreadByContact failed: %v", err)
		}

		var found bool
		for _, m := range thread {
			if m.TraceID == outboundTraceID {
				found = true
				if m.Direction != "outbound" {
					t.Errorf("expected direction 'outbound', got %q", m.Direction)
				}
				if m.Status == nil {
					t.Errorf("expected Status not to be nil")
				} else if *m.Status != "read" {
					t.Errorf("expected Status 'read', got %q", *m.Status)
				}
			} else if m.Direction == "inbound" {
				if m.Status != nil {
					t.Errorf("expected Status to be nil for inbound message, got %q", *m.Status)
				}
			}
		}
		if !found {
			t.Errorf("did not find outbound status checked message in thread")
		}
	})
}

func TestChatRepository_OutboundMessagesInConversationsList(t *testing.T) {
	pool := getTestPoolWithMigrations(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	chatRepo := repository.NewChatRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	act := activity.New(pool, nil, nil, nil)

	ws, err := wsRepo.Create(ctx, "outbound_inbox_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	senderIdentity1 := "+5511999990001"
	senderIdentity2 := "+5511999990002"

	now := time.Now().UTC()

	contactA, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp", "+5511888880001", "Contact A OutboundOnly", "", "")
	if err != nil {
		t.Fatalf("failed to resolve contact A: %v", err)
	}
	chatA, err := chatRepo.FindOrCreateChat(ctx, ws.ID, nil, contactA.ID)
	if err != nil {
		t.Fatalf("failed to create chat A: %v", err)
	}
	if _, err := act.Outbound(ctx, ws.ID, activity.OutboundTurn{
		Chat:       activity.ChatID(chatA.ID).WithChannel("whatsapp"),
		UID:        uuid.NewString(),
		Channel:    "whatsapp",
		SenderType: domain.SenderTypeHumanAgent,
		SenderName: "Agent",
		SenderID:   senderIdentity1,
		Body:       "Test outgoing msg A",
		Metadata: map[string]string{
			"channel":         "whatsapp",
			"sender_identity": senderIdentity1,
		},
		At: now.Add(-5 * time.Minute),
	}); err != nil {
		t.Fatalf("failed to add outbound message A: %v", err)
	}

	contactB, err := contactRepo.ResolveContact(ctx, ws.ID, "telegram", "contact_b_tg", "Contact B Mixed", "", "")
	if err != nil {
		t.Fatalf("failed to resolve contact B: %v", err)
	}
	chatB, err := chatRepo.FindOrCreateChat(ctx, ws.ID, nil, contactB.ID)
	if err != nil {
		t.Fatalf("failed to create chat B: %v", err)
	}
	if _, err := act.Inbound(ctx, ws.ID, activity.InboundTurn{
		Chat:       activity.ChatID(chatB.ID).WithChannel("telegram"),
		UID:        uuid.NewString(),
		Channel:    "telegram",
		SenderName: "Contact B Mixed",
		SenderID:   "contact_b_tg",
		Body:       "Inbound msg B",
		Metadata: map[string]interface{}{
			"channel": "telegram",
			"to":      senderIdentity2,
		},
		At: now.Add(-10 * time.Minute),
	}); err != nil {
		t.Fatalf("failed to add inbound message B: %v", err)
	}
	if _, err := act.Outbound(ctx, ws.ID, activity.OutboundTurn{
		Chat:       activity.ChatID(chatB.ID).WithChannel("telegram"),
		UID:        uuid.NewString(),
		Channel:    "telegram",
		SenderType: domain.SenderTypeHumanAgent,
		SenderName: "Agent",
		SenderID:   senderIdentity2,
		Body:       "Latest Outbound reply B",
		Metadata: map[string]string{
			"channel":         "telegram",
			"sender_identity": senderIdentity2,
		},
		At: now.Add(-2 * time.Minute),
	}); err != nil {
		t.Fatalf("failed to add outbound message B: %v", err)
	}

	t.Run("ListConversations includes outbound-only and outbound-latest conversations", func(t *testing.T) {
		conversations, err := chatRepo.ListConversations(ctx, ws.ID, "")
		if err != nil {
			t.Fatalf("ListConversations failed: %v", err)
		}

		if len(conversations) != 2 {
			t.Fatalf("expected 2 conversations (including outbound-only), got %d", len(conversations))
		}

		if conversations[0].ContactID != contactB.ID {
			t.Errorf("expected first conversation to be Contact B, got %s", conversations[0].ContactID)
		}
		if conversations[0].LastMessageBody != "Latest Outbound reply B" {
			t.Errorf("expected last message body to be latest outbound 'Latest Outbound reply B', got %q", conversations[0].LastMessageBody)
		}
		if conversations[0].TotalMessageCount != 2 {
			t.Errorf("expected total message count 2 (1 inbound + 1 outbound), got %d", conversations[0].TotalMessageCount)
		}

		if conversations[1].ContactID != contactA.ID {
			t.Errorf("expected second conversation to be Contact A, got %s", conversations[1].ContactID)
		}
		if conversations[1].LastMessageBody != "Test outgoing msg A" {
			t.Errorf("expected last message body 'Test outgoing msg A', got %q", conversations[1].LastMessageBody)
		}
		if conversations[1].TotalMessageCount != 1 {
			t.Errorf("expected total message count 1, got %d", conversations[1].TotalMessageCount)
		}
	})

	t.Run("ListConversations filters correctly by sender identity for outbound messages", func(t *testing.T) {
		convsSender1, err := chatRepo.ListConversations(ctx, ws.ID, senderIdentity1)
		if err != nil {
			t.Fatalf("ListConversations with filter senderIdentity1 failed: %v", err)
		}
		if len(convsSender1) != 1 {
			t.Fatalf("expected 1 conversation for senderIdentity1, got %d", len(convsSender1))
		}
		if convsSender1[0].ContactID != contactA.ID {
			t.Errorf("expected Contact A, got %s", convsSender1[0].ContactID)
		}

		convsSender2, err := chatRepo.ListConversations(ctx, ws.ID, senderIdentity2)
		if err != nil {
			t.Fatalf("ListConversations with filter senderIdentity2 failed: %v", err)
		}
		if len(convsSender2) != 1 {
			t.Fatalf("expected 1 conversation for senderIdentity2, got %d", len(convsSender2))
		}
		if convsSender2[0].ContactID != contactB.ID {
			t.Errorf("expected Contact B, got %s", convsSender2[0].ContactID)
		}
	})
}


