package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
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
		if chat1.Status != string(domain.ChatStatusOpen) {
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

	t.Run("UpdateChatStatus_Assign_SetTags_AIDisabled", func(t *testing.T) {
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
		// Tags
		if err := chatRepo.SetChatTags(ctx, ws.ID, chat.ID, []string{"vip", "support"}); err != nil {
			t.Fatalf("failed to set tags: %v", err)
		}
		// AIDisabled
		if err := chatRepo.SetAIDisabled(ctx, ws.ID, chat.ID, true); err != nil {
			t.Fatalf("failed to set ai disabled: %v", err)
		}
		// UnreadCount
		if err := chatRepo.UpdateChatUnreadCount(ctx, ws.ID, chat.ID, 5); err != nil {
			t.Fatalf("failed to update unread: %v", err)
		}

		updated, err := chatRepo.GetChat(ctx, ws.ID, chat.ID)
		if err != nil {
			t.Fatalf("failed to get updated chat: %v", err)
		}

		if updated.Status != string(domain.ChatStatusClosed) {
			t.Errorf("expected status closed, got %s", updated.Status)
		}
		if updated.AssignedEmail == nil || *updated.AssignedEmail != email {
			t.Errorf("expected assigned email %s, got %v", email, updated.AssignedEmail)
		}
		if len(updated.Tags) != 2 || updated.Tags[0] != "vip" {
			t.Errorf("expected tags [vip support], got %v", updated.Tags)
		}
		if !updated.AIDisabled {
			t.Errorf("expected ai disabled true, got false")
		}
		if updated.UnreadCount != 5 {
			t.Errorf("expected unread count 5, got %d", updated.UnreadCount)
		}

		// Increment
		if err := chatRepo.IncrementUnreadCount(ctx, ws.ID, chat.ID); err != nil {
			t.Fatalf("failed to increment unread: %v", err)
		}
		afterInc, _ := chatRepo.GetChat(ctx, ws.ID, chat.ID)
		if afterInc.UnreadCount != 6 {
			t.Errorf("expected unread count 6, got %d", afterInc.UnreadCount)
		}
	})

	t.Run("AddChatMessage_And_ListChatMessages_CursorPagination", func(t *testing.T) {
		chat, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &connID, contact.ID)
		if err != nil {
			t.Fatalf("failed to get chat: %v", err)
		}

		baseTime := time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
		uids := []string{"msg-1", "msg-2", "msg-3", "msg-4", "msg-5"}

		for i, uid := range uids {
			msg := &domain.ChatMessage{
				ChatID:      chat.ID,
				WorkspaceID: ws.ID,
				UID:         uid,
				Direction:   string(domain.DirectionInbound),
				SenderType:  string(domain.SenderTypeContact),
				SenderName:  "Alice",
				SenderID:    "+5511999999999",
				Body:        "Message " + uid,
				CreatedAt:   baseTime.Add(time.Duration(i) * time.Minute),
			}
			if err := chatRepo.AddChatMessage(ctx, msg); err != nil {
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
		msg := &domain.ChatMessage{
			ChatID:      chat.ID,
			WorkspaceID: ws.ID,
			UID:         msgUID,
			Direction:   string(domain.DirectionInbound),
			SenderType:  string(domain.SenderTypeContact),
			Body:        "Reactions testing",
			CreatedAt:   time.Now().UTC(),
		}
		if err := chatRepo.AddChatMessage(ctx, msg); err != nil {
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
}
