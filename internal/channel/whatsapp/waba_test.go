package whatsapp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pablojhp.pergo/internal/channel"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/netpolicy"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
)

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

	// Ensure migrations are run
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

func TestWABADispatch(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	// 1. Setup Encryptor and Repository
	kek := make([]byte, 32)
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	connectionsRepo := repository.NewConnectionRepository(pool, enc)
	wsRepo := repository.NewWorkspaceRepository(pool)

	// Create workspace
	ws, err := wsRepo.Create(ctx, "waba_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	// Save test credentials
	wabaConfig := WABAConfig{
		PhoneNumberID: "12345_phone_id",
		Token:         "test_access_token_abc123",
	}
	configBytes, _ := json.Marshal(wabaConfig)
	connID := uuid.New()
	err = connectionsRepo.Create(ctx, &repository.Connection{
		ID:             connID,
		WorkspaceID:    ws.ID,
		Name:           "WABA",
		Channel:        "whatsapp_cloud",
		SenderIdentity: "+12345_phone_id",
		Status:         "active",
		Credentials:    configBytes,
	})
	if err != nil {
		t.Fatalf("failed to save WABA credentials: %v", err)
	}

	// Setup tenant context
	tenantCtx := tenant.WithWorkspaceID(context.Background(), ws.ID)

	t.Run("Success Freeform Text Message", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify headers
			if r.Header.Get("Authorization") != "Bearer test_access_token_abc123" {
				t.Errorf("Authorization header = %q, want Bearer test_access_token_abc123", r.Header.Get("Authorization"))
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("Content-Type header = %q, want application/json", r.Header.Get("Content-Type"))
			}

			// Verify endpoint path
			if r.URL.Path != "/12345_phone_id/messages" {
				t.Errorf("path = %q, want /12345_phone_id/messages", r.URL.Path)
			}

			// Verify payload
			bodyBytes, _ := io.ReadAll(r.Body)
			var req wabaMessageRequest
			if err := json.Unmarshal(bodyBytes, &req); err != nil {
				t.Fatalf("failed to unmarshal request body: %v", err)
			}

			if req.MessagingProduct != "whatsapp" || req.RecipientType != "individual" {
				t.Errorf("unexpected product or recipient: %+v", req)
			}
			if req.To != "+5511999999999" || req.Type != "text" || req.Text == nil || req.Text.Body != "Hello from PerGo!" {
				t.Errorf("unexpected payload details: %+v", req)
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"messaging_product":"whatsapp","contacts":[{"input":"+5511999999999","wa_id":"5511999999999"}],"messages":[{"id":"wamid.test_id_999"}]}`))
		}))
		defer server.Close()

		adapter := NewWABAAdapter(connectionsRepo, nil, nil, "")
		adapter.SetBaseURL(server.URL)

		payload := &channel.MessagePayload{
			ConnectionID:   connID,
			SenderIdentity: "+12345_phone_id",
			To:             "+5511999999999",
			Body:           "Hello from PerGo!",
		}

		resp, err := adapter.Dispatch(tenantCtx, payload)
		if err != nil {
			t.Fatalf("expected nil error on success, got: %v", err)
		}
		if resp != "wamid.test_id_999" {
			t.Errorf("expected wamid 'wamid.test_id_999', got %q", resp)
		}
	})

	t.Run("Success Template Message", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bodyBytes, _ := io.ReadAll(r.Body)
			var req wabaMessageRequest
			_ = json.Unmarshal(bodyBytes, &req)

			if req.Type != "template" || req.Template == nil {
				t.Errorf("expected template message, got %+v", req)
				return
			}
			if req.Template.Name != "welcome_test" || req.Template.Language.Code != "pt_BR" {
				t.Errorf("unexpected template attributes: %+v", req.Template)
			}
			if len(req.Template.Components) != 1 || len(req.Template.Components[0].Parameters) != 2 {
				t.Errorf("unexpected components or parameters: %+v", req.Template.Components)
			}
			if req.Template.Components[0].Parameters[0].Text != "Pablo" || req.Template.Components[0].Parameters[1].Text != "PerGo" {
				t.Errorf("unexpected parameters content: %+v", req.Template.Components[0].Parameters)
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.template_test_123"}]}`))
		}))
		defer server.Close()

		adapter := NewWABAAdapter(connectionsRepo, nil, nil, "")
		adapter.SetBaseURL(server.URL)

		payload := &channel.MessagePayload{
			ConnectionID:   connID,
			SenderIdentity: "+12345_phone_id",
			To:             "+5511999999999",
			Metadata: map[string]string{
				"template_name":     "welcome_test",
				"template_language": "pt_BR",
				"param1":            "Pablo",
				"param2":            "PerGo",
			},
		}

		resp, err := adapter.Dispatch(tenantCtx, payload)
		if err != nil {
			t.Fatalf("expected nil error on template dispatch, got: %v", err)
		}
		if resp != "wamid.template_test_123" {
			t.Errorf("expected wamid 'wamid.template_test_123', got %q", resp)
		}
	})

	t.Run("Success Template Message New Struct", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bodyBytes, _ := io.ReadAll(r.Body)
			var req wabaMessageRequest
			_ = json.Unmarshal(bodyBytes, &req)

			if req.Type != "template" || req.Template == nil {
				t.Errorf("expected template message, got %+v", req)
				return
			}
			if req.Template.Name != "welcome_test_new" || req.Template.Language.Code != "en_US" {
				t.Errorf("unexpected template attributes: %+v", req.Template)
			}
			if len(req.Template.Components) != 1 || len(req.Template.Components[0].Parameters) != 1 {
				t.Errorf("unexpected components or parameters: %+v", req.Template.Components)
			}
			if req.Template.Components[0].Parameters[0].Text != "Alice" {
				t.Errorf("unexpected parameter content: %+v", req.Template.Components[0].Parameters)
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.HBgL..."}]}`))
		}))
		defer server.Close()

		adapter := NewWABAAdapter(connectionsRepo, nil, nil, "")
		adapter.SetBaseURL(server.URL)

		payload := &channel.MessagePayload{
			ConnectionID:   connID,
			SenderIdentity: "+12345_phone_id",
			To:             "+5511999999999",
			TemplateName:   "welcome_test_new",
			Language:       "en_US",
			Components: []domain.TemplateComponent{
				{
					Type: "body",
					Parameters: []domain.TemplateParameter{
						{Type: "text", Text: "Alice"},
					},
				},
			},
		}

		_, err := adapter.Dispatch(tenantCtx, payload)
		if err != nil {
			t.Fatalf("expected nil error on template dispatch, got: %v", err)
		}
	})

	t.Run("Terminal Error - Number Not on WhatsApp (131030)", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Recipient is not on WhatsApp","type":"OAuthException","code":131030,"fbtrace_id":"A1B2"}}`))
		}))
		defer server.Close()

		adapter := NewWABAAdapter(connectionsRepo, nil, nil, "")
		adapter.SetBaseURL(server.URL)

		_, err := adapter.Dispatch(tenantCtx, &channel.MessagePayload{
			ConnectionID:   connID,
			SenderIdentity: "+12345_phone_id",
			To:             "+12345",
			Body:           "hi",
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !channel.IsTerminal(err) {
			t.Errorf("expected error to be terminal, got: %v", err)
		}
	})

	t.Run("Terminal Error - Outside 24h Window (131047)", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Message failed to send because it was outside 24h customer service window","type":"OAuthException","code":131047}}`))
		}))
		defer server.Close()

		adapter := NewWABAAdapter(connectionsRepo, nil, nil, "")
		adapter.SetBaseURL(server.URL)

		_, err := adapter.Dispatch(tenantCtx, &channel.MessagePayload{
			ConnectionID:   connID,
			SenderIdentity: "+12345_phone_id",
			To:             "+12345",
			Body:           "hi",
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !channel.IsTerminal(err) {
			t.Errorf("expected error to be terminal, got: %v", err)
		}
	})

	t.Run("Transient Error - Rate Limit (130429)", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"Rate limit exceeded","type":"OAuthException","code":130429}}`))
		}))
		defer server.Close()

		adapter := NewWABAAdapter(connectionsRepo, nil, nil, "")
		adapter.SetBaseURL(server.URL)

		_, err := adapter.Dispatch(tenantCtx, &channel.MessagePayload{
			ConnectionID:   connID,
			SenderIdentity: "+12345_phone_id",
			To:             "+12345",
			Body:           "hi",
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if channel.IsTerminal(err) {
			t.Errorf("expected error to be transient, got terminal: %v", err)
		}
	})

	t.Run("Local Window Checker - Expired/Missing", func(t *testing.T) {
		mockChecker := &mockWABAWindowChecker{open: false}
		adapter := NewWABAAdapter(connectionsRepo, nil, mockChecker, "")

		_, err := adapter.Dispatch(tenantCtx, &channel.MessagePayload{
			ConnectionID:   connID,
			SenderIdentity: "+12345_phone_id",
			To:             "+12345",
			Body:           "hi",
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !channel.IsTerminal(err) {
			t.Fatalf("expected terminal error, got: %v", err)
		}
		if err.Error() != "terminal: customer service window expired" {
			t.Errorf("expected error message 'terminal: customer service window expired', got: %v", err)
		}
	})

	t.Run("Local Window Checker - Open", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.HBgL..."}]}`))
		}))
		defer server.Close()

		mockChecker := &mockWABAWindowChecker{open: true}
		adapter := NewWABAAdapter(connectionsRepo, nil, mockChecker, "")
		adapter.SetBaseURL(server.URL)

		_, err := adapter.Dispatch(tenantCtx, &channel.MessagePayload{
			ConnectionID:   connID,
			SenderIdentity: "+12345_phone_id",
			To:             "+12345",
			Body:           "hi",
		})
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}
	})

	t.Run("Success Send Media (WABA)", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify endpoint path
			if r.URL.Path != "/12345_phone_id/messages" {
				t.Errorf("path = %q, want /12345_phone_id/messages", r.URL.Path)
			}

			// Verify payload has image block and URL
			bodyBytes, _ := io.ReadAll(r.Body)
			var payload struct {
				Type  string `json:"type"`
				Image *struct {
					Link    string  `json:"link"`
					Caption *string `json:"caption,omitempty"`
				} `json:"image"`
			}
			if err := json.Unmarshal(bodyBytes, &payload); err != nil {
				t.Fatalf("unmarshal request payload: %v", err)
			}

			if payload.Type != "image" || payload.Image == nil {
				t.Fatalf("expected image message, got: %s", string(bodyBytes))
			}

			if payload.Image.Link != "/media/workspace123/hash123.png" {
				t.Errorf("expected link /media/workspace123/hash123.png, got %s", payload.Image.Link)
			}

			if payload.Image.Caption == nil || *payload.Image.Caption != "Test Caption" {
				t.Errorf("expected caption Test Caption, got %v", payload.Image.Caption)
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.HBgL..."}]}`))
		}))
		defer server.Close()

		adapter := NewWABAAdapter(connectionsRepo, nil, nil, "")
		adapter.SetBaseURL(server.URL)

		payload := &channel.MessagePayload{
			ConnectionID:   connID,
			SenderIdentity: "+12345_phone_id",
			To:             "+5511999999999",
			Media: &domain.Media{
				MediaURL:  "/media/workspace123/hash123.png",
				MediaType: "image",
				Caption:   "Test Caption",
			},
		}

		_, err := adapter.Dispatch(tenantCtx, payload)
		if err != nil {
			t.Fatalf("expected nil error on success, got: %v", err)
		}
	})

	t.Run("Success Interactive Message", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bodyBytes, _ := io.ReadAll(r.Body)
			var req struct {
				Type        string `json:"type"`
				Interactive *struct {
					Type   string `json:"type"`
					Body   struct{ Text string } `json:"body"`
					Action struct {
						Buttons []struct {
							Type  string `json:"type"`
							Reply struct {
								ID    string `json:"id"`
								Title string `json:"title"`
							} `json:"reply"`
						} `json:"buttons"`
					} `json:"action"`
				} `json:"interactive"`
			}
			_ = json.Unmarshal(bodyBytes, &req)

			if req.Type != "interactive" || req.Interactive == nil {
				t.Errorf("expected interactive message, got %+v", string(bodyBytes))
				return
			}
			if req.Interactive.Type != "button" {
				t.Errorf("unexpected interactive type: %s", req.Interactive.Type)
			}
			if req.Interactive.Body.Text != "Choose one" {
				t.Errorf("unexpected body text: %s", req.Interactive.Body.Text)
			}
			if len(req.Interactive.Action.Buttons) != 1 || req.Interactive.Action.Buttons[0].Reply.Title != "Yes" {
				t.Errorf("unexpected buttons content: %+v", req.Interactive.Action.Buttons)
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.interactive_test_123"}]}`))
		}))
		defer server.Close()

		adapter := NewWABAAdapter(connectionsRepo, nil, nil, "")
		adapter.SetBaseURL(server.URL)

		payload := &channel.MessagePayload{
			ConnectionID:   connID,
			SenderIdentity: "+12345_phone_id",
			To:             "+5511999999999",
			Interactive: &domain.Interactive{
				Type: "button",
				Body: domain.TextContent{Text: "Choose one"},
				Action: domain.Action{
					Buttons: []domain.Button{
						{Type: "reply", Reply: domain.Reply{ID: "btn1", Title: "Yes"}},
					},
				},
			},
		}

		resp, err := adapter.Dispatch(tenantCtx, payload)
		if err != nil {
			t.Fatalf("expected nil error on interactive dispatch, got: %v", err)
		}
		if resp != "wamid.interactive_test_123" {
			t.Errorf("expected wamid 'wamid.interactive_test_123', got %q", resp)
		}
	})

	t.Run("Success Override whatsapp_cloud", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bodyBytes, _ := io.ReadAll(r.Body)
			if string(bodyBytes) != `{"custom":"payload"}` {
				t.Errorf("expected override payload, got: %s", string(bodyBytes))
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.override_123"}]}`))
		}))
		defer server.Close()

		adapter := NewWABAAdapter(connectionsRepo, nil, nil, "")
		adapter.SetBaseURL(server.URL)

		payload := &channel.MessagePayload{
			ConnectionID:   connID,
			SenderIdentity: "+12345_phone_id",
			To:             "+5511999999999", // To is ignored if override dictates, but required by struct
			ChannelOverrides: map[string]json.RawMessage{
				"whatsapp_cloud": json.RawMessage(`{"custom":"payload"}`),
			},
		}

		resp, err := adapter.Dispatch(tenantCtx, payload)
		if err != nil {
			t.Fatalf("expected nil error on override dispatch, got: %v", err)
		}
		if resp != "wamid.override_123" {
			t.Errorf("expected wamid 'wamid.override_123', got %q", resp)
		}
	})
}

