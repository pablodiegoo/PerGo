package typebot_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/integration/typebot"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/repository"
)

type mockPublisher struct {
	called  bool
	data    []byte
	traceID string
}

func (m *mockPublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	m.called = true
	m.data = data
	m.traceID = traceID
	return nil
}

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

func TestTypebotForwarder_BotInactive(t *testing.T) {
	pub := &mockPublisher{}
	f := typebot.NewForwarder(nil, nil, pub)

	contact := &domain.Contact{
		ID:        uuid.New(),
		BotActive: false,
	}

	event := &inbound.InboundEvent{
		WorkspaceID:  uuid.New(),
		ConnectionID: uuid.New(),
		Body:         "Hello",
	}

	err := f.SyncInboundMessage(context.Background(), contact, event)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	if pub.called {
		t.Error("expected publisher to not be called when bot is inactive")
	}
}

func TestTypebotForwarder_PopulatesRoutingFields(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	wsRepo := repository.NewWorkspaceRepository(pool)
	sessionRepo := repository.NewTypebotSessionRepository(pool)
	integrationRepo := repository.NewIntegrationRepository(pool, noOpCryptoProvider{})

	ws, err := wsRepo.Create(ctx, "typebot_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	connID := uuid.New()
	connRepo := repository.NewConnectionRepository(pool, noOpCryptoProvider{})
	conn := &repository.Connection{
		ID:             connID,
		WorkspaceID:    ws.ID,
		Name:           "Test Connection",
		Channel:        "whatsapp",
		SenderIdentity: "+987654321",
		Status:         "connected",
	}
	if err := connRepo.Create(ctx, conn); err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}

	// 1. Setup mock typebot API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		
		if r.URL.Path == "/api/v1/typebots/bot1/startChat" {
			var requestBody typebot.StartChatRequest
			if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
				t.Errorf("failed to decode request body: %v", err)
			}
			
			expectedSessionId := fmt.Sprintf("%s:+123456789", connID.String())
			if requestBody.SessionID != expectedSessionId {
				t.Errorf("expected sessionId %q, got %q", expectedSessionId, requestBody.SessionID)
			}
			
			if requestBody.Message != "[Media Attachment]" {
				t.Errorf("expected message %q, got %q", "[Media Attachment]", requestBody.Message)
			}
			
			if requestBody.PrefilledVariables == nil {
				t.Errorf("expected prefilledVariables to be populated")
			} else {
				pergoMeta, ok := requestBody.PrefilledVariables["pergo_metadata"].(map[string]any)
				if !ok {
					t.Errorf("expected pergo_metadata to be map[string]any, got %T", requestBody.PrefilledVariables["pergo_metadata"])
				} else {
					if pergoMeta["media_url"] != "https://example.com/image.png" {
						t.Errorf("expected media_url %q, got %q", "https://example.com/image.png", pergoMeta["media_url"])
					}
					if pergoMeta["media_type"] != "image" {
						t.Errorf("expected media_type %q, got %q", "image", pergoMeta["media_type"])
					}
				}
			}
		}

		// Mock start chat response
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"sessionId": "mock-session-id",
			"messages": [
				{
					"id": "msg1",
					"type": "text",
					"content": {
						"type": "richText",
						"richText": [
							{
								"type": "p",
								"children": [{"text": "Hello from typebot"}]
							}
						]
					}
				}
			]
		}`))
	}))
	defer server.Close()

	// 2. Create Typebot Integration with config
	cfg := typebot.Config{
		APIURL: server.URL,
		Bots: []typebot.BotConfig{
			{
				ConnectionID: connID.String(),
				BotID:        "bot1",
				PublicToken:  "pub_tok",
				IsDefault:    true,
			},
		},
	}
	cfgBytes, _ := json.Marshal(cfg)
	integration := &repository.Integration{
		ID:          uuid.New(),
		WorkspaceID: ws.ID,
		Name:        "Typebot Test",
		Provider:    "typebot",
		Active:      true,
		Config:      cfgBytes,
	}
	if err := integrationRepo.Save(ctx, integration); err != nil {
		t.Fatalf("failed to save integration: %v", err)
	}

	pub := &mockPublisher{}
	f := typebot.NewForwarder(sessionRepo, integrationRepo, pub)

	contactRepo := repository.NewContactRepository(pool)
	contact, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp", "+123456789", "Test User", "", "+123456789")
	if err != nil {
		t.Fatalf("failed to resolve contact: %v", err)
	}
	contact.BotActive = true

	ev := &inbound.InboundEvent{
		WorkspaceID:  ws.ID,
		ConnectionID: connID,
		Channel:      "whatsapp",
		From:         "+123456789",
		To:           "+987654321",
		Body:         "Hello!",
		Media: &inbound.InboundMedia{
			MediaURL:  "https://example.com/image.png",
			MediaType: "image",
		},
	}

	err = f.SyncInboundMessage(ctx, contact, ev)
	if err != nil {
		t.Fatalf("SyncInboundMessage failed: %v", err)
	}

	if !pub.called {
		t.Fatalf("expected publisher to be called")
	}

	// 3. Verify the published message
	var outMsg domain.QueueMessage
	if err := json.Unmarshal(pub.data, &outMsg); err != nil {
		t.Fatalf("failed to unmarshal published data: %v", err)
	}

	if outMsg.ConnectionID != ev.ConnectionID {
		t.Errorf("expected ConnectionID %v, got %v", ev.ConnectionID, outMsg.ConnectionID)
	}
	if outMsg.SenderIdentity != ev.To {
		t.Errorf("expected SenderIdentity %v, got %v", ev.To, outMsg.SenderIdentity)
	}
	if outMsg.TraceID == "" {
		t.Errorf("expected TraceID to be populated")
	}
	if outMsg.TraceID != pub.traceID {
		t.Errorf("expected QueueMessage TraceID (%s) to match published traceID (%s)", outMsg.TraceID, pub.traceID)
	}
}
