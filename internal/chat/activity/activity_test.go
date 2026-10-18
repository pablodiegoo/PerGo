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
	if chatID == uuid.Nil {
		t.Fatal("expected valid ChatID")
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

	// Create Chat A (connA) and Chat B (connB) for the same contact via AI outbound turns
	resA, err := rec.Outbound(ctx, wsID, activity.OutboundTurn{
		Chat:       activity.ChatAt(connA, phone),
		UID:        "uid-connA-init",
		SenderType: domain.SenderTypeAIAgent,
		Channel:    "whatsapp_cloud",
		To:         phone,
		Body:       "Welcome on Line A",
		At:         t0,
	})
	if err != nil {
		t.Fatalf("init Chat A: %v", err)
	}
	resB, err := rec.Outbound(ctx, wsID, activity.OutboundTurn{
		Chat:       activity.ChatAt(connB, phone),
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

	// Create chat via AI outbound
	initRes, err := rec.Outbound(ctx, wsID, activity.OutboundTurn{
		Chat:       activity.ChatAt(connID, phone),
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

	// 5. StrictServiceWindow enforcement on expired WABA window
	expiredAt := t0.Add(-1 * time.Hour)
	_, err = pool.Exec(ctx, `UPDATE chats SET service_window_expires_at = $3 WHERE workspace_id = $1 AND id = $2`, wsID, chatID, expiredAt)
	if err != nil {
		t.Fatalf("set expired window: %v", err)
	}
	_, err = rec.Outbound(ctx, wsID, activity.OutboundTurn{
		Chat:                activity.ChatID(chatID),
		UID:                 "uid-blocked-window",
		SenderType:          domain.SenderTypeHumanAgent,
		Channel:             "whatsapp_cloud",
		Body:                "Blocked freeform",
		StrictServiceWindow: true,
		At:                  t0,
	})
	if !errors.Is(err, activity.ErrServiceWindowClosed) {
		t.Fatalf("expected ErrServiceWindowClosed, got %v", err)
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


