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

		if updated.Status != domain.ChatStatusClosed {
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
				Direction:   domain.DirectionInbound,
				SenderType:  domain.SenderTypeContact,
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
			Direction:   domain.DirectionInbound,
			SenderType:  domain.SenderTypeContact,
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
		msg := &domain.ChatMessage{
			ChatID:      chat.ID,
			WorkspaceID: ws.ID,
			UID:         msgUID,
			Direction:   domain.DirectionInbound,
			SenderType:  domain.SenderTypeContact,
			Body:        "Concurrent toggle target",
			CreatedAt:   time.Now().UTC(),
		}
		if err := chatRepo.AddChatMessage(ctx, msg); err != nil {
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

