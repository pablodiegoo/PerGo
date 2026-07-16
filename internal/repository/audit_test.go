package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAuditRepository_ConversationsAndThread(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := NewWorkspaceRepository(pool)
	auditRepo := NewAuditRepository(pool)
	contactRepo := NewContactRepository(pool)

	// Create test workspace
	ws, err := wsRepo.Create(ctx, "audit_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	// We will create two distinct recipient identities to test isolation/multi-instance filtering
	identity1 := "+5511999990001"
	identity2 := "+5511999990002"
	identity3 := "+5511999990003"

	// Resolve contacts in the DB first so the joins in ListConversations/ListThread work
	c1, err := contactRepo.ResolveContact(ctx, ws.ID, "telegram", "contact1", "Contact One", "", "")
	if err != nil {
		t.Fatalf("failed to resolve contact1: %v", err)
	}

	c2, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp", "contact2", "Contact Two", "", "")
	if err != nil {
		t.Fatalf("failed to resolve contact2: %v", err)
	}

	// Helper to insert an inbound message into audit_logs
	insertInbound := func(from, channel, to, body string, createdAt time.Time) {
		payload := map[string]any{
			"event":        "inbound_message",
			"trace_id":     uuid.New().String(),
			"message_id":   uuid.New().String(),
			"channel":      channel,
			"timestamp":    createdAt.Format(time.RFC3339),
			"workspace_id": ws.ID.String(),
			"from":         from,
			"to":           to,
			"body":         body,
		}
		payloadBytes, _ := json.Marshal(payload)
		_, err := pool.Exec(ctx, `
			INSERT INTO audit_logs (id, workspace_id, trace_id, event_type, payload, created_at)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, uuid.New(), ws.ID, payload["trace_id"], "inbound_message", payloadBytes, createdAt)
		if err != nil {
			t.Fatalf("failed to insert inbound log: %v", err)
		}
	}

	// Helper to insert an outbound message into audit_logs
	insertOutbound := func(to, channel, senderIdentity, body string, createdAt time.Time) {
		payload := map[string]any{
			"request": map[string]any{
				"to":              to,
				"channel":         channel,
				"sender_identity": senderIdentity,
				"body":            body,
			},
			"status": "sent",
		}
		payloadBytes, _ := json.Marshal(payload)
		_, err := pool.Exec(ctx, `
			INSERT INTO audit_logs (id, workspace_id, trace_id, event_type, payload, created_at)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, uuid.New(), ws.ID, uuid.New().String(), "outbound_message", payloadBytes, createdAt)
		if err != nil {
			t.Fatalf("failed to insert outbound log: %v", err)
		}
	}

	now := time.Now().UTC()

	// Inbound messages for contact1 on identity1
	insertInbound("contact1", "telegram", identity1, "Hello 1", now.Add(-10*time.Minute))
	insertInbound("contact1", "telegram", identity1, "Hello 2", now.Add(-8*time.Minute))

	// Outbound messages for contact1 on identity1
	insertOutbound("contact1", "telegram", identity1, "Reply 1", now.Add(-5*time.Minute))

	// Inbound message for contact1 on identity2 (different bot instance)
	insertInbound("contact1", "telegram", identity2, "Hello bot 2", now.Add(-2*time.Minute))

	// Inbound message for contact2
	insertInbound("contact2", "whatsapp", identity3, "Hello from Contact Two", now.Add(-1*time.Minute))

	// Test ListConversations
	t.Run("ListConversations grouping and filters", func(t *testing.T) {
		conversations, err := auditRepo.ListConversations(ctx, ws.ID, "")
		if err != nil {
			t.Fatalf("ListConversations failed: %v", err)
		}

		// We expect 2 separate conversations grouped by contact_id:
		// 1. contact2 (last body "Hello from Contact Two")
		// 2. contact1 (last body "Hello bot 2")
		if len(conversations) != 2 {
			t.Fatalf("expected 2 conversations, got %d", len(conversations))
		}

		// Since ordered by latest message created_at DESC:
		// First conversation should be contact2 (since -1 min is newer than -2 min)
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

		// Second conversation should be contact1
		if conversations[1].ContactID != c1.ID {
			t.Errorf("expected second conversation to be contact1, got: %s", conversations[1].ContactID)
		}
		if conversations[1].ContactName != "Contact One" {
			t.Errorf("expected name Contact One, got: %s", conversations[1].ContactName)
		}
		if conversations[1].LastMessageBody != "Hello bot 2" {
			t.Errorf("expected last body 'Hello bot 2', got: %s", conversations[1].LastMessageBody)
		}
		if conversations[1].TotalMessageCount != 3 {
			t.Errorf("expected count 3, got: %d", conversations[1].TotalMessageCount)
		}
	})

	// Test ListThread
	t.Run("ListThreadByContact union and isolation", func(t *testing.T) {
		// Thread for contact1 (mapped to c1.ID)
		thread, err := auditRepo.ListThreadByContact(ctx, ws.ID, c1.ID, nil)
		if err != nil {
			t.Fatalf("ListThreadByContact failed: %v", err)
		}

		// Expected 4 messages: Inbound "Hello 1", Inbound "Hello 2", Outbound "Reply 1", Inbound "Hello bot 2"
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

		// Thread for contact2 (mapped to c2.ID) should only contain "Hello from Contact Two"
		thread2, err := auditRepo.ListThreadByContact(ctx, ws.ID, c2.ID, nil)
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
}
