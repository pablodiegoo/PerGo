package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/pablojhp.pergo/internal/api/handler"
	"github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/chat/activity"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/repository"
)

type fakePublisher struct {
	published []struct {
		subject string
		data    []byte
		traceID string
	}
}

func (f *fakePublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	f.published = append(f.published, struct {
		subject string
		data    []byte
		traceID string
	}{subject, data, traceID})
	return nil
}

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	ctx := context.Background()
	dsn := os.Getenv("PERGO_DATABASE_URL")
	if dsn == "" {
		pgC, err := tcpostgres.Run(ctx,
			"postgres:16-alpine",
			tcpostgres.WithDatabase("pergo_test"),
			tcpostgres.WithUsername("pergo"),
			tcpostgres.WithPassword("secret"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).
					WithStartupTimeout(30*time.Second),
			),
		)
		if err != nil {
			t.Fatalf("start postgres: %v", err)
		}
		t.Cleanup(func() { _ = pgC.Terminate(context.Background()) })

		dsn, err = pgC.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			t.Fatalf("conn string: %v", err)
		}
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}

	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("failed to wrap pool as sql.DB: %v", err)
	}
	defer db.Close()

	if err := postgres.RunMigrations(db); err != nil {
		pool.Close()
		t.Fatalf("failed to run migrations: %v", err)
	}

	return pool
}

