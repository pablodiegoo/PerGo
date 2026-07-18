package webhook_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/webhook"
)

func TestReplyHandler(t *testing.T) {
	ctx := context.Background()
	wsID := uuid.New()
	connID := uuid.New()

	t.Run("publishes to messages.outbound", func(t *testing.T) {
		pub := &mockPublisher{}
		res := &mockRouteResolver{
			conn: &repository.Connection{
				ID:             connID,
				SenderIdentity: "+5511999999999",
			},
		}

		handler := webhook.NewReplyHandler(pub, res)

		vc := webhook.VerbContext{
			WorkspaceID: wsID,
			TraceID:     "trace-reply",
			Event: inbound.InboundEventPayload{
				Channel: "telegram",
				From:    "+123456",
				To:      "+5511999999999",
			},
		}

		err := handler.Execute(ctx, vc, json.RawMessage(`{"body":"test reply"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(pub.published) != 1 {
			t.Fatalf("expected 1 published message, got %d", len(pub.published))
		}

		sent := pub.published[0]
		if sent.subject != "messages.outbound" {
			t.Errorf("expected subject 'messages.outbound', got %q", sent.subject)
		}
		if sent.traceID != "trace-reply" {
			t.Errorf("expected traceID 'trace-reply', got %q", sent.traceID)
		}

		var qMsg domain.QueueMessage
		if err := json.Unmarshal(sent.payload, &qMsg); err != nil {
			t.Fatalf("failed to unmarshal published payload: %v", err)
		}

		if qMsg.WorkspaceID != wsID {
			t.Errorf("expected workspace ID %s, got %s", wsID, qMsg.WorkspaceID)
		}
		if qMsg.ConnectionID != connID {
			t.Errorf("expected connection ID %s, got %s", connID, qMsg.ConnectionID)
		}
		if qMsg.Body != "test reply" {
			t.Errorf("expected body 'test reply', got %q", qMsg.Body)
		}
		if qMsg.To != "+123456" {
			t.Errorf("expected To '+123456', got %q", qMsg.To)
		}
	})

	t.Run("skips when publisher is nil", func(t *testing.T) {
		handler := webhook.NewReplyHandler(nil, nil)
		vc := webhook.VerbContext{}
		err := handler.Execute(ctx, vc, json.RawMessage(`{"body":"test reply"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("returns error on invalid params", func(t *testing.T) {
		handler := webhook.NewReplyHandler(&mockPublisher{}, &mockRouteResolver{})
		vc := webhook.VerbContext{}
		err := handler.Execute(ctx, vc, json.RawMessage(`{bad`))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestWaitHandler(t *testing.T) {
	ctx := context.Background()
	handler := webhook.NewWaitHandler()
	vc := webhook.VerbContext{}

	t.Run("waits for specified duration", func(t *testing.T) {
		start := time.Now()
		err := handler.Execute(ctx, vc, json.RawMessage(`{"duration": "50ms"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		elapsed := time.Since(start)
		if elapsed < 40*time.Millisecond {
			t.Errorf("expected wait of at least 40ms, took %v", elapsed)
		}
	})

	t.Run("caps at 10 seconds", func(t *testing.T) {
		ctxWithTimeout, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		start := time.Now()
		err := handler.Execute(ctxWithTimeout, vc, json.RawMessage(`{"duration": "30s"}`))
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("expected context.DeadlineExceeded, got %v", err)
		}
		elapsed := time.Since(start)
		if elapsed > 500*time.Millisecond {
			t.Errorf("expected context cancel to stop wait within 500ms, took %v", elapsed)
		}
	})

	t.Run("returns error on invalid duration", func(t *testing.T) {
		err := handler.Execute(ctx, vc, json.RawMessage(`{"duration": "invalid"}`))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestForwardHandler(t *testing.T) {
	ctx := context.Background()
	wsID := uuid.New()
	connID := uuid.New()

	t.Run("publishes forward message", func(t *testing.T) {
		pub := &mockPublisher{}
		res := &mockRouteResolver{
			conn: &repository.Connection{
				ID:             connID,
				SenderIdentity: "+5511999999999",
			},
		}

		handler := webhook.NewForwardHandler(pub, res)

		vc := webhook.VerbContext{
			WorkspaceID: wsID,
			TraceID:     "trace-forward",
			Event: inbound.InboundEventPayload{
				Body: "Original payload body",
			},
		}

		err := handler.Execute(ctx, vc, json.RawMessage(`{"to": "+5511999", "channel": "telegram"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(pub.published) != 1 {
			t.Fatalf("expected 1 published message, got %d", len(pub.published))
		}

		sent := pub.published[0]
		var qMsg domain.QueueMessage
		if err := json.Unmarshal(sent.payload, &qMsg); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if qMsg.To != "+5511999" {
			t.Errorf("expected To '+5511999', got %q", qMsg.To)
		}
		if qMsg.Channel != "telegram" {
			t.Errorf("expected Channel 'telegram', got %q", qMsg.Channel)
		}
		if qMsg.Body != "Original payload body" {
			t.Errorf("expected body 'Original payload body', got %q", qMsg.Body)
		}
	})

	t.Run("skips when publisher is nil", func(t *testing.T) {
		handler := webhook.NewForwardHandler(nil, nil)
		vc := webhook.VerbContext{}
		err := handler.Execute(ctx, vc, json.RawMessage(`{"to": "+5511999", "channel": "telegram"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestTagHandler(t *testing.T) {
	pool := getTestPoolWithMigrations(t)
	if pool == nil {
		t.Skip("Postgres not available")
		return
	}
	defer pool.Close()

	ctx := context.Background()
	contactRepo := repository.NewContactRepository(pool)
	wsRepo := repository.NewWorkspaceRepository(pool)

	ws, err := wsRepo.Create(ctx, "tag_handler_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer wsRepo.Delete(ctx, ws.ID)

	c, err := contactRepo.ResolveContact(ctx, ws.ID, "telegram", "tag-sender", "", "", "")
	if err != nil {
		t.Fatalf("failed to resolve contact: %v", err)
	}

	handler := webhook.NewTagHandler(contactRepo)

	t.Run("adds tags to contact", func(t *testing.T) {
		vc := webhook.VerbContext{
			WorkspaceID: ws.ID,
			ContactID:   c.ID,
		}

		err := handler.Execute(ctx, vc, json.RawMessage(`{"tags": ["test-tag", "another-tag"]}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var tags []string
		err = pool.QueryRow(ctx, "SELECT tags FROM contacts WHERE id = $1", c.ID).Scan(&tags)
		if err != nil {
			t.Fatalf("failed to query tags: %v", err)
		}

		if len(tags) != 2 || tags[0] != "test-tag" || tags[1] != "another-tag" {
			t.Errorf("unexpected tags: %v", tags)
		}
	})

	t.Run("fails when contact not resolved", func(t *testing.T) {
		vc := webhook.VerbContext{
			WorkspaceID: ws.ID,
			ContactID:   uuid.Nil,
		}
		err := handler.Execute(ctx, vc, json.RawMessage(`{"tags": ["test-tag"]}`))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestCloseHandler(t *testing.T) {
	pool := getTestPoolWithMigrations(t)
	if pool == nil {
		t.Skip("Postgres not available")
		return
	}
	defer pool.Close()

	ctx := context.Background()
	contactRepo := repository.NewContactRepository(pool)
	wsRepo := repository.NewWorkspaceRepository(pool)

	ws, err := wsRepo.Create(ctx, "close_handler_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer wsRepo.Delete(ctx, ws.ID)

	c, err := contactRepo.ResolveContact(ctx, ws.ID, "telegram", "close-sender", "", "", "")
	if err != nil {
		t.Fatalf("failed to resolve contact: %v", err)
	}

	handler := webhook.NewCloseHandler(contactRepo)

	t.Run("closes thread", func(t *testing.T) {
		vc := webhook.VerbContext{
			WorkspaceID: ws.ID,
			ContactID:   c.ID,
		}

		err := handler.Execute(ctx, vc, json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var closedAt *time.Time
		err = pool.QueryRow(ctx, "SELECT closed_at FROM contacts WHERE id = $1", c.ID).Scan(&closedAt)
		if err != nil {
			t.Fatalf("failed to query closed_at: %v", err)
		}

		if closedAt == nil {
			t.Error("expected closed_at to be set, got nil")
		}
	})

	t.Run("fails when contact not resolved", func(t *testing.T) {
		vc := webhook.VerbContext{
			WorkspaceID: ws.ID,
			ContactID:   uuid.Nil,
		}
		err := handler.Execute(ctx, vc, json.RawMessage(`{}`))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestPauseBotHandler(t *testing.T) {
	pool := getTestPoolWithMigrations(t)
	if pool == nil {
		t.Skip("Postgres not available")
		return
	}
	defer pool.Close()

	ctx := context.Background()
	contactRepo := repository.NewContactRepository(pool)
	wsRepo := repository.NewWorkspaceRepository(pool)

	ws, err := wsRepo.Create(ctx, "pause_handler_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer wsRepo.Delete(ctx, ws.ID)

	c, err := contactRepo.ResolveContact(ctx, ws.ID, "telegram", "pause-sender", "", "", "")
	if err != nil {
		t.Fatalf("failed to resolve contact: %v", err)
	}

	handler := webhook.NewPauseBotHandler(contactRepo)

	t.Run("pauses indefinitely", func(t *testing.T) {
		vc := webhook.VerbContext{
			WorkspaceID: ws.ID,
			ContactID:   c.ID,
		}

		err := handler.Execute(ctx, vc, json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var botActive bool
		var botPausedAt *time.Time
		err = pool.QueryRow(ctx, "SELECT bot_active, bot_paused_at FROM contacts WHERE id = $1", c.ID).Scan(&botActive, &botPausedAt)
		if err != nil {
			t.Fatalf("failed to query contact: %v", err)
		}

		if botActive {
			t.Error("expected bot_active to be false")
		}
		if botPausedAt == nil {
			t.Fatal("expected bot_paused_at to be set")
		}
		if time.Since(*botPausedAt) > 10*time.Second {
			t.Errorf("expected bot_paused_at to be set to now, got %v", botPausedAt)
		}
	})

	t.Run("pauses with duration offset", func(t *testing.T) {
		vc := webhook.VerbContext{
			WorkspaceID: ws.ID,
			ContactID:   c.ID,
		}

		err := handler.Execute(ctx, vc, json.RawMessage(`{"duration": "2h"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var botActive bool
		var botPausedAt *time.Time
		err = pool.QueryRow(ctx, "SELECT bot_active, bot_paused_at FROM contacts WHERE id = $1", c.ID).Scan(&botActive, &botPausedAt)
		if err != nil {
			t.Fatalf("failed to query contact: %v", err)
		}

		if botActive {
			t.Error("expected bot_active to be false")
		}
		if botPausedAt == nil {
			t.Fatal("expected bot_paused_at to be set")
		}

		elapsed := time.Since(*botPausedAt)
		if elapsed < 9*time.Hour || elapsed > 11*time.Hour {
			t.Errorf("expected bot_paused_at to be offset by ~10h from now, got %v", elapsed)
		}
	})

	t.Run("fails on invalid duration", func(t *testing.T) {
		vc := webhook.VerbContext{
			WorkspaceID: ws.ID,
			ContactID:   c.ID,
		}
		err := handler.Execute(ctx, vc, json.RawMessage(`{"duration": "bad"}`))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("fails when contact not resolved", func(t *testing.T) {
		vc := webhook.VerbContext{
			WorkspaceID: ws.ID,
			ContactID:   uuid.Nil,
		}
		err := handler.Execute(ctx, vc, json.RawMessage(`{}`))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
