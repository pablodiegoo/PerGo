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

	"github.com/pablojhp.pergo/internal/api/handler"
	"github.com/pablojhp.pergo/internal/api/middleware"
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

	dsn := os.Getenv("PERGO_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("PostgreSQL not available at %s: %v", dsn, err)
	}

	err = pool.Ping(ctx)
	if err != nil {
		pool.Close()
		t.Skipf("PostgreSQL ping failed at %s: %v", dsn, err)
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

	h := handler.NewChatwootWebhookHandler(pool, mappingRepo, pub)

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

	connectionID := uuid.New()
	conn := &repository.Connection{
		ID:             connectionID,
		WorkspaceID:    ws.ID,
		Name:           "Test Connection",
		Channel:        "telegram",
		SenderIdentity: "@my_bot",
		Credentials:    []byte(`{}`),
	}
	if err := connRepo.Create(ctx, conn); err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}

	mapping := &repository.ChatwootMapping{
		WorkspaceID:            ws.ID,
		ContactID:              contact.ID,
		ConnectionID:           connectionID,
		ChatwootContactID:      101,
		ChatwootConversationID: 202,
		Channel:                "telegram",
		SenderIdentity:         "@my_bot",
	}
	if err := mappingRepo.Upsert(ctx, mapping); err != nil {
		t.Fatalf("failed to upsert mapping: %v", err)
	}

	pub := &fakePublisher{}
	h := handler.NewChatwootWebhookHandler(pool, mappingRepo, pub)

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

		if len(pub.published) != 1 {
			t.Fatalf("expected 1 published message, got %d", len(pub.published))
		}

		pubMsg := pub.published[0]
		if pubMsg.subject != "messages.outbound" {
			t.Errorf("expected subject messages.outbound, got %q", pubMsg.subject)
		}

		var queueMsg domain.QueueMessage
		if err := json.Unmarshal(pubMsg.data, &queueMsg); err != nil {
			t.Fatalf("failed to unmarshal QueueMessage: %v", err)
		}

		if queueMsg.WorkspaceID != ws.ID {
			t.Errorf("expected workspace ID %s, got %s", ws.ID, queueMsg.WorkspaceID)
		}
		if queueMsg.ConnectionID != connectionID {
			t.Errorf("expected connection ID %s, got %s", connectionID, queueMsg.ConnectionID)
		}
		if queueMsg.SenderIdentity != "@my_bot" {
			t.Errorf("expected sender identity @my_bot, got %q", queueMsg.SenderIdentity)
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
	})
}
