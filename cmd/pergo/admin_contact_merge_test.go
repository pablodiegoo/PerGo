package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"
	"github.com/nats-io/nats.go"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/pablojhp.pergo/internal/api/handler/admin"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/platform/audit"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/platform/queue"
	"github.com/pablojhp.pergo/internal/repository"
)

var (
	integrationDBURL   string
	integrationNATSURL string
)

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// 1. Start PostgreSQL Container
	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("pergo"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
	)
	if err != nil {
		log.Fatalf("failed to start postgres container: %v", err)
	}
	defer func() {
		if err := pgContainer.Terminate(context.Background()); err != nil {
			log.Printf("failed to terminate postgres container: %v", err)
		}
	}()

	pgConnStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Fatalf("failed to get postgres connection string: %v", err)
	}
	integrationDBURL = pgConnStr
	os.Setenv("PERGO_TEST_DSN", pgConnStr)
	os.Setenv("PERGO_DATABASE_URL", pgConnStr)

	// Connect to pool with retries to ensure Postgres is fully ready
	var pool *pgxpool.Pool
	for i := 0; i < 10; i++ {
		pool, err = pgxpool.New(ctx, pgConnStr)
		if err == nil {
			err = pool.Ping(ctx)
			if err == nil {
				break
			}
			pool.Close()
		}
		log.Printf("waiting for postgres to accept connections (attempt %d/10)... error: %v", i+1, err)
		time.Sleep(1 * time.Second)
	}
	if err != nil {
		log.Fatalf("postgres failed to accept connections after retries: %v", err)
	}

	// Run migrations
	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		pool.Close()
		log.Fatalf("failed to get sql.DB wrapper: %v", err)
	}
	if err := postgres.RunMigrations(db); err != nil {
		db.Close()
		pool.Close()
		log.Fatalf("failed to run migrations: %v", err)
	}
	db.Close()
	pool.Close()

	// 2. Start NATS Container with JetStream enabled
	natsContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "nats:2.10-alpine",
			ExposedPorts: []string{"4222/tcp"},
			Cmd:          []string{"-js"},
			WaitingFor:   wait.ForListeningPort("4222/tcp"),
		},
		Started: true,
	})
	if err != nil {
		log.Fatalf("failed to start nats container: %v", err)
	}
	defer func() {
		if err := natsContainer.Terminate(context.Background()); err != nil {
			log.Printf("failed to terminate nats container: %v", err)
		}
	}()

	natsHost, err := natsContainer.Host(ctx)
	if err != nil {
		log.Fatalf("failed to get nats host: %v", err)
	}
	natsPort, err := natsContainer.MappedPort(ctx, "4222/tcp")
	if err != nil {
		log.Fatalf("failed to get nats port: %v", err)
	}
	natsURL := fmt.Sprintf("nats://%s:%s", natsHost, natsPort.Port())
	integrationNATSURL = natsURL
	os.Setenv("PERGO_NATS_URL", natsURL)

	os.Exit(m.Run())
}

