package chatwoot_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/integration/chatwoot"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/repository"
)

// noOpCryptoProvider for testing configuration encryption
type noOpCryptoProvider struct{}

func (n noOpCryptoProvider) Encrypt(plaintext []byte) ([]byte, string, int, error) {
	return plaintext, "noop", 1, nil
}

func (n noOpCryptoProvider) Decrypt(ciphertext []byte) ([]byte, error) {
	return ciphertext, nil
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

func TestChatwootSyncer(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	wsRepo := repository.NewWorkspaceRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, noOpCryptoProvider{})
	contactRepo := repository.NewContactRepository(pool)
	integrationRepo := repository.NewIntegrationRepository(pool, noOpCryptoProvider{})
	mappingRepo := repository.NewChatwootMappingRepository(pool)

	ws, err := wsRepo.Create(ctx, "chatwoot_syncer_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	conn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "Telegram Connection",
		Channel:        "telegram",
		SenderIdentity: "@test_bot",
		Status:         "active",
	}
	if err := connRepo.Create(ctx, conn); err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}

	contact, err := contactRepo.ResolveContact(ctx, ws.ID, "telegram", "12345", "Test Customer", "test_cust", "+12345")
	if err != nil {
		t.Fatalf("failed to resolve contact: %v", err)
	}

	t.Run("SyncInboundMessage_NotMapped_CreatesEverything", func(t *testing.T) {
		var searchCount, createContactCount, createConvCount, postMessageCount int

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/v1/accounts/1/contacts/search":
				searchCount++
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"payload": []}`))

			case r.Method == http.MethodPost && r.URL.Path == "/api/v1/accounts/1/contacts":
				createContactCount++
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"payload": {"contact": {"id": 101}}}`))

			case r.Method == http.MethodPost && r.URL.Path == "/api/v1/accounts/1/conversations":
				createConvCount++
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id": 202}`))

			case r.Method == http.MethodPost && r.URL.Path == "/api/v1/accounts/1/conversations/202/messages":
				postMessageCount++
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id": 303}`))

			default:
				t.Errorf("unexpected API call: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusInternalServerError)
			}
		}))
		defer server.Close()

		cfg := map[string]interface{}{
			"api_url":      server.URL,
			"access_token": "test-token",
			"inbox_id":     int64(2),
			"account_id":   int64(1),
		}
		cfgBytes, _ := json.Marshal(cfg)
		integration := &repository.Integration{
			ID:          uuid.New(),
			WorkspaceID: ws.ID,
			Name:        "Chatwoot Test",
			Provider:    "chatwoot",
			Active:      true,
			Config:      cfgBytes,
		}
		if err := integrationRepo.Save(ctx, integration); err != nil {
			t.Fatalf("failed to save integration: %v", err)
		}

		syncer := chatwoot.NewChatwootSyncer(integrationRepo, mappingRepo, server.Client())

		ev := &inbound.InboundEvent{
			WorkspaceID:  ws.ID,
			ConnectionID: conn.ID,
			Channel:      "telegram",
			From:         "12345",
			To:           "@test_bot",
			Body:         "Hello syncer!",
		}

		err = syncer.SyncInboundMessage(ctx, contact, ev)
		if err != nil {
			t.Fatalf("SyncInboundMessage failed: %v", err)
		}

		if searchCount != 1 || createContactCount != 1 || createConvCount != 1 || postMessageCount != 1 {
			t.Errorf("unexpected call counts: search=%d, createContact=%d, createConv=%d, postMessage=%d",
				searchCount, createContactCount, createConvCount, postMessageCount)
		}

		mapping, err := mappingRepo.GetByContactAndConnection(ctx, ws.ID, contact.ID, conn.ID)
		if err != nil {
			t.Fatalf("failed to find mapping: %v", err)
		}
		if mapping.ChatwootContactID != 101 || mapping.ChatwootConversationID != 202 {
			t.Errorf("unexpected mapping values: %+v", mapping)
		}
	})

	t.Run("SyncInboundMessage_Mapped_PostsDirectly", func(t *testing.T) {
		var postMessageCount int

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodPost && r.URL.Path == "/api/v1/accounts/1/conversations/202/messages" {
				postMessageCount++
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id": 303}`))
			} else {
				t.Errorf("unexpected API call: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusInternalServerError)
			}
		}))
		defer server.Close()

		existingMapping := &repository.ChatwootMapping{
			WorkspaceID:            ws.ID,
			ContactID:              contact.ID,
			ConnectionID:           conn.ID,
			ChatwootContactID:      101,
			ChatwootConversationID: 202,
			Channel:                "telegram",
			SenderIdentity:         "@test_bot",
		}
		if err := mappingRepo.Upsert(ctx, existingMapping); err != nil {
			t.Fatalf("failed to upsert mapping: %v", err)
		}

		cfg := map[string]interface{}{
			"api_url":      server.URL,
			"access_token": "test-token",
			"inbox_id":     int64(2),
			"account_id":   int64(1),
		}
		cfgBytes, _ := json.Marshal(cfg)
		integration, err := integrationRepo.GetByProvider(ctx, ws.ID, "chatwoot")
		if err != nil {
			t.Fatalf("failed to get integration: %v", err)
		}
		integration.Config = cfgBytes
		if err := integrationRepo.Save(ctx, integration); err != nil {
			t.Fatalf("failed to save integration: %v", err)
		}

		syncer := chatwoot.NewChatwootSyncer(integrationRepo, mappingRepo, server.Client())

		ev := &inbound.InboundEvent{
			WorkspaceID:  ws.ID,
			ConnectionID: conn.ID,
			Channel:      "telegram",
			From:         "12345",
			To:           "@test_bot",
			Body:         "Hello mapped direct!",
		}

		err = syncer.SyncInboundMessage(ctx, contact, ev)
		if err != nil {
			t.Fatalf("SyncInboundMessage failed: %v", err)
		}

		if postMessageCount != 1 {
			t.Errorf("expected 1 postMessage, got %d", postMessageCount)
		}
	})

	t.Run("SyncInboundMessage_Mapped404_DeletesMappingAndRecreates", func(t *testing.T) {
		var postMessageCount, searchCount, createContactCount, createConvCount int

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.Method == http.MethodPost && r.URL.Path == "/api/v1/accounts/1/conversations/202/messages":
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error": "conversation not found"}`))

			case r.Method == http.MethodGet && r.URL.Path == "/api/v1/accounts/1/contacts/search":
				searchCount++
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"payload": []}`))

			case r.Method == http.MethodPost && r.URL.Path == "/api/v1/accounts/1/contacts":
				createContactCount++
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"payload": {"contact": {"id": 101}}}`))

			case r.Method == http.MethodPost && r.URL.Path == "/api/v1/accounts/1/conversations":
				createConvCount++
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id": 203}`))

			case r.Method == http.MethodPost && r.URL.Path == "/api/v1/accounts/1/conversations/203/messages":
				postMessageCount++
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id": 304}`))

			default:
				t.Errorf("unexpected API call: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusInternalServerError)
			}
		}))
		defer server.Close()

		existingMapping := &repository.ChatwootMapping{
			WorkspaceID:            ws.ID,
			ContactID:              contact.ID,
			ConnectionID:           conn.ID,
			ChatwootContactID:      101,
			ChatwootConversationID: 202,
			Channel:                "telegram",
			SenderIdentity:         "@test_bot",
		}
		if err := mappingRepo.Upsert(ctx, existingMapping); err != nil {
			t.Fatalf("failed to upsert mapping: %v", err)
		}

		cfg := map[string]interface{}{
			"api_url":      server.URL,
			"access_token": "test-token",
			"inbox_id":     int64(2),
			"account_id":   int64(1),
		}
		cfgBytes, _ := json.Marshal(cfg)
		integration, err := integrationRepo.GetByProvider(ctx, ws.ID, "chatwoot")
		if err != nil {
			t.Fatalf("failed to get integration: %v", err)
		}
		integration.Config = cfgBytes
		if err := integrationRepo.Save(ctx, integration); err != nil {
			t.Fatalf("failed to save integration: %v", err)
		}

		syncer := chatwoot.NewChatwootSyncer(integrationRepo, mappingRepo, server.Client())

		ev := &inbound.InboundEvent{
			WorkspaceID:  ws.ID,
			ConnectionID: conn.ID,
			Channel:      "telegram",
			From:         "12345",
			To:           "@test_bot",
			Body:         "Hello retry sync!",
		}

		err = syncer.SyncInboundMessage(ctx, contact, ev)
		if err != nil {
			t.Fatalf("SyncInboundMessage failed: %v", err)
		}

		mapping, err := mappingRepo.GetByContactAndConnection(ctx, ws.ID, contact.ID, conn.ID)
		if err != nil {
			t.Fatalf("failed to find mapping: %v", err)
		}
		if mapping.ChatwootConversationID != 203 {
			t.Errorf("expected mapping conversation ID to be 203, got %d", mapping.ChatwootConversationID)
		}
	})
}
