package webhook_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pablojhp.pergo/internal/chat/activity"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/webhook"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func getTestPoolWithMigrations(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("PERGO_DATABASE_URL")
	if dsn == "" {
		bgCtx := context.Background()
		pgContainer, err := tcpostgres.Run(bgCtx,
			"postgres:16-alpine",
			tcpostgres.WithDatabase("pergo_test"),
			tcpostgres.WithUsername("postgres"),
			tcpostgres.WithPassword("postgres"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).
					WithStartupTimeout(30*time.Second),
			),
		)
		if err != nil {
			t.Fatalf("failed to start postgres container: %v", err)
		}
		t.Cleanup(func() {
			_ = pgContainer.Terminate(bgCtx)
		})
		connStr, err := pgContainer.ConnectionString(bgCtx, "sslmode=disable")
		if err != nil {
			t.Fatalf("failed to get connection string: %v", err)
		}
		dsn = connStr
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("PostgreSQL not available at %s: %v", dsn, err)
		return nil
	}

	err = pool.Ping(ctx)
	if err != nil {
		pool.Close()
		t.Skipf("PostgreSQL ping failed at %s: %v", dsn, err)
		return nil
	}

	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("failed to wrap pool: %v", err)
	}
	defer db.Close()

	if err := postgres.RunMigrations(db); err != nil {
		pool.Close()
		t.Fatalf("failed to run migrations: %v", err)
	}
	return pool
}