type mockWABAWindowChecker struct {
	open bool
	err  error
}

func (m *mockWABAWindowChecker) IsWindowOpenBool(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channelName string, recipientIdentity string, safetyBuffer time.Duration) (bool, error) {
	return m.open, m.err
}

func TestWABA_MediaExternalURL(t *testing.T) {
	pool := getTestPool(t)
	kek := make([]byte, 32)
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	connectionsRepo := repository.NewConnectionRepository(pool, enc)
	wsID := uuid.New()
	tenantCtx := tenant.WithWorkspaceID(context.Background(), wsID)

	// Clean up duplicate connection if exists from previous test runs
	_, _ = pool.Exec(context.Background(), "DELETE FROM connections WHERE sender_identity = $1", "+12345")

	// Create test workspace to satisfy FK constraint on connections
	_, err = pool.Exec(context.Background(), "INSERT INTO workspaces (id, name, created_at, updated_at) VALUES ($1, $2, now(), now())", wsID, "test-workspace-"+wsID.String())
	if err != nil {
		t.Fatalf("failed to create test workspace for FK: %v", err)
	}

	creds := WABAConfig{
		PhoneNumberID: "12345",
		Token:         "token_123",
		WABAAccountID: "waba_123",
	}
	credsJSON, _ := json.Marshal(creds)
	connID := uuid.New()
	err = connectionsRepo.Create(context.Background(), &repository.Connection{
		ID:             connID,
		WorkspaceID:    wsID,
		Name:           "WABA",
		Channel:        "whatsapp_cloud",
		SenderIdentity: "+12345",
		Status:         "active",
		Credentials:    credsJSON,
	})
	if err != nil {
		t.Fatalf("failed to save credentials: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Image struct {
				Link string `json:"link"`
			} `json:"image"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)

		if payload.Image.Link != "https://example.com/media/workspace123/hash123.png" {
			t.Errorf("expected link https://example.com/media/workspace123/hash123.png, got %s", payload.Image.Link)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.HBgL..."}]}`))
	}))
	defer server.Close()

	adapter := NewWABAAdapter(connectionsRepo, server.Client(), nil, "https://example.com")
	adapter.SetBaseURL(server.URL)

	payload := &channel.MessagePayload{
		ConnectionID:   connID,
		SenderIdentity: "+12345",
		To:             "+5511999999999",
		Media: &domain.Media{
			MediaURL:  "/media/workspace123/hash123.png",
			MediaType: "image",
		},
	}

	_, err = adapter.Dispatch(tenantCtx, payload)
	if err != nil {
		t.Fatalf("expected nil error on success, got: %v", err)
	}
}

