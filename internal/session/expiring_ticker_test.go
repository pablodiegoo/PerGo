package session

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pablojhp.pergo/internal/chat/activity"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

type mockPublisher struct {
	published []struct {
		subject string
		data    []byte
		traceID string
	}
}

func (m *mockPublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	m.published = append(m.published, struct {
		subject string
		data    []byte
		traceID string
	}{subject: subject, data: data, traceID: traceID})
	return nil
}

func setupTickerTestPool(t *testing.T) *pgxpool.Pool {
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
		t.Fatalf("failed to connect to postgres: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		t.Fatalf("failed to wrap pool: %v", err)
	}
	defer db.Close()

	if err := postgres.RunMigrations(db); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}
	return pool
}

func TestSessionTicker_CheckExpiringSessions(t *testing.T) {
	pool := setupTickerTestPool(t)
	ctx := context.Background()

	wsID := uuid.New()
	connID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id, name) VALUES ($1, $2)`, wsID, "ticker-ws"); err != nil {
		t.Fatalf("seed ws: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO connections (id, workspace_id, name, slug, channel, sender_identity, status) VALUES ($1, $2, 'WABA', $3, 'whatsapp_cloud', $4, 'connected')`, connID, wsID, "waba-"+connID.String()[:8], "+1555"+connID.String()[:7]); err != nil {
		t.Fatalf("seed conn: %v", err)
	}

	rec := activity.New(pool, nil, nil, nil)
	contactRepo := repository.NewContactRepository(pool)
	now := time.Now().UTC().Truncate(time.Second)

	// 1. Standard session inbound 23h5m ago -> expires in 55m (within 1h horizon)
	phoneStd := "+5511999990001"
	cStd, err := contactRepo.ResolveContact(ctx, wsID, "whatsapp_cloud", phoneStd, "Std User", "", phoneStd)
	if err != nil {
		t.Fatalf("ResolveContact std failed: %v", err)
	}
	inStd, err := rec.Inbound(ctx, wsID, activity.InboundTurn{
		Chat:           activity.ChatOf(connID, cStd.ID).WithChannel("whatsapp_cloud"),
		UID:            "in-std-1",
		SenderName:     "Std User",
		SenderID:       phoneStd,
		Body:           "hi std",
		Channel:        "whatsapp_cloud",
		EntryPointType: domain.EntryPointStandard,
		At:             now.Add(-23*time.Hour - 5*time.Minute),
	})
	if err != nil {
		t.Fatalf("Inbound std failed: %v", err)
	}

	// 2. CTWA session inbound 23h5m ago -> expires in 55m (within 1h horizon)
	phoneCTWA := "+5511999990002"
	cCTWA, err := contactRepo.ResolveContact(ctx, wsID, "whatsapp_cloud", phoneCTWA, "CTWA User", "", phoneCTWA)
	if err != nil {
		t.Fatalf("ResolveContact ctwa failed: %v", err)
	}
	inCTWA, err := rec.Inbound(ctx, wsID, activity.InboundTurn{
		Chat:           activity.ChatOf(connID, cCTWA.ID).WithChannel("whatsapp_cloud"),
		UID:            "in-ctwa-1",
		SenderName:     "CTWA User",
		SenderID:       phoneCTWA,
		Body:           "hi ctwa",
		Channel:        "whatsapp_cloud",
		EntryPointType: domain.EntryPointCTWA,
		At:             now.Add(-23*time.Hour - 5*time.Minute),
	})
	if err != nil {
		t.Fatalf("Inbound ctwa failed: %v", err)
	}

	pub := &mockPublisher{}
	ticker := NewSessionTicker(rec, pub)
	ticker.CheckExpiringSessionsAt(ctx, now)

	foundByChat := make(map[string]ExpiringSessionEvent)
	for _, p := range pub.published {
		if p.subject != "webhooks.events" {
			t.Errorf("got subject %s, want webhooks.events", p.subject)
		}
		var evt ExpiringSessionEvent
		if err := json.Unmarshal(p.data, &evt); err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}
		foundByChat[evt.ChatID] = evt
	}

	evtStd, ok := foundByChat[inStd.Chat.ID.String()]
	if !ok {
		t.Fatalf("expected expiring event for standard chat %s, got %+v", inStd.Chat.ID, foundByChat)
	}
	if evtStd.Event != "session.expiring_soon" || evtStd.WorkspaceID != wsID.String() || evtStd.ContactID != inStd.Chat.ContactID.String() || evtStd.ConnectionID != connID.String() || evtStd.RecipientPhone != phoneStd || evtStd.EntryPointType != "standard" {
		t.Fatalf("unexpected standard event: %+v", evtStd)
	}

	evtCTWA, ok := foundByChat[inCTWA.Chat.ID.String()]
	if !ok {
		t.Fatalf("expected expiring event for CTWA chat %s, got %+v", inCTWA.Chat.ID, foundByChat)
	}
	if evtCTWA.EntryPointType != "ctwa" || evtCTWA.RecipientPhone != phoneCTWA {
		t.Fatalf("unexpected CTWA event: %+v", evtCTWA)
	}

	// Second tick does not re-emit
	prevCount := len(pub.published)
	ticker.CheckExpiringSessionsAt(ctx, now.Add(time.Minute))
	for _, p := range pub.published[prevCount:] {
		var evt ExpiringSessionEvent
		_ = json.Unmarshal(p.data, &evt)
		if evt.ChatID == inStd.Chat.ID.String() || evt.ChatID == inCTWA.Chat.ID.String() {
			t.Fatalf("expected chat %s not to be re-emitted on second tick", evt.ChatID)
		}
	}
}