func TestChatwootWebhookAuth(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	wsRepo := repository.NewWorkspaceRepository(pool)
	apiKeyRepo := repository.NewAPIKeyRepository(pool)
	mappingRepo := repository.NewChatwootMappingRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	pub := &fakePublisher{}

	ws, err := wsRepo.Create(ctx, "chatwoot_webhook_auth_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	_, rawKey, err := apiKeyRepo.Create(ctx, ws.ID, "Test Key")
	if err != nil {
		t.Fatalf("failed to create API Key: %v", err)
	}

	h := handler.NewChatwootWebhookHandler(pool, mappingRepo, contactRepo, pub)

	t.Run("ValidTokenQueryParam_Returns200", func(t *testing.T) {
		e := echo.New()
		e.Use(middleware.AuthMiddleware(apiKeyRepo))
		e.POST("/api/integrations/chatwoot", h.Handle)

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/integrations/chatwoot?token=%s", rawKey), strings.NewReader(`{}`))
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("InvalidTokenQueryParam_Returns401", func(t *testing.T) {
		e := echo.New()
		e.Use(middleware.AuthMiddleware(apiKeyRepo))
		e.POST("/api/integrations/chatwoot", h.Handle)

		req := httptest.NewRequest(http.MethodPost, "/api/integrations/chatwoot?token=invalidkey12345", nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("MissingTokenQueryParam_Returns401", func(t *testing.T) {
		e := echo.New()
		e.Use(middleware.AuthMiddleware(apiKeyRepo))
		e.POST("/api/integrations/chatwoot", h.Handle)

		req := httptest.NewRequest(http.MethodPost, "/api/integrations/chatwoot", nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestChatwootWebhookHandler_Integration(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	wsRepo := repository.NewWorkspaceRepository(pool)
	apiKeyRepo := repository.NewAPIKeyRepository(pool)
	mappingRepo := repository.NewChatwootMappingRepository(pool)
	contactRepo := repository.NewContactRepository(pool)

	ws, err := wsRepo.Create(ctx, "chatwoot_webhook_integration_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	_, rawKey, err := apiKeyRepo.Create(ctx, ws.ID, "Test Key")
	if err != nil {
		t.Fatalf("failed to create API Key: %v", err)
	}

	kek := make([]byte, 32)
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	connRepo := repository.NewConnectionRepository(pool, enc)

	contact, err := contactRepo.ResolveContact(ctx, ws.ID, "telegram", "contact_tg_123", "Test Cust", "testcust", "+123")
	if err != nil {
		t.Fatalf("failed to resolve contact: %v", err)
	}

	senderIdentity := "@my_bot_" + uuid.New().String()
	connectionID := uuid.New()
	conn := &repository.Connection{
		ID:             connectionID,
		WorkspaceID:    ws.ID,
		Name:           "Test Connection",
		Channel:        "telegram",
		SenderIdentity: senderIdentity,
		Credentials:    []byte(`{}`),
	}
	if err := connRepo.Create(ctx, conn); err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}

	connB := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "Second Connection",
		Channel:        "telegram",
		SenderIdentity: "@my_second_bot_" + uuid.New().String(),
		Credentials:    []byte(`{}`),
	}
	if err := connRepo.Create(ctx, connB); err != nil {
		t.Fatalf("failed to create second connection: %v", err)
	}

	chatRepo := repository.NewChatRepository(pool)
	if _, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &connectionID, contact.ID); err != nil {
		t.Fatalf("failed to create chat A: %v", err)
	}
	if _, err := chatRepo.FindOrCreateChat(ctx, ws.ID, &connB.ID, contact.ID); err != nil {
		t.Fatalf("failed to create chat B: %v", err)
	}

	mapping := &repository.ChatwootMapping{
		WorkspaceID:            ws.ID,
		ContactID:              contact.ID,
		ConnectionID:           connectionID,
		ChatwootContactID:      101,
		ChatwootConversationID: 202,
		Channel:                "telegram",
		SenderIdentity:         senderIdentity,
	}
	if err := mappingRepo.Upsert(ctx, mapping); err != nil {
		t.Fatalf("failed to upsert mapping: %v", err)
	}

	pub := &fakePublisher{}
	actRec := activity.New(pool, pub, nil, nil)
	h := handler.NewChatwootWebhookHandler(pool, mappingRepo, contactRepo, pub)

	e := echo.New()
	e.Use(middleware.AuthMiddleware(apiKeyRepo))
	e.POST("/api/integrations/chatwoot", h.Handle)

	t.Run("IgnoreNonUserOutgoingMessages", func(t *testing.T) {
		payload := `{
			"event": "message_created",
			"message_type": "incoming",
			"private": false,
			"content": "hello from customer",
			"sender": {
				"type": "contact"
			},
			"conversation": {
				"id": 202,
				"inbox_id": 1
			}
		}`

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/integrations/chatwoot?token=%s", rawKey), strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", rec.Code)
		}
		if len(pub.published) != 0 {
			t.Errorf("expected 0 published messages, got %d", len(pub.published))
		}
	})

	t.Run("IgnorePrivateNotes", func(t *testing.T) {
		payload := `{
			"event": "message_created",
			"message_type": "outgoing",
			"private": true,
			"content": "private staff note",
			"sender": {
				"type": "user"
			},
			"conversation": {
				"id": 202,
				"inbox_id": 1
			}
		}`

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/integrations/chatwoot?token=%s", rawKey), strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", rec.Code)
		}
		if len(pub.published) != 0 {
			t.Errorf("expected 0 published messages, got %d", len(pub.published))
		}
	})

	t.Run("ProcessValidAgentReplyAndPublish", func(t *testing.T) {
		pub.published = nil // Reset

		payload := `{
			"event": "message_created",
			"message_type": "outgoing",
			"private": false,
			"content": "hello customer, this is agent",
			"sender": {
				"type": "user"
			},
			"conversation": {
				"id": 202,
				"inbox_id": 1
			}
		}`

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/integrations/chatwoot?token=%s", rawKey), strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		// Expect messages.outbound AND handoff events published
		var outboundPub *struct {
			subject string
			data    []byte
			traceID string
		}
		for i := range pub.published {
			if pub.published[i].subject == "messages.outbound" {
				outboundPub = &pub.published[i]
				break
			}
		}
		if outboundPub == nil {
			t.Fatalf("expected messages.outbound published message, got %+v", pub.published)
		}

		var queueMsg domain.QueueMessage
		if err := json.Unmarshal(outboundPub.data, &queueMsg); err != nil {
			t.Fatalf("failed to unmarshal QueueMessage: %v", err)
		}

		if queueMsg.WorkspaceID != ws.ID {
			t.Errorf("expected workspace ID %s, got %s", ws.ID, queueMsg.WorkspaceID)
		}
		if queueMsg.ConnectionID != connectionID {
			t.Errorf("expected connection ID %s, got %s", connectionID, queueMsg.ConnectionID)
		}
		if queueMsg.SenderIdentity != senderIdentity {
			t.Errorf("expected sender identity %q, got %q", senderIdentity, queueMsg.SenderIdentity)
		}
		if queueMsg.To != "contact_tg_123" {
			t.Errorf("expected recipient (customer address) contact_tg_123, got %q", queueMsg.To)
		}
		if queueMsg.Channel != "telegram" {
			t.Errorf("expected channel telegram, got %q", queueMsg.Channel)
		}
		if queueMsg.Body != "hello customer, this is agent" {
			t.Errorf("expected body 'hello customer, this is agent', got %q", queueMsg.Body)
		}

		// Verify ChatMessage was recorded via OutboundProcessor.Ingest with SenderTypeHumanAgent
		chatMsg, err := chatRepo.GetChatMessageByUID(ctx, ws.ID, queueMsg.TraceID)
		if err != nil || chatMsg == nil {
			t.Fatalf("expected ChatMessage persisted for Chatwoot agent reply, got err=%v msg=%v", err, chatMsg)
		}
		if chatMsg.SenderType != domain.SenderTypeHumanAgent {
			t.Errorf("expected SenderType %q, got %q", domain.SenderTypeHumanAgent, chatMsg.SenderType)
		}
		if chatMsg.SenderName != "Chatwoot Agent" {
			t.Errorf("expected SenderName 'Chatwoot Agent', got %q", chatMsg.SenderName)
		}

		// Verify Chat on connection A has AI paused (~12h) while Chat on connection B for the same Contact remains IsEngaged()
		now := time.Now().UTC()
		snapA, _, err := actRec.Snapshot(ctx, ws.ID, activity.ChatOf(connectionID, contact.ID))
		if err != nil {
			t.Fatalf("Snapshot chat A failed: %v", err)
		}
		if snapA.AI.Effective(now).IsEngaged() {
			t.Errorf("expected chat A AI not engaged after Chatwoot reply, got %+v", snapA.AI)
		}
		if snapA.AI.Mode != domain.AIModePaused || snapA.AI.PausedUntil == nil {
			t.Errorf("expected chat A AI mode=paused with PausedUntil set, got %+v", snapA.AI)
		}

		snapB, _, err := actRec.Snapshot(ctx, ws.ID, activity.ChatOf(connB.ID, contact.ID))
		if err != nil {
			t.Fatalf("Snapshot chat B failed: %v", err)
		}
		if !snapB.AI.Effective(now).IsEngaged() {
			t.Errorf("expected chat B AI for same contact to remain engaged, got %+v", snapB.AI)
		}
	})
}