func TestWABAInboundAdapterStatuses(t *testing.T) {
	ctx := context.Background()
	adapter := NewWABAInboundAdapter(nil)

	// Create dummy credentials JSON
	creds := wabaVerifyCreds{
		VerifyToken: "my_verify_token",
		Token:       "my_token",
	}
	credsJSON, _ := json.Marshal(creds)
	wsID := uuid.New()
	conn := &repository.Connection{
		WorkspaceID: wsID,
		Credentials: credsJSON,
	}

	// Payload with sent, delivered, read statuses
	payload := []byte(`{
		"object": "whatsapp_business_account",
		"entry": [
			{
				"id": "waba_id",
				"changes": [
					{
						"field": "messages",
						"value": {
							"messaging_product": "whatsapp",
							"metadata": {
								"display_phone_number": "15550000000",
								"phone_number_id": "phone_id_123"
							},
							"statuses": [
								{
									"id": "wamid.sent_123",
									"status": "sent",
									"recipient_id": "5511999999999",
									"timestamp": "1700000000"
								},
								{
									"id": "wamid.delivered_123",
									"status": "delivered",
									"recipient_id": "5511999999999",
									"timestamp": "1700000001"
								},
								{
									"id": "wamid.read_123",
									"status": "read",
									"recipient_id": "5511999999999",
									"timestamp": "1700000002"
								}
							]
						}
					}
				]
			}
		]
	}`)

	events, err := adapter.Parse(ctx, payload, nil, conn)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	expectedStatuses := []struct {
		id     string
		status string
	}{
		{"wamid.sent_123", "sent"},
		{"wamid.delivered_123", "delivered"},
		{"wamid.read_123", "read"},
	}

	for i, expected := range expectedStatuses {
		ev := events[i]
		if ev.WorkspaceID != wsID {
			t.Errorf("event %d: expected WorkspaceID %s, got %s", i, wsID, ev.WorkspaceID)
		}
		if ev.MessageID != expected.id {
			t.Errorf("event %d: expected MessageID %s, got %s", i, expected.id, ev.MessageID)
		}
		if ev.Channel != "whatsapp_cloud" {
			t.Errorf("event %d: expected Channel whatsapp_cloud, got %s", i, ev.Channel)
		}
		if ev.From != "5511999999999" {
			t.Errorf("event %d: expected From 5511999999999, got %s", i, ev.From)
		}
		if ev.To != "15550000000" {
			t.Errorf("event %d: expected To 15550000000, got %s", i, ev.To)
		}
		if ev.Body != expected.status {
			t.Errorf("event %d: expected Body %s, got %s", i, expected.status, ev.Body)
		}
		if ev.Metadata == nil || ev.Metadata["type"] != "status_update" {
			t.Errorf("event %d: expected Metadata type status_update, got %v", i, ev.Metadata)
		}
	}
}