func TestVerbsEngine(t *testing.T) {
	pool := getTestPoolWithMigrations(t)
	if pool == nil {
		return
	}
	defer pool.Close()

	ctx := context.Background()

	// Clean up tables
	_, _ = pool.Exec(ctx, "DELETE FROM contact_identities")
	_, _ = pool.Exec(ctx, "DELETE FROM contacts")
	_, _ = pool.Exec(ctx, "DELETE FROM workspaces")

	contactRepo := repository.NewContactRepository(pool)
	wsRepo := repository.NewWorkspaceRepository(pool)

	ws, err := wsRepo.Create(ctx, "verbs_engine_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	engine := webhook.NewVerbsEngine(nil, contactRepo, nil, nil)

	t.Run("Normal sequential flow with tag and close", func(t *testing.T) {
		c, err := contactRepo.ResolveContact(ctx, ws.ID, "telegram", "sender-1", "", "", "")
		if err != nil {
			t.Fatalf("ResolveContact failed: %v", err)
		}

		evt := inbound.InboundEventPayload{
			Event:       "message.received",
			TraceID:     "trace-1",
			MessageID:   "msg-1",
			Channel:     "telegram",
			WorkspaceID: ws.ID.String(),
			From:        "sender-1",
			To:          "bot-1",
			Body:        "Hello bot",
		}
		evtBytes, _ := json.Marshal(evt)

		task := webhook.WebhookDeliveryTask{
			ID:             uuid.New(),
			SubscriptionID: uuid.New(),
			WorkspaceID:    ws.ID,
			Event:          "message.received",
			TraceID:        "trace-1",
			Payload:        evtBytes,
		}

		verbs := []webhook.Verb{
			{
				Action: "tag",
				Params: json.RawMessage(`{"tags": ["urgent", "lead"]}`),
			},
			{
				Action: "wait",
				Params: json.RawMessage(`{"duration": "10ms"}`),
			},
			{
				Action: "close",
				Params: json.RawMessage(`{}`),
			},
		}

		err = engine.Execute(ctx, task, verbs)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		updatedContact, err := contactRepo.GetByID(ctx, ws.ID, c.ID)
		if err != nil {
			t.Fatalf("failed to query contact: %v", err)
		}

		if len(updatedContact.Tags) != 2 {
			t.Errorf("expected 2 tags, got %v", updatedContact.Tags)
		}

		var chatStatus string
		err = pool.QueryRow(ctx, "SELECT status FROM chats WHERE workspace_id = $1 AND contact_id = $2", ws.ID, c.ID).Scan(&chatStatus)
		if err != nil {
			t.Fatalf("failed to query chat status: %v", err)
		}
		if chatStatus != "closed" {
			t.Errorf("expected chat status 'closed', got %q", chatStatus)
		}
	})

	t.Run("Wait cap enforcement", func(t *testing.T) {
		evt := inbound.InboundEventPayload{
			Event:       "message.received",
			TraceID:     "trace-2",
			MessageID:   "msg-2",
			Channel:     "telegram",
			WorkspaceID: ws.ID.String(),
			From:        "sender-2",
		}
		evtBytes, _ := json.Marshal(evt)

		task := webhook.WebhookDeliveryTask{
			ID:             uuid.New(),
			SubscriptionID: uuid.New(),
			WorkspaceID:    ws.ID,
			Payload:        evtBytes,
		}

		start := time.Now()
		verbs := []webhook.Verb{
			{
				Action: "wait",
				Params: json.RawMessage(`{"duration": "50ms"}`),
			},
		}
		err := engine.Execute(ctx, task, verbs)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		elapsed := time.Since(start)
		if elapsed < 40*time.Millisecond {
			t.Errorf("expected wait of 50ms, took %v", elapsed)
		}
	})

	t.Run("Parameter parsing errors", func(t *testing.T) {
		evt := inbound.InboundEventPayload{
			Event:       "message.received",
			TraceID:     "trace-3",
			MessageID:   "msg-3",
			Channel:     "telegram",
			WorkspaceID: ws.ID.String(),
			From:        "sender-3",
		}
		evtBytes, _ := json.Marshal(evt)
		task := webhook.WebhookDeliveryTask{
			ID:          uuid.New(),
			WorkspaceID: ws.ID,
			Payload:     evtBytes,
		}

		// Invalid wait duration string
		verbs := []webhook.Verb{
			{
				Action: "wait",
				Params: json.RawMessage(`{"duration": "invalid"}`),
			},
		}
		err := engine.Execute(ctx, task, verbs)
		if err == nil {
			t.Fatal("expected error on invalid wait duration")
		}

		// Invalid JSON params
		verbs2 := []webhook.Verb{
			{
				Action: "tag",
				Params: json.RawMessage(`{"tags":`),
			},
		}
		err2 := engine.Execute(ctx, task, verbs2)
		if err2 == nil {
			t.Fatal("expected error on invalid json params")
		}
	})

	t.Run("Context cancellation halts execution midway", func(t *testing.T) {
		evt := inbound.InboundEventPayload{
			Event:       "message.received",
			TraceID:     "trace-4",
			MessageID:   "msg-4",
			Channel:     "telegram",
			WorkspaceID: ws.ID.String(),
			From:        "sender-4",
		}
		evtBytes, _ := json.Marshal(evt)
		task := webhook.WebhookDeliveryTask{
			ID:          uuid.New(),
			WorkspaceID: ws.ID,
			Payload:     evtBytes,
		}

		cancelCtx, cancel := context.WithCancel(ctx)

		verbs := []webhook.Verb{
			{
				Action: "wait",
				Params: json.RawMessage(`{"duration": "5s"}`),
			},
			{
				Action: "tag",
				Params: json.RawMessage(`{"tags": ["should-not-be-added"]}`),
			},
		}

		// Cancel the context after 50ms
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		err := engine.Execute(cancelCtx, task, verbs)
		if err == nil {
			t.Fatal("expected context canceled error")
		}
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("expected canceled/deadline error, got %v", err)
		}

		// Verify tag was NOT added
		c, err := contactRepo.ResolveContact(ctx, ws.ID, "telegram", "sender-4", "", "", "")
		if err != nil {
			t.Fatalf("ResolveContact failed: %v", err)
		}
		updatedC, err := contactRepo.GetByID(ctx, ws.ID, c.ID)
		if err != nil {
			t.Fatalf("query contact failed: %v", err)
		}
		for _, tag := range updatedC.Tags {
			if tag == "should-not-be-added" {
				t.Error("tag was added despite context cancellation")
			}
		}
	})

	rec := activity.New(pool, nil, nil, nil)
	engine.WithActivity(rec)

	t.Run("Pause bot default duration (12h)", func(t *testing.T) {
		c, err := contactRepo.ResolveContact(ctx, ws.ID, "telegram", "sender-pause-indef", "", "", "")
		if err != nil {
			t.Fatalf("ResolveContact failed: %v", err)
		}

		evt := inbound.InboundEventPayload{
			Event:       "message.received",
			TraceID:     "trace-pause-1",
			MessageID:   "msg-pause-1",
			Channel:     "telegram",
			WorkspaceID: ws.ID.String(),
			From:        "sender-pause-indef",
		}
		evtBytes, _ := json.Marshal(evt)
		task := webhook.WebhookDeliveryTask{
			ID:          uuid.New(),
			WorkspaceID: ws.ID,
			Payload:     evtBytes,
		}

		verbs := []webhook.Verb{
			{
				Action: "pause_bot",
				Params: json.RawMessage(`{}`),
			},
		}

		before := time.Now().UTC()
		err = engine.Execute(ctx, task, verbs)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		state, _, err := rec.Snapshot(ctx, ws.ID, activity.ChatOf(uuid.Nil, c.ID))
		if err != nil {
			t.Fatalf("Snapshot failed: %v", err)
		}
		if state.AI.Mode != domain.AIModePaused {
			t.Errorf("expected AI mode paused, got %q", state.AI.Mode)
		}
		expected := before.Add(12 * time.Hour)
		if diff := state.AI.PausedUntil.Sub(expected); diff < -10*time.Second || diff > 10*time.Second {
			t.Errorf("expected PausedUntil ≈ now+12h (%v), got %v", expected, state.AI.PausedUntil)
		}
	})

	t.Run("Pause bot with duration", func(t *testing.T) {
		c, err := contactRepo.ResolveContact(ctx, ws.ID, "telegram", "sender-pause-dur", "", "", "")
		if err != nil {
			t.Fatalf("ResolveContact failed: %v", err)
		}

		evt := inbound.InboundEventPayload{
			Event:       "message.received",
			TraceID:     "trace-pause-2",
			MessageID:   "msg-pause-2",
			Channel:     "telegram",
			WorkspaceID: ws.ID.String(),
			From:        "sender-pause-dur",
		}
		evtBytes, _ := json.Marshal(evt)
		task := webhook.WebhookDeliveryTask{
			ID:          uuid.New(),
			WorkspaceID: ws.ID,
			Payload:     evtBytes,
		}

		verbs := []webhook.Verb{
			{
				Action: "pause_bot",
				Params: json.RawMessage(`{"duration": "30m"}`),
			},
		}

		before := time.Now().UTC()
		err = engine.Execute(ctx, task, verbs)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		state, _, err := rec.Snapshot(ctx, ws.ID, activity.ChatOf(uuid.Nil, c.ID))
		if err != nil {
			t.Fatalf("Snapshot failed: %v", err)
		}
		if state.AI.Mode != domain.AIModePaused {
			t.Errorf("expected AI mode paused, got %q", state.AI.Mode)
		}
		expected := before.Add(30 * time.Minute)
		if diff := state.AI.PausedUntil.Sub(expected); diff < -10*time.Second || diff > 10*time.Second {
			t.Errorf("expected PausedUntil ≈ now+30m (%v), got %v", expected, state.AI.PausedUntil)
		}
	})

	t.Run("Pause bot with invalid duration", func(t *testing.T) {
		evt := inbound.InboundEventPayload{
			Event:       "message.received",
			TraceID:     "trace-pause-3",
			MessageID:   "msg-pause-3",
			Channel:     "telegram",
			WorkspaceID: ws.ID.String(),
			From:        "sender-pause-invalid",
		}
		evtBytes, _ := json.Marshal(evt)
		task := webhook.WebhookDeliveryTask{
			ID:          uuid.New(),
			WorkspaceID: ws.ID,
			Payload:     evtBytes,
		}

		verbs := []webhook.Verb{
			{
				Action: "pause_bot",
				Params: json.RawMessage(`{"duration": "invalid"}`),
			},
		}

		err := engine.Execute(ctx, task, verbs)
		if err == nil {
			t.Fatal("expected error on invalid duration")
		}
	})
}
