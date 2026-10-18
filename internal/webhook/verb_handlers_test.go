package webhook_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/chat/activity"
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

	t.Run("records ai_agent ChatMessage and enforces WABA 24h window via OutboundProcessor.Ingest", func(t *testing.T) {
		pool := getTestPoolWithMigrations(t)
		if pool == nil {
			t.Skip("Postgres not available")
			return
		}
		defer pool.Close()

		wsRepo := repository.NewWorkspaceRepository(pool)
		connRepo := repository.NewConnectionRepository(pool, nil)
		contactRepo := repository.NewContactRepository(pool)
		chatRepo := repository.NewChatRepository(pool)

		ws, err := wsRepo.Create(ctx, "verb_ingest_ws_"+uuid.New().String())
		if err != nil {
			t.Fatalf("create workspace: %v", err)
		}
		defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

		tgConn := &repository.Connection{
			ID:             uuid.New(),
			WorkspaceID:    ws.ID,
			Name:           "TG Verb Conn",
			Slug:           "tg-verb-" + uuid.New().String()[:8],
			Channel:        "telegram",
			SenderIdentity: "bot-verb",
			Status:         "active",
			IsDefault:      true,
		}
		if err := connRepo.Create(ctx, tgConn); err != nil {
			t.Fatalf("create tg connection: %v", err)
		}

		wabaConn := &repository.Connection{
			ID:             uuid.New(),
			WorkspaceID:    ws.ID,
			Name:           "WABA Verb Conn",
			Slug:           "waba-verb-" + uuid.New().String()[:8],
			Channel:        "whatsapp_cloud",
			SenderIdentity: "+5511888887777",
			Status:         "active",
			IsDefault:      true,
		}
		if err := connRepo.Create(ctx, wabaConn); err != nil {
			t.Fatalf("create waba connection: %v", err)
		}

		tgContact, err := contactRepo.ResolveContact(ctx, ws.ID, "telegram", "+5511900001111", "", "", "")
		if err != nil {
			t.Fatalf("resolve tg contact: %v", err)
		}

		pub := &mockPublisher{}
		engine := webhook.NewVerbsEngine(pub, contactRepo, nil, connRepo)

		// 1. Reply on telegram records ChatMessage with SenderTypeAIAgent and SenderName "Webhook Verb"
		evt := inbound.InboundEventPayload{
			Event:       "message.received",
			TraceID:     "trace-verb-ingest-1",
			MessageID:   "msg-verb-ingest-1",
			Channel:     "telegram",
			WorkspaceID: ws.ID.String(),
			From:        "+5511900001111",
			To:          "bot-verb",
			Body:        "User question",
		}
		evtBytes, _ := json.Marshal(evt)
		task := webhook.WebhookDeliveryTask{
			ID:          uuid.New(),
			WorkspaceID: ws.ID,
			TraceID:     "trace-verb-ingest-1",
			Payload:     evtBytes,
		}
		err = engine.Execute(ctx, task, []webhook.Verb{
			{Action: "reply", Params: json.RawMessage(`{"body":"Bot verb reply"}`)},
			{Action: "forward", Params: json.RawMessage(`{"to":"+5511900002222","channel":"telegram"}`)},
		})
		if err != nil {
			t.Fatalf("engine.Execute reply+forward failed: %v", err)
		}

		replyMsg, err := chatRepo.GetChatMessageByUID(ctx, ws.ID, "trace-verb-ingest-1")
		if err != nil || replyMsg == nil {
			t.Fatalf("expected ChatMessage for reply verb, got err=%v msg=%v", err, replyMsg)
		}
		if replyMsg.SenderType != domain.SenderTypeAIAgent {
			t.Errorf("expected SenderType %q, got %q", domain.SenderTypeAIAgent, replyMsg.SenderType)
		}
		if replyMsg.SenderName != "Webhook Verb" {
			t.Errorf("expected SenderName 'Webhook Verb', got %q", replyMsg.SenderName)
		}

		fwdMsg, err := chatRepo.GetChatMessageByUID(ctx, ws.ID, "trace-verb-ingest-1-1")
		if err != nil || fwdMsg == nil {
			t.Fatalf("expected ChatMessage for forward verb (UID trace-verb-ingest-1-1), got err=%v msg=%v", err, fwdMsg)
		}
		if fwdMsg.SenderType != domain.SenderTypeAIAgent {
			t.Errorf("expected forward SenderType %q, got %q", domain.SenderTypeAIAgent, fwdMsg.SenderType)
		}
		if fwdMsg.Body != "User question" {
			t.Errorf("expected forward Body 'User question', got %q", fwdMsg.Body)
		}
		_ = tgContact

		// 2. Reply on WABA without open 24h window is rejected with TemplateRequiredError
		wabaEvt := inbound.InboundEventPayload{
			Event:       "message.received",
			TraceID:     "trace-verb-waba-closed",
			MessageID:   "msg-verb-waba-closed",
			Channel:     "whatsapp_cloud",
			WorkspaceID: ws.ID.String(),
			From:        "+5511900003333",
			To:          "+5511888887777",
			Body:        "Late reply",
		}
		wabaEvtBytes, _ := json.Marshal(wabaEvt)
		wabaTask := webhook.WebhookDeliveryTask{
			ID:          uuid.New(),
			WorkspaceID: ws.ID,
			TraceID:     "trace-verb-waba-closed",
			Payload:     wabaEvtBytes,
		}
		err = engine.Execute(ctx, wabaTask, []webhook.Verb{
			{Action: "reply", Params: json.RawMessage(`{"body":"Should be blocked by 24h window"}`)},
		})
		if err == nil {
			t.Fatalf("expected WABA reply outside 24h window to fail, got nil")
		}
		var tmplErr *domain.TemplateRequiredError
		if !errors.As(err, &tmplErr) {
			t.Errorf("expected TemplateRequiredError, got %T: %v", err, err)
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

		updated, err := contactRepo.GetByID(ctx, ws.ID, c.ID)
		if err != nil {
			t.Fatalf("failed to load contact: %v", err)
		}

		if len(updated.Tags) != 2 || updated.Tags[0] != "another-tag" || updated.Tags[1] != "test-tag" {
			t.Errorf("unexpected tags: %v", updated.Tags)
		}
		tagRepo := repository.NewTagRepository(pool)
		catalog, err := tagRepo.ListTags(ctx, ws.ID)
		if err != nil || len(catalog) != 2 {
			t.Errorf("expected 2 tags in workspace catalog, got %v (err=%v)", catalog, err)
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

	t.Run("closes chat", func(t *testing.T) {
		vc := webhook.VerbContext{
			WorkspaceID: ws.ID,
			ContactID:   c.ID,
		}

		err := handler.Execute(ctx, vc, json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var status string
		err = pool.QueryRow(ctx, "SELECT status FROM chats WHERE workspace_id = $1 AND contact_id = $2", ws.ID, c.ID).Scan(&status)
		if err != nil {
			t.Fatalf("failed to query chat status: %v", err)
		}

		if status != "closed" {
			t.Errorf("expected chat status 'closed', got %q", status)
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

	rec := activity.New(pool, nil, nil, nil)
	handler := webhook.NewPauseBotHandlerWithActivity(rec)

	t.Run("pauses default 12h", func(t *testing.T) {
		vc := webhook.VerbContext{
			WorkspaceID: ws.ID,
			ContactID:   c.ID,
		}

		before := time.Now().UTC()
		err := handler.Execute(ctx, vc, json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		state, _, err := rec.Snapshot(ctx, ws.ID, activity.ChatOf(uuid.Nil, c.ID))
		if err != nil {
			t.Fatalf("failed to snapshot chat: %v", err)
		}

		if state.AI.Mode != domain.AIModePaused {
			t.Errorf("expected AI mode paused, got %q", state.AI.Mode)
		}
		expected := before.Add(12 * time.Hour)
		if diff := state.AI.PausedUntil.Sub(expected); diff < -10*time.Second || diff > 10*time.Second {
			t.Errorf("expected PausedUntil ≈ now+12h (%v), got %v", expected, state.AI.PausedUntil)
		}
	})

	t.Run("pauses with custom duration", func(t *testing.T) {
		vc := webhook.VerbContext{
			WorkspaceID: ws.ID,
			ContactID:   c.ID,
		}

		before := time.Now().UTC()
		err := handler.Execute(ctx, vc, json.RawMessage(`{"duration": "30m"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		state, _, err := rec.Snapshot(ctx, ws.ID, activity.ChatOf(uuid.Nil, c.ID))
		if err != nil {
			t.Fatalf("failed to snapshot chat: %v", err)
		}

		if state.AI.Mode != domain.AIModePaused {
			t.Errorf("expected AI mode paused, got %q", state.AI.Mode)
		}
		expected := before.Add(30 * time.Minute)
		if diff := state.AI.PausedUntil.Sub(expected); diff < -10*time.Second || diff > 10*time.Second {
			t.Errorf("expected PausedUntil ≈ now+30m (%v), got %v", expected, state.AI.PausedUntil)
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
