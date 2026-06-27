package whatsapp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pablojhp.omnigo/internal/channel"
	"github.com/pablojhp.omnigo/internal/domain"
	"github.com/pablojhp.omnigo/internal/platform/crypto"
	"github.com/pablojhp.omnigo/internal/platform/postgres"
	"github.com/pablojhp.omnigo/internal/platform/postgres/tenant"
	"github.com/pablojhp.omnigo/internal/repository"
)

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("OMNIGO_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/omnigo?sslmode=disable"
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
	credsRepo := repository.NewCredentialsRepository(pool, enc)
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
	err = credsRepo.Save(ctx, ws.ID, "whatsapp_cloud", configBytes)
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
			if req.To != "+5511999999999" || req.Type != "text" || req.Text == nil || req.Text.Body != "Hello from OmniGo!" {
				t.Errorf("unexpected payload details: %+v", req)
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"messaging_product":"whatsapp","contacts":[{"input":"+5511999999999","wa_id":"5511999999999"}],"messages":[{"id":"wamid.HBgL..."}]}`))
		}))
		defer server.Close()

		adapter := NewWABAAdapter(credsRepo, nil, nil)
		adapter.SetBaseURL(server.URL)

		payload := &channel.MessagePayload{
			To:   "+5511999999999",
			Body: "Hello from OmniGo!",
		}

		err := adapter.Dispatch(tenantCtx, payload)
		if err != nil {
			t.Fatalf("expected nil error on success, got: %v", err)
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
			if req.Template.Components[0].Parameters[0].Text != "Pablo" || req.Template.Components[0].Parameters[1].Text != "OmniGo" {
				t.Errorf("unexpected parameters content: %+v", req.Template.Components[0].Parameters)
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.HBgL..."}]}`))
		}))
		defer server.Close()

		adapter := NewWABAAdapter(credsRepo, nil, nil)
		adapter.SetBaseURL(server.URL)

		payload := &channel.MessagePayload{
			To: "+5511999999999",
			Metadata: map[string]string{
				"template_name":     "welcome_test",
				"template_language": "pt_BR",
				"param1":            "Pablo",
				"param2":            "OmniGo",
			},
		}

		err := adapter.Dispatch(tenantCtx, payload)
		if err != nil {
			t.Fatalf("expected nil error on template dispatch, got: %v", err)
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

		adapter := NewWABAAdapter(credsRepo, nil, nil)
		adapter.SetBaseURL(server.URL)

		payload := &channel.MessagePayload{
			To:           "+5511999999999",
			TemplateName: "welcome_test_new",
			Language:     "en_US",
			Components: []channel.TemplateComponent{
				{
					Type: "body",
					Parameters: []channel.TemplateParameter{
						{Type: "text", Text: "Alice"},
					},
				},
			},
		}

		err := adapter.Dispatch(tenantCtx, payload)
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

		adapter := NewWABAAdapter(credsRepo, nil, nil)
		adapter.SetBaseURL(server.URL)

		err := adapter.Dispatch(tenantCtx, &channel.MessagePayload{To: "+12345", Body: "hi"})
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

		adapter := NewWABAAdapter(credsRepo, nil, nil)
		adapter.SetBaseURL(server.URL)

		err := adapter.Dispatch(tenantCtx, &channel.MessagePayload{To: "+12345", Body: "hi"})
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

		adapter := NewWABAAdapter(credsRepo, nil, nil)
		adapter.SetBaseURL(server.URL)

		err := adapter.Dispatch(tenantCtx, &channel.MessagePayload{To: "+12345", Body: "hi"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if channel.IsTerminal(err) {
			t.Errorf("expected error to be transient, got terminal: %v", err)
		}
	})

	t.Run("Local Window Checker - Expired/Missing", func(t *testing.T) {
		mockChecker := &mockWABAWindowChecker{open: false}
		adapter := NewWABAAdapter(credsRepo, nil, mockChecker)

		err := adapter.Dispatch(tenantCtx, &channel.MessagePayload{To: "+12345", Body: "hi"})
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
		adapter := NewWABAAdapter(credsRepo, nil, mockChecker)
		adapter.SetBaseURL(server.URL)

		err := adapter.Dispatch(tenantCtx, &channel.MessagePayload{To: "+12345", Body: "hi"})
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

		adapter := NewWABAAdapter(credsRepo, nil, nil)
		adapter.SetBaseURL(server.URL)

		payload := &channel.MessagePayload{
			To: "+5511999999999",
			Media: &domain.Media{
				MediaURL:  "/media/workspace123/hash123.png",
				MediaType: "image",
				Caption:   "Test Caption",
			},
		}

		err := adapter.Dispatch(tenantCtx, payload)
		if err != nil {
			t.Fatalf("expected nil error on success, got: %v", err)
		}
	})
}

type mockWABAWindowChecker struct {
	open bool
	err  error
}

func (m *mockWABAWindowChecker) IsWindowOpen(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channelName string) (bool, error) {
	return m.open, m.err
}