func TestWABAAdapter_MetaErrorClassification(t *testing.T) {
	adapter := NewWABAAdapter(nil, nil, nil, "")

	tests := []struct {
		name       string
		statusCode int
		errorCode  int
		isTerminal bool
	}{
		{
			name:       "Invalid Catalog ID (131009)",
			statusCode: 400,
			errorCode:  131009,
			isTerminal: true,
		},
		{
			name:       "Invalid Product SKU (131084)",
			statusCode: 400,
			errorCode:  131084,
			isTerminal: true,
		},
		{
			name:       "User Not on WhatsApp (131030)",
			statusCode: 400,
			errorCode:  131030,
			isTerminal: true,
		},
		{
			name:       "Outside 24h Window (131047)",
			statusCode: 400,
			errorCode:  131047,
			isTerminal: true,
		},
		{
			name:       "Rate Limit Exceeded (130429)",
			statusCode: 429,
			errorCode:  130429,
			isTerminal: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errResp := &MetaErrorResponse{}
			errResp.Error.Code = tt.errorCode
			errResp.Error.Message = "Test Meta Error"

			err := adapter.classifyError(tt.statusCode, errResp)
			if err == nil {
				t.Fatalf("expected non-nil error")
			}
			if channel.IsTerminal(err) != tt.isTerminal {
				t.Errorf("IsTerminal(err) = %v, want %v", channel.IsTerminal(err), tt.isTerminal)
			}
		})
	}
}

