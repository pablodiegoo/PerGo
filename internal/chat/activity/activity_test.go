package activity_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/pablojhp.pergo/internal/chat/activity"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/postgres"
)

var (
	pgOnce sync.Once
	pgDSN  string
)

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("PERGO_DATABASE_URL")
	if dsn != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if pool, err := pgxpool.New(ctx, dsn); err == nil {
			if err := pool.Ping(ctx); err == nil {
				db, err := postgres.NewSQLDB(pool)
				if err == nil {
					_ = postgres.RunMigrations(db)
					_ = db.Close()
				}
				return pool
			}
			pool.Close()
		}
	}

	pgOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		defer func() {
			_ = recover()
		}()
		pgContainer, err := tcpostgres.Run(ctx,
			"postgres:16-alpine",
			tcpostgres.WithDatabase("pergo"),
			tcpostgres.WithUsername("postgres"),
			tcpostgres.WithPassword("postgres"),
		)
		if err != nil || pgContainer == nil {
			return
		}
		connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			return
		}
		var pool *pgxpool.Pool
		for i := 0; i < 10; i++ {
			pool, err = pgxpool.New(ctx, connStr)
			if err == nil {
				if pool.Ping(ctx) == nil {
					break
				}
				pool.Close()
			}
			time.Sleep(500 * time.Millisecond)
		}
		if pool == nil {
			return
		}
		defer pool.Close()
		db, err := postgres.NewSQLDB(pool)
		if err != nil {
			return
		}
		defer db.Close()
		if err := postgres.RunMigrations(db); err != nil {
			return
		}
		pgDSN = connStr
	})

	if pgDSN == "" {
		t.Skip("PostgreSQL testcontainer not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, pgDSN)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("PostgreSQL ping failed: %v", err)
	}
	return pool
}

