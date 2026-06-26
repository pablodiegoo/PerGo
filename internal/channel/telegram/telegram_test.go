package telegram

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

func TestTelegramDispatch(t *testing.T) {
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
	ws, err := wsRepo.Create(ctx, "telegram_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	// Save test credentials
	telegramConfig := TelegramConfig{
		Token: "123456:ABC-DEF_test_token",
	}
	configBytes, _ := json.Marshal(telegramConfig)
	err = credsRepo.Save(ctx, ws.ID, "telegram", configBytes)
	if err != nil {
		t.Fatalf("failed to save Telegram credentials: %v", err)
	}

	// Setup tenant context
	tenantCtx := tenant.WithWorkspaceID(context.Background(), ws.ID)

	t.Run("Success Send Message", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify Content-Type
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("Content-Type header = %q, want application/json", r.Header.Get("Content-Type"))
			}

			// Verify endpoint path
			if r.URL.Path != "/bot123456:ABC-DEF_test_token/sendMessage" {
				t.Errorf("path = %q, want /bot123456:ABC-DEF_test_token/sendMessage", r.URL.Path)
			}

			// Verify payload
			bodyBytes, _ := io.ReadAll(r.Body)
			var req telegramMessageRequest
			if err := json.Unmarshal(bodyBytes, &req); err != nil {
				t.Fatalf("failed to unmarshal request body: %v", err)
			}

			if req.ChatID != "987654321" || req.Text != "Hello Telegram!" {
				t.Errorf("unexpected payload details: %+v", req)
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":12345}}`))
		}))
		defer server.Close()

		adapter := NewTelegramAdapter(credsRepo, nil)
		adapter.SetBaseURL(server.URL)

		payload := &channel.MessagePayload{
			To:   "987654321",
			Body: "Hello Telegram!",
		}

		err := adapter.Dispatch(tenantCtx, payload)
		if err != nil {
			t.Fatalf("expected nil error on success, got: %v", err)
		}
	})

	t.Run("Terminal Error - Chat Not Found (400)", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`))
		}))
		defer server.Close()

		adapter := NewTelegramAdapter(credsRepo, nil)
		adapter.SetBaseURL(server.URL)

		err := adapter.Dispatch(tenantCtx, &channel.MessagePayload{To: "invalid_chat", Body: "hi"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !channel.IsTerminal(err) {
			t.Errorf("expected error to be terminal, got: %v", err)
		}
	})

	t.Run("Terminal Error - Bot Blocked (403)", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`))
		}))
		defer server.Close()

		adapter := NewTelegramAdapter(credsRepo, nil)
		adapter.SetBaseURL(server.URL)

		err := adapter.Dispatch(tenantCtx, &channel.MessagePayload{To: "blocked_chat", Body: "hi"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !channel.IsTerminal(err) {
			t.Errorf("expected error to be terminal, got: %v", err)
		}
	})

	t.Run("Transient Error - Too Many Requests (429)", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 5 seconds"}`))
		}))
		defer server.Close()

		adapter := NewTelegramAdapter(credsRepo, nil)
		adapter.SetBaseURL(server.URL)

		err := adapter.Dispatch(tenantCtx, &channel.MessagePayload{To: "987654321", Body: "hi"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if channel.IsTerminal(err) {
			t.Errorf("expected error to be transient, got terminal: %v", err)
		}
	})
}