func TestWABAAdapter_ProductPayloads(t *testing.T) {
	t.Run("Single Product Payload JSON Formatting", func(t *testing.T) {
		req := wabaMessageRequest{
			MessagingProduct: "whatsapp",
			RecipientType:    "individual",
			To:               "+5511999999999",
			Type:             "interactive",
			Interactive: &wabaInteractive{
				Type:   "product",
				Body:   &wabaInteractiveText{Text: "Check out this product"},
				Footer: &wabaInteractiveText{Text: "Store Footer"},
				Action: wabaProductAction{
					CatalogID:         "cat_123",
					ProductRetailerID: "sku_abc",
				},
			},
		}

		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("failed to marshal single product request: %v", err)
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if parsed["type"] != "interactive" {
			t.Errorf("expected type 'interactive', got %v", parsed["type"])
		}

		interactive, ok := parsed["interactive"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected interactive map")
		}
		if interactive["type"] != "product" {
			t.Errorf("expected interactive type 'product', got %v", interactive["type"])
		}
		if interactive["header"] != nil {
			t.Errorf("expected header to be nil for single product")
		}

		action, ok := interactive["action"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected action map")
		}
		if action["catalog_id"] != "cat_123" {
			t.Errorf("expected catalog_id 'cat_123', got %v", action["catalog_id"])
		}
		if action["product_retailer_id"] != "sku_abc" {
			t.Errorf("expected product_retailer_id 'sku_abc', got %v", action["product_retailer_id"])
		}
	})

	t.Run("Multi-Product List Payload JSON Formatting", func(t *testing.T) {
		req := wabaMessageRequest{
			MessagingProduct: "whatsapp",
			RecipientType:    "individual",
			To:               "+5511999999999",
			Type:             "interactive",
			Interactive: &wabaInteractive{
				Type:   "product_list",
				Header: &wabaInteractiveText{Type: "text", Text: "Featured Catalog"},
				Body:   &wabaInteractiveText{Text: "Browse our items"},
				Footer: &wabaInteractiveText{Text: "Thank you"},
				Action: wabaProductAction{
					CatalogID: "cat_456",
					Sections: []wabaProductSection{
						{
							Title: "Electronics",
							ProductItems: []wabaProductItem{
								{ProductRetailerID: "sku_100"},
								{ProductRetailerID: "sku_101"},
							},
						},
						{
							Title: "Apparel",
							ProductItems: []wabaProductItem{
								{ProductRetailerID: "sku_200"},
							},
						},
					},
				},
			},
		}

		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("failed to marshal product list request: %v", err)
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		interactive, ok := parsed["interactive"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected interactive map")
		}
		if interactive["type"] != "product_list" {
			t.Errorf("expected interactive type 'product_list', got %v", interactive["type"])
		}

		header, ok := interactive["header"].(map[string]interface{})
		if !ok || header["text"] != "Featured Catalog" || header["type"] != "text" {
			t.Errorf("unexpected header: %v", interactive["header"])
		}

		action, ok := interactive["action"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected action map")
		}
		if action["catalog_id"] != "cat_456" {
			t.Errorf("expected catalog_id 'cat_456', got %v", action["catalog_id"])
		}

		sections, ok := action["sections"].([]interface{})
		if !ok || len(sections) != 2 {
			t.Fatalf("expected 2 sections, got %v", action["sections"])
		}
	})

	t.Run("Dispatch Single Product and Product List via Server", func(t *testing.T) {
		var receivedReq wabaMessageRequest

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bodyBytes, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(bodyBytes, &receivedReq)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.prod_123"}]}`))
		}))
		defer server.Close()

		dsn := os.Getenv("PERGO_DATABASE_URL")
		if dsn == "" {
			dsn = "postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		pool, err := pgxpool.New(ctx, dsn)
		if err != nil {
			t.Skipf("Skipping DB Dispatch integration subtest: %v", err)
			return
		}
		if err := pool.Ping(ctx); err != nil {
			pool.Close()
			t.Skipf("Skipping DB Dispatch integration subtest: %v", err)
			return
		}
		defer pool.Close()

		db, err := postgres.NewSQLDB(pool)
		if err != nil {
			t.Fatalf("failed sqlDB: %v", err)
		}
		defer db.Close()
		_ = postgres.RunMigrations(db)

		kek := make([]byte, 32)
		enc, _ := crypto.NewEncryptor(kek)
		connectionsRepo := repository.NewConnectionRepository(pool, enc)
		wsRepo := repository.NewWorkspaceRepository(pool)

		ws, err := wsRepo.Create(ctx, "waba_prod_ws_"+uuid.New().String())
		if err != nil {
			t.Fatalf("failed workspace create: %v", err)
		}
		defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

		wabaConfig := WABAConfig{
			PhoneNumberID:    "12345_phone_id",
			Token:            "test_access_token",
			DefaultCatalogID: "default_cat_999",
		}
		configBytes, _ := json.Marshal(wabaConfig)
		connID := uuid.New()
		_ = connectionsRepo.Create(ctx, &repository.Connection{
			ID:             connID,
			WorkspaceID:    ws.ID,
			Name:           "WABA",
			Channel:        "whatsapp_cloud",
			SenderIdentity: "+12345_phone_id",
			Status:         "active",
			Credentials:    configBytes,
		})

		tenantCtx := tenant.WithWorkspaceID(context.Background(), ws.ID)
		adapter := NewWABAAdapter(connectionsRepo, server.Client(), nil, "")
		adapter.SetBaseURL(server.URL)

		// 1. Dispatch Single Product
		payloadSingle := &channel.MessagePayload{
			ConnectionID:   connID,
			SenderIdentity: "+12345_phone_id",
			To:             "+5511999999999",
			Type:           domain.MessageTypeProduct,
			Product: &domain.ProductPayload{
				CatalogID:         "cat_single_123",
				ProductRetailerID: "sku_single_99",
				Body:              "Single product description",
				Footer:            "Footer text",
			},
		}

		resp, err := adapter.Dispatch(tenantCtx, payloadSingle)
		if err != nil {
			t.Fatalf("Dispatch single product error: %v", err)
		}
		if resp != "wamid.prod_123" {
			t.Errorf("expected wamid 'wamid.prod_123', got %q", resp)
		}
		if receivedReq.Type != "interactive" || receivedReq.Interactive == nil || receivedReq.Interactive.Type != "product" {
			t.Errorf("unexpected payload sent for single product: %+v", receivedReq)
		}

		// 2. Dispatch Product List
		payloadList := &channel.MessagePayload{
			ConnectionID:   connID,
			SenderIdentity: "+12345_phone_id",
			To:             "+5511999999999",
			Type:           domain.MessageTypeProductList,
			Product: &domain.ProductPayload{
				Header: "Catalog List Header",
				Body:   "Multi product list body",
				Footer: "List footer",
				Sections: []domain.ProductSection{
					{
						Title: "Section 1",
						ProductItems: []domain.ProductItem{
							{ProductRetailerID: "sku_s1_1"},
						},
					},
				},
			},
		}

		respList, err := adapter.Dispatch(tenantCtx, payloadList)
		if err != nil {
			t.Fatalf("Dispatch product list error: %v", err)
		}
		if respList != "wamid.prod_123" {
			t.Errorf("expected wamid 'wamid.prod_123', got %q", respList)
		}
		if receivedReq.Type != "interactive" || receivedReq.Interactive == nil || receivedReq.Interactive.Type != "product_list" {
			t.Errorf("unexpected payload sent for product list: %+v", receivedReq)
		}
		if receivedReq.Interactive.Header == nil || receivedReq.Interactive.Header.Text != "Catalog List Header" {
			t.Errorf("unexpected header for product list: %+v", receivedReq.Interactive.Header)
		}
	})
}