func seedWorkspaceAndConn(t *testing.T, ctx context.Context, pool *pgxpool.Pool, channel, sender string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	wsID := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO workspaces (id, name) VALUES ($1, $2)`, wsID, "ws-"+wsID.String())
	if err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspaces WHERE id = $1`, wsID)
	})

	connID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO connections (id, workspace_id, name, slug, channel, sender_identity, status, credentials, is_default)
		VALUES ($1, $2, 'Conn', $3, $4, $5, 'active', '{}', true)
	`, connID, wsID, "conn-"+connID.String()[:8], channel, sender)
	if err != nil {
		t.Fatalf("insert connection: %v", err)
	}
	return wsID, connID
}

func seedContactWithIdentity(t *testing.T, ctx context.Context, pool *pgxpool.Pool, wsID uuid.UUID, channel, phone string) uuid.UUID {
	t.Helper()
	contactID := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO contacts (id, workspace_id, name) VALUES ($1, $2, $3)`, contactID, wsID, phone)
	if err != nil {
		t.Fatalf("insert contact: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO contact_identities (contact_id, workspace_id, channel, sender_identity)
		VALUES ($1, $2, $3, $4)
	`, contactID, wsID, channel, phone)
	if err != nil {
		t.Fatalf("insert contact identity: %v", err)
	}
	return contactID
}

func TestRecorder_Outbound_HumanHandoff_SlidingWindow_And_Idempotency(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	wsID, connID := seedWorkspaceAndConn(t, ctx, pool, "whatsapp_cloud", "+551100000001")

	pub := activity.NewMemoryPublisher()
	deb := activity.NewMemoryDebounceCanceller()
	rec := activity.New(pool, pub, deb, nil)

	t0 := time.Now().UTC().Truncate(time.Millisecond)
	customerPhone := "+5511999991111"
	contactID := seedContactWithIdentity(t, ctx, pool, wsID, "whatsapp_cloud", customerPhone)

	// Open the 24h WABA window via an inbound turn first
	inReceipt, err := rec.Inbound(ctx, wsID, activity.InboundTurn{
		Chat:       activity.ChatOf(connID, contactID).WithChannel("whatsapp_cloud"),
		UID:        "uid-inbound-open-1",
		SenderName: "Customer",
		SenderID:   customerPhone,
		Body:       "Hi, I need support",
		Channel:    "whatsapp_cloud",
		At:         t0.Add(-5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("Inbound failed: %v", err)
	}

	// 1. First human_agent outbound turn pauses AI for 12h, cancels debounce by ChatID, and publishes handoff events
	receipt1, err := rec.Outbound(ctx, wsID, activity.OutboundTurn{
		Chat:       activity.ChatAt(connID, customerPhone),
		UID:        "uid-human-1",
		SenderType: domain.SenderTypeHumanAgent,
		SenderName: "Agent Smith",
		SenderID:   "+551100000001",
		Channel:    "whatsapp_cloud",
		To:         customerPhone,
		Body:       "Hello, human agent here.",
		At:         t0,
	})
	if err != nil {
		t.Fatalf("Outbound 1 failed: %v", err)
	}
	if receipt1.Duplicate {
		t.Fatal("expected Duplicate=false on first Outbound")
	}
	chatID := receipt1.Chat.ID
	if chatID != inReceipt.Chat.ID {
		t.Fatalf("expected ChatID %s, got %s", inReceipt.Chat.ID, chatID)
	}

	wantPause1 := t0.Add(12 * time.Hour)
	if receipt1.Chat.AI.Mode != domain.AIModePaused || receipt1.Chat.AI.PausedUntil == nil || !receipt1.Chat.AI.PausedUntil.Equal(wantPause1) {
		t.Fatalf("Chat.AI = %+v, want paused until %v", receipt1.Chat.AI, wantPause1)
	}
	if receipt1.Message == nil || receipt1.Message.Metadata["status"] != "queued" {
		t.Fatalf("expected queued message, got %+v", receipt1.Message)
	}

	// Debounce cancelled by (wsID, chatID)
	cancelled := deb.Cancelled()
	if len(cancelled) != 1 || cancelled[0].WorkspaceID != wsID || cancelled[0].ChatID != chatID {
		t.Fatalf("expected debounce cancelled for chat %s, got %+v", chatID, cancelled)
	}

	// Handoff events published to both subjects
	msgs := pub.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 handoff messages published, got %d: %+v", len(msgs), msgs)
	}
	var payload map[string]any
	_ = json.Unmarshal(msgs[0].Data, &payload)
	if payload["chat_id"] != chatID.String() || payload["event"] != "chat.handoff.human_takeover" {
		t.Fatalf("unexpected handoff payload: %+v", payload)
	}

	// 2. Duplicate UID is a no-op receipt with no extra events
	dupReceipt, err := rec.Outbound(ctx, wsID, activity.OutboundTurn{
		Chat:       activity.ChatID(chatID),
		UID:        "uid-human-1",
		SenderType: domain.SenderTypeHumanAgent,
		Body:       "Duplicate retry",
		At:         t0.Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("duplicate Outbound failed: %v", err)
	}
	if !dupReceipt.Duplicate {
		t.Fatal("expected Duplicate=true on retry with same UID")
	}
	if len(pub.Messages()) != 2 {
		t.Fatalf("expected no new handoff events on duplicate UID, got %d", len(pub.Messages()))
	}

	// 3. Second human_agent reply while paused slides T forward without re-publishing handoff
	t1 := t0.Add(2 * time.Hour)
	receipt2, err := rec.Outbound(ctx, wsID, activity.OutboundTurn{
		Chat:       activity.ChatID(chatID),
		UID:        "uid-human-2",
		SenderType: domain.SenderTypeHumanAgent,
		SenderName: "Agent Smith",
		Body:       "Second human reply",
		At:         t1,
	})
	if err != nil {
		t.Fatalf("Outbound 2 failed: %v", err)
	}
	wantPause2 := t1.Add(12 * time.Hour)
	if receipt2.Chat.AI.Mode != domain.AIModePaused || receipt2.Chat.AI.PausedUntil == nil || !receipt2.Chat.AI.PausedUntil.Equal(wantPause2) {
		t.Fatalf("expected T to slide to %v, got %+v", wantPause2, receipt2.Chat.AI)
	}
	if len(pub.Messages()) != 2 {
		t.Fatalf("expected NO new handoff events when already paused, got %d", len(pub.Messages()))
	}
}

func TestRecorder_PerChatIsolation_SameContactTwoConnections(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	wsID, connA := seedWorkspaceAndConn(t, ctx, pool, "whatsapp_cloud", "+551100000010")
	connB := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO connections (id, workspace_id, name, slug, channel, sender_identity, status, credentials, is_default)
		VALUES ($1, $2, 'Conn B', 'conn-b', 'whatsapp', '+551100000020', 'active', '{}', false)
	`, connB, wsID)
	if err != nil {
		t.Fatalf("insert connB: %v", err)
	}

	pub := activity.NewMemoryPublisher()
	deb := activity.NewMemoryDebounceCanceller()
	rec := activity.New(pool, pub, deb, nil)

	t0 := time.Now().UTC().Truncate(time.Millisecond)
	phone := "+5511988887777"
	contactID := seedContactWithIdentity(t, ctx, pool, wsID, "whatsapp_cloud", phone)

	// Open window on Chat A (connA, whatsapp_cloud) via Inbound; create Chat B (connB, whatsapp) via ChatOf
	resA, err := rec.Inbound(ctx, wsID, activity.InboundTurn{
		Chat:     activity.ChatOf(connA, contactID).WithChannel("whatsapp_cloud"),
		UID:      "uid-connA-in",
		SenderID: phone,
		Body:     "Inbound on Line A",
		Channel:  "whatsapp_cloud",
		At:       t0,
	})
	if err != nil {
		t.Fatalf("init Chat A: %v", err)
	}
	resB, err := rec.Outbound(ctx, wsID, activity.OutboundTurn{
		Chat:       activity.ChatOf(connB, contactID).WithChannel("whatsapp"),
		UID:        "uid-connB-init",
		SenderType: domain.SenderTypeAIAgent,
		Channel:    "whatsapp",
		To:         phone,
		Body:       "Welcome on Line B",
		At:         t0,
	})
	if err != nil {
		t.Fatalf("init Chat B: %v", err)
	}
	if resA.Chat.ID == resB.Chat.ID {
		t.Fatalf("expected distinct Chats for connA and connB, got same ID %s", resA.Chat.ID)
	}
	if resA.Chat.ContactID != resB.Chat.ContactID {
		t.Fatalf("expected same ContactID across connA and connB, got %s vs %s", resA.Chat.ContactID, resB.Chat.ContactID)
	}

	// Human agent replies on Chat A -> pauses Chat A and cancels debounce for Chat A ONLY; Chat B remains active!
	_, err = rec.Outbound(ctx, wsID, activity.OutboundTurn{
		Chat:       activity.ChatID(resA.Chat.ID),
		UID:        "uid-connA-human",
		SenderType: domain.SenderTypeHumanAgent,
		Body:       "Human taking over Line A",
		At:         t0.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("human send on Chat A: %v", err)
	}

	snapA, okA, err := rec.Snapshot(ctx, wsID, activity.ChatID(resA.Chat.ID))
	if err != nil || !okA {
		t.Fatalf("Snapshot A failed: ok=%v err=%v", okA, err)
	}
	snapB, okB, err := rec.Snapshot(ctx, wsID, activity.ChatID(resB.Chat.ID))
	if err != nil || !okB {
		t.Fatalf("Snapshot B failed: ok=%v err=%v", okB, err)
	}

	if snapA.AI.Effective(t0.Add(2 * time.Minute)).IsEngaged() {
		t.Fatal("expected Chat A to be paused after human reply")
	}
	if !snapB.AI.Effective(t0.Add(2 * time.Minute)).IsEngaged() {
		t.Fatalf("expected Chat B to remain active (engaged), got %+v", snapB.AI)
	}

	cancelled := deb.Cancelled()
	if len(cancelled) != 1 || cancelled[0].ChatID != resA.Chat.ID {
		t.Fatalf("expected debounce cancelled only for Chat A (%s), got %+v", resA.Chat.ID, cancelled)
	}
}

func TestRecorder_SetAI_And_DispatchFailed(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	wsID, connID := seedWorkspaceAndConn(t, ctx, pool, "whatsapp_cloud", "+551100000002")

	pub := activity.NewMemoryPublisher()
	deb := activity.NewMemoryDebounceCanceller()
	rec := activity.New(pool, pub, deb, nil)

	t0 := time.Now().UTC().Truncate(time.Millisecond)
	phone := "+5511977776666"
	contactID := seedContactWithIdentity(t, ctx, pool, wsID, "whatsapp_cloud", phone)

	// Open chat & window via Inbound, then send AI outbound
	inRes, err := rec.Inbound(ctx, wsID, activity.InboundTurn{
		Chat:     activity.ChatOf(connID, contactID).WithChannel("whatsapp_cloud"),
		UID:      "uid-in-dispatch-test",
		SenderID: phone,
		Body:     "Hello",
		Channel:  "whatsapp_cloud",
		At:       t0.Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("Inbound failed: %v", err)
	}
	initRes, err := rec.Outbound(ctx, wsID, activity.OutboundTurn{
		Chat:       activity.ChatID(inRes.Chat.ID),
		UID:        "uid-dispatch-test",
		SenderType: domain.SenderTypeAIAgent,
		Channel:    "whatsapp_cloud",
		To:         phone,
		Body:       "Initial AI message",
		At:         t0,
	})
	if err != nil {
		t.Fatalf("Outbound failed: %v", err)
	}
	chatID := initRes.Chat.ID

	// 1. DispatchFailed marks message status as "failed"
	if err := rec.DispatchFailed(ctx, wsID, "uid-dispatch-test"); err != nil {
		t.Fatalf("DispatchFailed failed: %v", err)
	}
	var status string
	err = pool.QueryRow(ctx, `SELECT metadata->>'status' FROM chat_messages WHERE workspace_id = $1 AND uid = $2`, wsID, "uid-dispatch-test").Scan(&status)
	if err != nil {
		t.Fatalf("query message status: %v", err)
	}
	if status != "failed" {
		t.Fatalf("expected metadata status 'failed', got %q", status)
	}

	// 2. SetAI(AIPauseUntil(48h)) pauses for 48h; subsequent human reply does not shorten it
	pause48 := t0.Add(48 * time.Hour)
	aiState, err := rec.SetAI(ctx, wsID, activity.ChatID(chatID), activity.AIPauseUntil(pause48))
	if err != nil {
		t.Fatalf("SetAI(AIPauseUntil) failed: %v", err)
	}
	if aiState.Mode != domain.AIModePaused || aiState.PausedUntil == nil || !aiState.PausedUntil.Equal(pause48) {
		t.Fatalf("expected paused until %v, got %+v", pause48, aiState)
	}

	afterHuman, err := rec.Outbound(ctx, wsID, activity.OutboundTurn{
		Chat:       activity.ChatID(chatID),
		UID:        "uid-human-during-48h",
		SenderType: domain.SenderTypeHumanAgent,
		Body:       "Human reply during 48h pause",
		At:         t0.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("Outbound during 48h pause failed: %v", err)
	}
	if afterHuman.Chat.AI.PausedUntil == nil || !afterHuman.Chat.AI.PausedUntil.Equal(pause48) {
		t.Fatalf("expected 48h pause preserved, got %+v", afterHuman.Chat.AI)
	}

	// 3. SetAI(AIOff()) sets off; survives past 12h and dominates human handoff
	aiOff, err := rec.SetAI(ctx, wsID, activity.ChatID(chatID), activity.AIOff())
	if err != nil {
		t.Fatalf("SetAI(AIOff) failed: %v", err)
	}
	if aiOff.Mode != domain.AIModeOff {
		t.Fatalf("expected AIModeOff, got %+v", aiOff)
	}
	snap, _, err := rec.Snapshot(ctx, wsID, activity.ChatID(chatID))
	if err != nil {
		t.Fatalf("Snapshot failed: %v", err)
	}
	if snap.AI.Effective(t0.Add(24 * time.Hour)).IsEngaged() {
		t.Fatal("expected manual AIOff to survive past 12h")
	}

	// 4. SetAI(AIResume()) resumes AI to active
	aiResumed, err := rec.SetAI(ctx, wsID, activity.ChatID(chatID), activity.AIResume())
	if err != nil {
		t.Fatalf("SetAI(AIResume) failed: %v", err)
	}
	if !aiResumed.IsEngaged() {
		t.Fatalf("expected AI engaged after AIResume, got %+v", aiResumed)
	}

	// 5. Service window enforcement on expired WABA window returns *domain.TemplateRequiredError
	expiredAt := t0.Add(-1 * time.Hour)
	_, err = pool.Exec(ctx, `UPDATE chats SET service_window_expires_at = $3 WHERE workspace_id = $1 AND id = $2`, wsID, chatID, expiredAt)
	if err != nil {
		t.Fatalf("set expired window: %v", err)
	}
	_, err = rec.Outbound(ctx, wsID, activity.OutboundTurn{
		Chat:       activity.ChatID(chatID),
		UID:        "uid-blocked-window",
		SenderType: domain.SenderTypeHumanAgent,
		Channel:    "whatsapp_cloud",
		Body:       "Blocked freeform",
		At:         t0,
	})
	var tmplErr *domain.TemplateRequiredError
	if !errors.As(err, &tmplErr) {
		t.Fatalf("expected *domain.TemplateRequiredError, got %T: %v", err, err)
	}
}

func TestRecorder_Inbound(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	wsID, connID := seedWorkspaceAndConn(t, ctx, pool, "whatsapp_cloud", "+551100009001")
	pub := activity.NewMemoryPublisher()
	deb := activity.NewMemoryDebounceCanceller()
	rec := activity.New(pool, pub, deb, nil)

	contactID := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO contacts (id, workspace_id, name) VALUES ($1, $2, 'Alice')`, contactID, wsID)
	if err != nil {
		t.Fatalf("insert contact: %v", err)
	}

	// Create a second connection so the same Contact has two independent Chats
	conn2ID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO connections (id, workspace_id, name, slug, channel, sender_identity, status, credentials, is_default)
		VALUES ($1, $2, 'Test WABA 2', $3, 'whatsapp_cloud', '+551100009002', 'active', '{}', false)
	`, conn2ID, wsID, "conn-"+conn2ID.String()[:8])
	if err != nil {
		t.Fatalf("insert second connection: %v", err)
	}

	t0 := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

	// Seed Chat 1 (closed) on connID and Chat 2 (closed) on conn2ID for the same contact
	var chat1ID, chat2ID uuid.UUID
	err = pool.QueryRow(ctx, `
		INSERT INTO chats (workspace_id, connection_id, contact_id, status, unread_count, ai_mode, last_message_at)
		VALUES ($1, $2, $3, 'closed', 0, 'active', $4)
		RETURNING id
	`, wsID, connID, contactID, t0.Add(-2*time.Hour)).Scan(&chat1ID)
	if err != nil {
		t.Fatalf("insert chat1: %v", err)
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO chats (workspace_id, connection_id, contact_id, status, unread_count, ai_mode, last_message_at)
		VALUES ($1, $2, $3, 'closed', 0, 'active', $4)
		RETURNING id
	`, wsID, conn2ID, contactID, t0.Add(-2*time.Hour)).Scan(&chat2ID)
	if err != nil {
		t.Fatalf("insert chat2: %v", err)
	}

	// 1. Inbound on connID reopens Chat 1, increments unread to 1, extends service window, leaves Chat 2 closed
	r1, err := rec.Inbound(ctx, wsID, activity.InboundTurn{
		Chat:       activity.ChatOf(connID, contactID).WithChannel("whatsapp_cloud"),
		UID:        "wamid.in.1",
		SenderName: "Alice",
		SenderID:   "+5511999990001",
		Body:       "Hello on conn 1",
		Channel:    "whatsapp_cloud",
		At:         t0,
	})
	if err != nil {
		t.Fatalf("Inbound 1 failed: %v", err)
	}
	if r1.Duplicate {
		t.Fatal("expected Duplicate=false on first inbound")
	}
	if !r1.AIMayAct() {
		t.Fatal("expected AIMayAct()=true when AI is active")
	}
	if r1.Chat.ID != chat1ID {
		t.Fatalf("expected chat1ID %v, got %v", chat1ID, r1.Chat.ID)
	}
	if r1.Chat.Status != domain.ChatStatusOpen || r1.Chat.UnreadCount != 1 {
		t.Fatalf("expected Chat 1 open with unread=1, got status=%q unread=%d", r1.Chat.Status, r1.Chat.UnreadCount)
	}
	wantWindow := t0.Add(24 * time.Hour)
	if r1.Chat.ServiceWindowExpiresAt == nil || !r1.Chat.ServiceWindowExpiresAt.Equal(wantWindow) {
		t.Fatalf("expected ServiceWindowExpiresAt=%v, got %v", wantWindow, r1.Chat.ServiceWindowExpiresAt)
	}
	if r1.Chat.LastInboundAt == nil || !r1.Chat.LastInboundAt.Equal(t0) {
		t.Fatalf("expected LastInboundAt=%v, got %v", t0, r1.Chat.LastInboundAt)
	}
	snap1, exists1, err := rec.Snapshot(ctx, wsID, activity.ChatID(chat1ID))
	if err != nil || !exists1 {
		t.Fatalf("Snapshot chat1: exists=%v err=%v", exists1, err)
	}
	if snap1.LastInboundAt == nil || !snap1.LastInboundAt.Equal(t0) {
		t.Fatalf("expected persisted LastInboundAt=%v in Snapshot, got %v", t0, snap1.LastInboundAt)
	}

	// Verify Chat 2 of the same Contact is STILL closed with unread=0
	snap2, exists, err := rec.Snapshot(ctx, wsID, activity.ChatID(chat2ID))
	if err != nil || !exists {
		t.Fatalf("Snapshot chat2: exists=%v err=%v", exists, err)
	}
	if snap2.Status != domain.ChatStatusClosed || snap2.UnreadCount != 0 {
		t.Fatalf("expected Chat 2 to remain closed with unread=0, got status=%q unread=%d", snap2.Status, snap2.UnreadCount)
	}

	// 2. Duplicate UID on Inbound is idempotent (does not increment unread twice)
	rDup, err := rec.Inbound(ctx, wsID, activity.InboundTurn{
		Chat:       activity.ChatOf(connID, contactID).WithChannel("whatsapp_cloud"),
		UID:        "wamid.in.1",
		SenderName: "Alice",
		SenderID:   "+5511999990001",
		Body:       "Hello on conn 1",
		Channel:    "whatsapp_cloud",
		At:         t0.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("duplicate Inbound failed: %v", err)
	}
	if !rDup.Duplicate {
		t.Fatal("expected Duplicate=true on replay of wamid.in.1")
	}
	if rDup.AIMayAct() {
		t.Fatal("expected AIMayAct()=false on duplicate inbound")
	}
	if rDup.Chat.UnreadCount != 1 {
		t.Fatalf("expected unread_count to stay 1 on duplicate, got %d", rDup.Chat.UnreadCount)
	}

	// 3. Human takeover pauses AI for 12h; inbound during pause has AIMayAct()=false; inbound after 13h has AIMayAct()=true without reset write
	_, err = rec.Outbound(ctx, wsID, activity.OutboundTurn{
		Chat:       activity.ChatID(chat1ID),
		UID:        "wamid.out.human.1",
		SenderType: domain.SenderTypeHumanAgent,
		SenderName: "Agent Bob",
		Body:       "I am taking over",
		At:         t0.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("human Outbound failed: %v", err)
	}

	rDuringPause, err := rec.Inbound(ctx, wsID, activity.InboundTurn{
		Chat:       activity.ChatID(chat1ID),
		UID:        "wamid.in.during-pause",
		SenderName: "Alice",
		SenderID:   "+5511999990001",
		Body:       "Are you there?",
		At:         t0.Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatalf("Inbound during pause failed: %v", err)
	}
	if rDuringPause.AIMayAct() {
		t.Fatal("expected AIMayAct()=false while human pause is active")
	}
	if rDuringPause.Chat.UnreadCount != 2 {
		t.Fatalf("expected unread_count=2, got %d", rDuringPause.Chat.UnreadCount)
	}

	// Snooze Chat 1 until t0+4h, then send inbound at t0+13h (after both snooze and 12h AI pause expired)
	snoozeUntil := t0.Add(4 * time.Hour)
	_, err = pool.Exec(ctx, `UPDATE chats SET status = 'snoozed', snoozed_until = $3 WHERE workspace_id = $1 AND id = $2`, wsID, chat1ID, snoozeUntil)
	if err != nil {
		t.Fatalf("snooze chat1: %v", err)
	}

	rAfterExpiry, err := rec.Inbound(ctx, wsID, activity.InboundTurn{
		Chat:       activity.ChatID(chat1ID),
		UID:        "wamid.in.after-expiry",
		SenderName: "Alice",
		SenderID:   "+5511999990001",
		Body:       "Back after 13h",
		At:         t0.Add(13 * time.Hour),
	})
	if err != nil {
		t.Fatalf("Inbound after expiry failed: %v", err)
	}
	if !rAfterExpiry.AIMayAct() {
		t.Fatal("expected AIMayAct()=true after 12h pause expired")
	}
	if rAfterExpiry.Chat.Status != domain.ChatStatusOpen || rAfterExpiry.Chat.SnoozedUntil != nil {
		t.Fatalf("expected snoozed chat to reopen and clear snoozed_until, got status=%q snoozed=%v", rAfterExpiry.Chat.Status, rAfterExpiry.Chat.SnoozedUntil)
	}
}

func TestRecorder_S3_ServiceWindow_IndependentConnections_CTWA_TouchOutbound_And_ClaimExpiring(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	wsID, connA := seedWorkspaceAndConn(t, ctx, pool, "whatsapp_cloud", "+551100001001")
	connB := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO connections (id, workspace_id, name, slug, channel, sender_identity, status, credentials, is_default)
		VALUES ($1, $2, 'WABA B', 'waba-b', 'whatsapp_cloud', '+551100001002', 'active', '{}', false)
	`, connB, wsID)
	if err != nil {
		t.Fatalf("insert connB: %v", err)
	}

	connWeb := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO connections (id, workspace_id, name, slug, channel, sender_identity, status, credentials, is_default)
		VALUES ($1, $2, 'WA Web', 'wa-web', 'whatsapp', '+551100001003', 'active', '{}', false)
	`, connWeb, wsID)
	if err != nil {
		t.Fatalf("insert connWeb: %v", err)
	}

	rec := activity.New(pool, activity.NewMemoryPublisher(), activity.NewMemoryDebounceCanceller(), nil)
	t0 := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

	t.Run("Two WABA connections chatting with same Contact have independent windows", func(t *testing.T) {
		phone := "+5511911112222"
		contactID := seedContactWithIdentity(t, ctx, pool, wsID, "whatsapp_cloud", phone)

		// Inbound on connA opens 24h window on connA ONLY
		_, err := rec.Inbound(ctx, wsID, activity.InboundTurn{
			Chat:       activity.ChatOf(connA, contactID).WithChannel("whatsapp_cloud"),
			UID:        "wamid.indep.1",
			SenderName: "Customer",
			SenderID:   phone,
			Body:       "Hello Line A",
			Channel:    "whatsapp_cloud",
			At:         t0,
		})
		if err != nil {
			t.Fatalf("Inbound connA: %v", err)
		}

		// Snapshot on connA is open; Snapshot on connB is closed (does not exist)
		snapA, existsA, err := rec.Snapshot(ctx, wsID, activity.ChatAt(connA, phone).WithChannel("whatsapp_cloud"))
		if err != nil || !existsA {
			t.Fatalf("Snapshot connA: exists=%v err=%v", existsA, err)
		}
		if !domain.Window(snapA, existsA, t0.Add(time.Hour)).Open {
			t.Fatal("expected connA window to be open")
		}

		snapB, existsB, err := rec.Snapshot(ctx, wsID, activity.ChatAt(connB, phone).WithChannel("whatsapp_cloud"))
		if err != nil {
			t.Fatalf("Snapshot connB: %v", err)
		}
		if existsB || domain.Window(snapB, existsB, t0.Add(time.Hour)).Open {
			t.Fatalf("expected connB to have no chat and closed window, got exists=%v window=%+v", existsB, domain.Window(snapB, existsB, t0.Add(time.Hour)))
		}

		// Freeform Outbound on connA succeeds; freeform Outbound on connB fails with *domain.TemplateRequiredError
		if _, err := rec.Outbound(ctx, wsID, activity.OutboundTurn{
			Chat:       activity.ChatAt(connA, phone),
			UID:        "uid-indep-connA-out",
			SenderType: domain.SenderTypeAIAgent,
			Channel:    "whatsapp_cloud",
			Body:       "Reply on Line A",
			At:         t0.Add(time.Hour),
		}); err != nil {
			t.Fatalf("expected Outbound on connA to succeed, got %v", err)
		}

		_, err = rec.Outbound(ctx, wsID, activity.OutboundTurn{
			Chat:       activity.ChatOf(connB, contactID).WithChannel("whatsapp_cloud"),
			UID:        "uid-indep-connB-out",
			SenderType: domain.SenderTypeAIAgent,
			Channel:    "whatsapp_cloud",
			Body:       "Blocked on Line B",
			At:         t0.Add(time.Hour),
		})
		var tmplErr *domain.TemplateRequiredError
		if !errors.As(err, &tmplErr) {
			t.Fatalf("expected *domain.TemplateRequiredError on connB, got %T: %v", err, err)
		}
	})

	t.Run("CTWA inbound opens 24h window, standard follow-up within window extends 24h and preserves ctwa, standard after expiry resets to standard", func(t *testing.T) {
		phone := "+5511922223333"
		contactID := seedContactWithIdentity(t, ctx, pool, wsID, "whatsapp_cloud", phone)

		rCTWA, err := rec.Inbound(ctx, wsID, activity.InboundTurn{
			Chat:           activity.ChatOf(connA, contactID).WithChannel("whatsapp_cloud"),
			UID:            "wamid.ctwa.100",
			SenderID:       phone,
			Body:           "Ad click",
			Channel:        "whatsapp_cloud",
			EntryPointType: domain.EntryPointCTWA,
			At:             t0,
		})
		if err != nil {
			t.Fatalf("CTWA Inbound: %v", err)
		}
		wantFirstExp := t0.Add(24 * time.Hour)
		if rCTWA.Chat.ServiceWindowExpiresAt == nil || !rCTWA.Chat.ServiceWindowExpiresAt.Equal(wantFirstExp) {
			t.Fatalf("expected 24h expiry %v, got %v", wantFirstExp, rCTWA.Chat.ServiceWindowExpiresAt)
		}
		if rCTWA.Chat.EntryPointType != domain.EntryPointCTWA {
			t.Fatalf("expected entry_point_type 'ctwa', got %q", rCTWA.Chat.EntryPointType)
		}

		// Standard inbound 1h later (while 24h window is still open) extends to t1 + 24h and preserves ctwa
		t1 := t0.Add(1 * time.Hour)
		rStd, err := rec.Inbound(ctx, wsID, activity.InboundTurn{
			Chat:           activity.ChatOf(connA, contactID).WithChannel("whatsapp_cloud"),
			UID:            "wamid.std.101",
			SenderID:       phone,
			Body:           "Follow-up",
			Channel:        "whatsapp_cloud",
			EntryPointType: domain.EntryPointStandard,
			At:             t1,
		})
		if err != nil {
			t.Fatalf("Standard Inbound: %v", err)
		}
		wantSecondExp := t1.Add(24 * time.Hour)
		if rStd.Chat.ServiceWindowExpiresAt == nil || !rStd.Chat.ServiceWindowExpiresAt.Equal(wantSecondExp) {
			t.Fatalf("expected 24h expiry %v extended after standard turn, got %v", wantSecondExp, rStd.Chat.ServiceWindowExpiresAt)
		}
		if rStd.Chat.EntryPointType != domain.EntryPointCTWA {
			t.Fatalf("expected entry_point_type 'ctwa' preserved, got %q", rStd.Chat.EntryPointType)
		}

		// Outbound freeform within 24h window (at t0 + 20h) succeeds
		if _, err := rec.Outbound(ctx, wsID, activity.OutboundTurn{
			Chat:       activity.ChatID(rCTWA.Chat.ID),
			UID:        "uid-ctwa-20h-reply",
			SenderType: domain.SenderTypeAIAgent,
			Channel:    "whatsapp_cloud",
			Body:       "Open within 24h",
			At:         t0.Add(20 * time.Hour),
		}); err != nil {
			t.Fatalf("expected freeform reply at 20h to succeed in 24h CTWA window, got %v", err)
		}

		// Outbound freeform at t0 + 48h fails with TemplateRequiredError because CSW is strictly 24h
		_, err = rec.Outbound(ctx, wsID, activity.OutboundTurn{
			Chat:       activity.ChatID(rCTWA.Chat.ID),
			UID:        "uid-ctwa-48h-reply",
			SenderType: domain.SenderTypeAIAgent,
			Channel:    "whatsapp_cloud",
			Body:       "Expired at 48h",
			At:         t0.Add(48 * time.Hour),
		})
		var tmplErr *domain.TemplateRequiredError
		if !errors.As(err, &tmplErr) {
			t.Fatalf("expected *domain.TemplateRequiredError at 48h for CTWA chat, got %T: %v", err, err)
		}

		// Standard inbound after 24h window expired resets entry_point_type to standard
		tAfterExpiry := t0.Add(49 * time.Hour)
		rStdAfter, err := rec.Inbound(ctx, wsID, activity.InboundTurn{
			Chat:           activity.ChatOf(connA, contactID).WithChannel("whatsapp_cloud"),
			UID:            "wamid.std.102",
			SenderID:       phone,
			Body:           "Message after window expired",
			Channel:        "whatsapp_cloud",
			EntryPointType: domain.EntryPointStandard,
			At:             tAfterExpiry,
		})
		if err != nil {
			t.Fatalf("Standard Inbound after expiry: %v", err)
		}
		if rStdAfter.Chat.EntryPointType != domain.EntryPointStandard {
			t.Fatalf("expected entry_point_type reset to 'standard' after expiry, got %q", rStdAfter.Chat.EntryPointType)
		}
	})

	t.Run("ChatAt is strictly read-only and campaign/template dispatch to new number does not open window", func(t *testing.T) {
		unknownPhone := "+5511933334444"

		// 1. Snapshot(ChatAt) for unknown phone never creates a Contact or Chat
		_, exists, err := rec.Snapshot(ctx, wsID, activity.ChatAt(connA, unknownPhone).WithChannel("whatsapp_cloud"))
		if err != nil || exists {
			t.Fatalf("expected exists=false err=nil, got exists=%v err=%v", exists, err)
		}

		var count int
		_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM contact_identities WHERE workspace_id = $1 AND sender_identity = $2`, wsID, unknownPhone).Scan(&count)
		if count != 0 {
			t.Fatalf("expected 0 contact_identities created by read-only ChatAt, got %d", count)
		}

		// 2. Template Outbound via ChatOf creates Chat and sets last_outbound_at, but does NOT open service window
		tmplPhone := "+5511944445555"
		tmplContactID := seedContactWithIdentity(t, ctx, pool, wsID, "whatsapp_cloud", tmplPhone)

		tmplOut, err := rec.Outbound(ctx, wsID, activity.OutboundTurn{
			Chat:         activity.ChatOf(connA, tmplContactID).WithChannel("whatsapp_cloud"),
			UID:          "uid-tmpl-new-num",
			SenderType:   domain.SenderTypeAIAgent,
			Channel:      "whatsapp_cloud",
			TemplateName: "welcome_offer",
			Language:     "pt_BR",
			MessageType:  "template",
			At:           t0,
		})
		if err != nil {
			t.Fatalf("template Outbound failed: %v", err)
		}
		if tmplOut.Chat.ServiceWindowExpiresAt != nil {
			t.Fatalf("expected template dispatch not to open window, got %v", tmplOut.Chat.ServiceWindowExpiresAt)
		}
		if tmplOut.Chat.LastOutboundAt == nil || !tmplOut.Chat.LastOutboundAt.Equal(t0) {
			t.Fatalf("expected LastOutboundAt=%v, got %v", t0, tmplOut.Chat.LastOutboundAt)
		}

		// Subsequent freeform Outbound to that Chat is rejected because window is still closed
		_, err = rec.Outbound(ctx, wsID, activity.OutboundTurn{
			Chat:       activity.ChatID(tmplOut.Chat.ID),
			UID:        "uid-freeform-after-tmpl",
			SenderType: domain.SenderTypeAIAgent,
			Channel:    "whatsapp_cloud",
			Body:       "Freeform without customer reply",
			At:         t0.Add(5 * time.Minute),
		})
		var tmplErr *domain.TemplateRequiredError
		if !errors.As(err, &tmplErr) {
			t.Fatalf("expected *domain.TemplateRequiredError after template-only initiation, got %T: %v", err, err)
		}

		// Second template Outbound on existing Chat updates last_outbound_at, and Inbound exposes PrevLastOutboundAt
		tSecond := t0.Add(10 * time.Minute)
		if _, err := rec.Outbound(ctx, wsID, activity.OutboundTurn{
			Chat:         activity.ChatOf(connA, tmplContactID).WithChannel("whatsapp_cloud"),
			UID:          "uid-tmpl-new-num-2",
			SenderType:   domain.SenderTypeSystem,
			Channel:      "whatsapp_cloud",
			TemplateName: "welcome_offer",
			Language:     "pt_BR",
			MessageType:  "template",
			At:           tSecond,
		}); err != nil {
			t.Fatalf("second template Outbound on existing chat failed: %v", err)
		}
		inAfterOut, err := rec.Inbound(ctx, wsID, activity.InboundTurn{
			Chat:     activity.ChatOf(connA, tmplContactID).WithChannel("whatsapp_cloud"),
			UID:      "wamid.reply.after.touch",
			SenderID: tmplPhone,
			Body:     "Customer replied!",
			Channel:  "whatsapp_cloud",
			At:       tSecond.Add(3 * time.Second),
		})
		if err != nil {
			t.Fatalf("Inbound after Outbound failed: %v", err)
		}
		if inAfterOut.PrevLastOutboundAt == nil || !inAfterOut.PrevLastOutboundAt.Equal(tSecond) {
			t.Fatalf("expected PrevLastOutboundAt=%v, got %v", tSecond, inAfterOut.PrevLastOutboundAt)
		}
	})

	t.Run("ClaimExpiring claims whatsapp_cloud chat once per window and Inbound resets claim", func(t *testing.T) {
		// Clean any prior expiring chats in other workspaces by using a specific horizon
		baseNow := time.Date(2030, 5, 1, 12, 0, 0, 0, time.UTC)
		expPhone := "+5511955556666"
		expContactID := seedContactWithIdentity(t, ctx, pool, wsID, "whatsapp_cloud", expPhone)

		// Inbound at baseNow - 23h30m -> window expires at baseNow + 30m (within 1h horizon)
		inRes, err := rec.Inbound(ctx, wsID, activity.InboundTurn{
			Chat:     activity.ChatOf(connA, expContactID).WithChannel("whatsapp_cloud"),
			UID:      "wamid.exp.1",
			SenderID: expPhone,
			Body:     "Opening window",
			Channel:  "whatsapp_cloud",
			At:       baseNow.Add(-23*time.Hour - 30*time.Minute),
		})
		if err != nil {
			t.Fatalf("Inbound for expiring test: %v", err)
		}

		// Also create a WhatsApp Web chat in the same horizon -> must NOT have service_window_expires_at set or be claimed
		webPhone := "+5511966667777"
		webContactID := seedContactWithIdentity(t, ctx, pool, wsID, "whatsapp", webPhone)
		webRes, err := rec.Inbound(ctx, wsID, activity.InboundTurn{
			Chat:     activity.ChatOf(connWeb, webContactID).WithChannel("whatsapp"),
			UID:      "wamid.web.exp.1",
			SenderID: webPhone,
			Body:     "Web chat",
			Channel:  "whatsapp",
			At:       baseNow.Add(-23*time.Hour - 30*time.Minute),
		})
		if err != nil {
			t.Fatalf("Inbound on web chat: %v", err)
		}
		if webRes.Chat.ServiceWindowExpiresAt != nil {
			t.Fatalf("expected nil ServiceWindowExpiresAt on whatsapp (non-WABA) chat, got %v", webRes.Chat.ServiceWindowExpiresAt)
		}

		claimed, err := rec.ClaimExpiring(ctx, baseNow, 1*time.Hour, 50)
		if err != nil {
			t.Fatalf("ClaimExpiring 1 failed: %v", err)
		}
		var found *activity.ExpiringChat
		for i := range claimed {
			if claimed[i].WorkspaceID == wsID {
				if claimed[i].ChatID == inRes.Chat.ID {
					found = &claimed[i]
				} else {
					t.Fatalf("unexpected chat claimed in workspace: %+v", claimed[i])
				}
			}
		}
		if found == nil {
			t.Fatalf("expected chat %s to be claimed, got %+v", inRes.Chat.ID, claimed)
		}
		if found.ContactID != expContactID || found.ConnectionID != connA || found.RecipientPhone != expPhone || found.Channel != "whatsapp_cloud" {
			t.Fatalf("unexpected ExpiringChat fields: %+v", *found)
		}

		// Second ClaimExpiring call in same window must NOT re-claim
		claimed2, err := rec.ClaimExpiring(ctx, baseNow.Add(time.Minute), 1*time.Hour, 50)
		if err != nil {
			t.Fatalf("ClaimExpiring 2 failed: %v", err)
		}
		for _, c := range claimed2 {
			if c.ChatID == inRes.Chat.ID {
				t.Fatalf("expected chat %s not to be claimed a second time in same window", inRes.Chat.ID)
			}
		}

		// New Inbound turn extends window and resets notified_expiring_at = NULL
		newInAt := baseNow.Add(10 * time.Minute)
		_, err = rec.Inbound(ctx, wsID, activity.InboundTurn{
			Chat:     activity.ChatID(inRes.Chat.ID),
			UID:      "wamid.exp.2",
			SenderID: expPhone,
			Body:     "Renewing window",
			Channel:  "whatsapp_cloud",
			At:       newInAt,
		})
		if err != nil {
			t.Fatalf("Inbound renewal failed: %v", err)
		}

		// 23h30m after newInAt, ClaimExpiring claims the renewed window again
		futureNow := newInAt.Add(23*time.Hour + 30*time.Minute)
		claimed3, err := rec.ClaimExpiring(ctx, futureNow, 1*time.Hour, 50)
		if err != nil {
			t.Fatalf("ClaimExpiring 3 failed: %v", err)
		}
		renewedFound := false
		for _, c := range claimed3 {
			if c.ChatID == inRes.Chat.ID {
				renewedFound = true
			}
		}
		if !renewedFound {
			t.Fatalf("expected renewed window for chat %s to be claimed again", inRes.Chat.ID)
		}

		// Every workspace-scoped call rejects uuid.Nil workspace even when ChatID is provided
		refByID := activity.ChatID(inRes.Chat.ID)
		if _, err := rec.Inbound(ctx, uuid.Nil, activity.InboundTurn{Chat: refByID, UID: "nil-ws-in"}); !errors.Is(err, activity.ErrInvalidWorkspaceID) {
			t.Fatalf("Inbound with nil ws: got %v, want ErrInvalidWorkspaceID", err)
		}
		if _, err := rec.Outbound(ctx, uuid.Nil, activity.OutboundTurn{Chat: refByID, UID: "nil-ws-out"}); !errors.Is(err, activity.ErrInvalidWorkspaceID) {
			t.Fatalf("Outbound with nil ws: got %v, want ErrInvalidWorkspaceID", err)
		}
		if _, err := rec.SetAI(ctx, uuid.Nil, refByID, activity.AIOff()); !errors.Is(err, activity.ErrInvalidWorkspaceID) {
			t.Fatalf("SetAI with nil ws: got %v, want ErrInvalidWorkspaceID", err)
		}
		if err := rec.DispatchFailed(ctx, uuid.Nil, "wamid.exp.2"); !errors.Is(err, activity.ErrInvalidWorkspaceID) {
			t.Fatalf("DispatchFailed with nil ws: got %v, want ErrInvalidWorkspaceID", err)
		}
		if _, _, err := rec.Snapshot(ctx, uuid.Nil, refByID); !errors.Is(err, activity.ErrInvalidWorkspaceID) {
			t.Fatalf("Snapshot with nil ws: got %v, want ErrInvalidWorkspaceID", err)
		}
	})
}