func TestAdminContactMerge(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("skipping: no PostgreSQL available")
	}

	ctx := t.Context()
	wsRepo := repository.NewWorkspaceRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	recipientSessionRepo := repository.NewRecipientSessionRepository(pool)
	auditRepo := repository.NewAuditRepository(pool)
	userActionLogRepo := repository.NewUserActionLogRepository(pool)
	dedupRepo := repository.NewInboundDedupRepository(pool)

	// Create workspace
	ws, err := wsRepo.Create(ctx, "Contact Merge Integration Workspace")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	// Connect to NATS
	nc, err := nats.Connect(integrationNATSURL)
	if err != nil {
		t.Fatalf("failed to connect to NATS: %v", err)
	}
	defer nc.Close()

	// Ensure NATS streams
	_, err = queue.EnsureStream(ctx, nc)
	if err != nil {
		t.Fatalf("failed to ensure MESSAGES stream: %v", err)
	}
	_, err = queue.EnsureWebhookStream(ctx, nc)
	if err != nil {
		t.Fatalf("failed to ensure WEBHOOKS stream: %v", err)
	}
	_, err = queue.EnsureInboundStream(ctx, nc)
	if err != nil {
		t.Fatalf("failed to ensure INBOUND stream: %v", err)
	}

	// Create inbound processor and audit writer
	auditWriter := audit.NewWriter(pool, 100, 1)
	var auditClosed bool
	defer func() {
		if !auditClosed {
			_ = auditWriter.Close()
		}
	}()

	publisher := queue.NewJetStreamPublisher(nc)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)
	proc := inbound.NewInboundProcessor(dedupRepo, wsRepo, nil, publisher, auditWriter, recipientSessionRepo, contactRepo, dispatchRepo)

	// 1. Simulate inbound events to create distinct contacts
	// Message from Telegram contact
	telMsgID := "tel-msg-101"
	telEvent := &inbound.InboundEvent{
		WorkspaceID: ws.ID,
		MessageID:   telMsgID,
		Channel:     "telegram",
		From:        "chat_tg_123",
		To:          "bot_username",
		Body:        "Telegram Hello",
		SenderName:  "Telegram User One",
		Metadata:    map[string]string{"username": "tg_user_123"},
	}
	err = proc.Process(ctx, telEvent)
	if err != nil {
		t.Fatalf("Process Telegram event failed: %v", err)
	}

	// Message from WhatsApp contact
	waMsgID := "wa-msg-202"
	waEvent := &inbound.InboundEvent{
		WorkspaceID: ws.ID,
		MessageID:   waMsgID,
		Channel:     "whatsapp",
		From:        "+5511999998888",
		To:          "+551130004000",
		Body:        "WhatsApp Hello",
		SenderName:  "WhatsApp User Two",
	}
	err = proc.Process(ctx, waEvent)
	if err != nil {
		t.Fatalf("Process WhatsApp event failed: %v", err)
	}

	// Flush audit logs
	err = auditWriter.Close()
	auditClosed = true
	if err != nil {
		t.Fatalf("Close audit writer: %v", err)
	}

	// Retrieve both contacts and verify distinct IDs
	tgContact, err := contactRepo.ResolveContact(ctx, ws.ID, "telegram", "chat_tg_123", "Telegram User One", "@tg_user_123", "")
	if err != nil {
		t.Fatalf("resolve telegram contact: %v", err)
	}
	waContact, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp", "+5511999998888", "WhatsApp User Two", "", "")
	if err != nil {
		t.Fatalf("resolve whatsapp contact: %v", err)
	}

	if tgContact.ID == waContact.ID {
		t.Fatalf("expected distinct contacts, got same ID: %s", tgContact.ID)
	}

	// Setup Echo server for testing the Handler
	e := echo.New()
	inboxHandler := &admin.InboxHandler{
		Repo:           auditRepo,
		Sessions:       recipientSessionRepo,
		Workspaces:     wsRepo,
		Connections:    nil,
		Publisher:      publisher,
		Templates:      nil,
		ContactRepo:    contactRepo,
		UserActionLogs: userActionLogRepo,
	}

	// POST /admin/contacts/merge
	e.POST("/admin/contacts/merge", inboxHandler.MergeContacts)
	// GET /admin/inbox/messages
	e.GET("/admin/inbox/messages", inboxHandler.PollMessages)

	// 2. Perform successful merge via handler POST request
	reqBody := ""
	mergeReq := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/contacts/merge?primary_id=%s&secondary_id=%s", tgContact.ID, waContact.ID), strings.NewReader(reqBody))
	mergeReq.AddCookie(&http.Cookie{Name: "pergo-active-workspace", Value: ws.ID.String()})
	mergeRec := httptest.NewRecorder()
	e.ServeHTTP(mergeRec, mergeReq)

	if mergeRec.Code != http.StatusOK {
		t.Fatalf("expected merge status 200, got %d: %s", mergeRec.Code, mergeRec.Body.String())
	}

	// Check HX-Location header to ensure it redirects back to primary contact chat
	loc := mergeRec.Header().Get("HX-Location")
	expectedLoc := fmt.Sprintf("/admin/inbox/chat?contact_id=%s", tgContact.ID)
	if loc != expectedLoc {
		t.Errorf("expected HX-Location %q, got %q", expectedLoc, loc)
	}

	// Verify secondary contact is deleted
	_, err = contactRepo.GetByID(ctx, ws.ID, waContact.ID)
	if err == nil || err.Error() != repository.ErrContactNotFound.Error() {
		t.Errorf("expected secondary contact to be deleted, got: %v", err)
	}

	// Verify primary contact has WhatsApp identity mapped
	updatedPrimary, err := contactRepo.GetByID(ctx, ws.ID, tgContact.ID)
	if err != nil {
		t.Fatalf("failed to retrieve updated primary: %v", err)
	}
	var hasWAIdentity bool
	for _, ident := range updatedPrimary.Identities {
		if ident.Channel == "whatsapp" && ident.SenderIdentity == "+5511999998888" {
			hasWAIdentity = true
		}
	}
	if !hasWAIdentity {
		t.Error("primary contact did not acquire secondary's WhatsApp identity")
	}

	// 3. Conversation thread unifies histories correctly under the primary contact ID
	pollReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/inbox/messages?contact_id=%s", tgContact.ID), nil)
	pollReq.AddCookie(&http.Cookie{Name: "pergo-active-workspace", Value: ws.ID.String()})
	pollRec := httptest.NewRecorder()
	e.ServeHTTP(pollRec, pollReq)

	if pollRec.Code != http.StatusOK {
		t.Fatalf("expected poll messages status 200, got %d: %s", pollRec.Code, pollRec.Body.String())
	}
	pollBody := pollRec.Body.String()
	if !strings.Contains(pollBody, "Telegram Hello") {
		t.Error("unified history did not contain primary contact message 'Telegram Hello'")
	}
	if !strings.Contains(pollBody, "WhatsApp Hello") {
		t.Error("unified history did not contain secondary contact message 'WhatsApp Hello'")
	}

	// 4. Check that error conditions (e.g. merging contacts from different workspaces) are rejected
	ws2, err := wsRepo.Create(ctx, "Workspace 2 for Contact Merge")
	if err != nil {
		t.Fatalf("failed to create workspace 2: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws2.ID)
	}()

	otherContact, err := contactRepo.ResolveContact(ctx, ws2.ID, "telegram", "chat_tg_999", "Other Workspace User", "", "")
	if err != nil {
		t.Fatalf("resolve other workspace contact: %v", err)
	}

	// Attempt merging otherContact (workspace 2) into primary (workspace 1)
	err = contactRepo.MergeContacts(ctx, ws.ID, tgContact.ID, otherContact.ID)
	if err == nil {
		t.Error("expected merge across different workspaces to fail, but it succeeded")
	}

	// 5. Verify transaction rollback on merge failures
	// Create another secondary contact to test rollback
	rollbackSec, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp", "+5511999991111", "Rollback Secondary", "", "")
	if err != nil {
		t.Fatalf("resolve rollback secondary: %v", err)
	}

	// Attempt merging with invalid workspace ID to force a database error / validation error
	invalidWS := uuid.New()
	err = contactRepo.MergeContacts(ctx, invalidWS, tgContact.ID, rollbackSec.ID)
	if err == nil {
		t.Error("expected merge with invalid workspace to fail")
	}

	// Verify rollbackSec still exists and is not deleted
	dbSec, err := contactRepo.GetByID(ctx, ws.ID, rollbackSec.ID)
	if err != nil {
		t.Fatalf("expected rollback secondary to still exist, but got error: %v", err)
	}
	if dbSec.ID != rollbackSec.ID {
		t.Errorf("mismatch rollback secondary: got %+v", dbSec)
	}
}