func TestWABAInboundAdapter_OrderParsing(t *testing.T) {
	ctx := context.Background()
	adapter := NewWABAInboundAdapter(nil)

	creds := wabaVerifyCreds{
		VerifyToken: "my_verify_token",
		Token:       "my_token",
	}
	credsJSON, _ := json.Marshal(creds)
	wsID := uuid.New()
	conn := &repository.Connection{
		WorkspaceID: wsID,
		Credentials: credsJSON,
	}

	payload := []byte(`{
		"object": "whatsapp_business_account",
		"entry": [
			{
				"id": "entry_123",
				"changes": [
					{
						"field": "messages",
						"value": {
							"messaging_product": "whatsapp",
							"metadata": {
								"display_phone_number": "15550001111",
								"phone_number_id": "phone_123"
							},
							"contacts": [{"profile": {"name": "Test Customer"}, "wa_id": "5511999999999"}],
							"messages": [
								{
									"from": "5511999999999",
									"id": "wamid.order_test_001",
									"timestamp": "1700000000",
									"type": "order",
									"order": {
										"catalog_id": "cat_999",
										"text": "Please deliver quickly",
										"product_items": [
											{
												"product_retailer_id": "SKU-001",
												"quantity": "2",
												"item_price": "15.50",
												"currency": "BRL"
											},
											{
												"product_retailer_id": "SKU-002",
												"quantity": "1",
												"item_price": "30.00",
												"currency": "BRL"
											}
										]
									}
								}
							]
						}
					}
				]
			}
		]
	}`)

	events, err := adapter.Parse(ctx, payload, nil, conn)
	if err != nil {
		t.Fatalf("failed to parse order webhook: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	ev := events[0]
	if ev.MessageID != "wamid.order_test_001" {
		t.Errorf("expected MessageID 'wamid.order_test_001', got %q", ev.MessageID)
	}
	if ev.From != "5511999999999" {
		t.Errorf("expected From '5511999999999', got %q", ev.From)
	}
	if ev.Metadata == nil {
		t.Fatalf("expected non-nil Metadata")
	}
	if ev.Metadata["type"] != "order" {
		t.Errorf("expected Metadata['type'] = 'order', got %q", ev.Metadata["type"])
	}

	orderJSON, ok := ev.Metadata["order_json"]
	if !ok || orderJSON == "" {
		t.Fatalf("expected non-empty Metadata['order_json']")
	}

	var orderEv domain.OrderCreatedEvent
	if err := json.Unmarshal([]byte(orderJSON), &orderEv); err != nil {
		t.Fatalf("failed to unmarshal order_json: %v", err)
	}

	if orderEv.CatalogID != "cat_999" {
		t.Errorf("expected CatalogID 'cat_999', got %q", orderEv.CatalogID)
	}
	if orderEv.TotalPrice != 61.00 {
		t.Errorf("expected TotalPrice 61.00, got %f", orderEv.TotalPrice)
	}
	if orderEv.Currency != "BRL" {
		t.Errorf("expected Currency 'BRL', got %q", orderEv.Currency)
	}
	if len(orderEv.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(orderEv.Items))
	}
	if orderEv.Items[0].ProductRetailerID != "SKU-001" || orderEv.Items[0].Quantity != 2 || orderEv.Items[0].ItemPrice != 15.50 {
		t.Errorf("unexpected item 0: %+v", orderEv.Items[0])
	}

	if !strings.Contains(ev.Body, "🛒 Order Received (Catalog: cat_999)") {
		t.Errorf("expected Body to contain catalog summary header, got: %s", ev.Body)
	}
	if !strings.Contains(ev.Body, "Note: Please deliver quickly") {
		t.Errorf("expected Body to contain note, got: %s", ev.Body)
	}
}


func TestWABAAdapter_SSRFProtection(t *testing.T) {
	client := netpolicy.NewPublicHTTPClient()
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:9999", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	_, err = client.Do(req)
	if err == nil {
		t.Fatalf("expected error for restricted IP, got nil")
	}
}