func TestStatusPredicate_ParityWithDomainEffectiveStatus(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	past := now.Add(-30 * time.Minute)
	exact := now
	future := now.Add(30 * time.Minute)

	statuses := []domain.ChatStatus{
		domain.ChatStatusOpen,
		domain.ChatStatusClosed,
		domain.ChatStatusSnoozed,
	}
	snoozeTimes := []*time.Time{nil, &past, &exact, &future}
	targetFilters := []domain.ChatStatus{
		domain.ChatStatusOpen,
		domain.ChatStatusClosed,
		domain.ChatStatusSnoozed,
	}

	effExpr := activity.EffectiveStatusSQL("$1::text", "$2::timestamptz", "$3::timestamptz")
	predExpr := activity.StatusPredicate("$1::text", "$2::timestamptz", "$4::text", "$3::timestamptz")
	query := "SELECT " + effExpr + ", " + predExpr

	for _, st := range statuses {
		for _, sn := range snoozeTimes {
			wantEff := domain.EffectiveStatus(st, sn, now)
			for _, target := range targetFilters {
				var gotEff string
				var gotMatch bool
				err := pool.QueryRow(ctx, query, string(st), sn, now, string(target)).Scan(&gotEff, &gotMatch)
				if err != nil {
					t.Fatalf("query parity for status=%q snoozed=%v target=%q: %v", st, sn, target, err)
				}
				if domain.ChatStatus(gotEff) != wantEff {
					t.Fatalf("EffectiveStatusSQL(%q, %v) = %q, want %q", st, sn, gotEff, wantEff)
				}
				wantMatch := wantEff == target
				if gotMatch != wantMatch {
					t.Fatalf("StatusPredicate(%q, %v, target=%q) = %v, want %v (effective=%q)", st, sn, target, gotMatch, wantMatch, wantEff)
				}
			}
		}
	}
}


