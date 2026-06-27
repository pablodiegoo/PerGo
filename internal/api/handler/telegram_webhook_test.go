package handler

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

	"github.com/pablojhp.omnigo/internal/platform/crypto"
	"github.com/pablojhp.omnigo/internal/platform/postgres"
	"github.com/pablojhp.omnigo/internal/platform/storage"
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

func TestTelegramWebhookHandler(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	// 1. Setup Encryptor & repos
	kek := make([]byte, 32)
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	credsRepo := repository.NewCredentialsRepository(pool, enc)
	sessRepo := repository.NewRecipientSessionRepository(pool)
	wsRepo := repository.NewWorkspaceRepository(pool)

	// 2. Create test workspace
	ws, err := wsRepo.Create(ctx, "tg_webhook_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	// 3. Save Telegram credentials with token and secret_token
	configPayload := map[string]string{
		"token":        "bot123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
		"secret_token": "my-secret-telegram-webhook-token",
	}
	configBytes, _ := json.Marshal(configPayload)
	err = credsRepo.Save(ctx, ws.ID, "telegram", configBytes)
	if err != nil {
		t.Fatalf("failed to save telegram credentials: %v", err)
	}

	// Setup Echo
	e := echo.New()
	dedupRepo := repository.NewInboundDedupRepository(pool)
	h := NewTelegramWebhookHandler(wsRepo, credsRepo, sessRepo, dedupRepo, nil, nil, nil)

	t.Run("Missing Secret Token Header -> 403", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/webhooks/telegram/%s", ws.ID), strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/webhooks/telegram/:workspace_id")
		c.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: ws.ID.String()},
		})

		err := h.Handle(c)
		if err != nil {
			t.Fatalf("Handle returned error: %v", err)
		}
		if rec.Code != http.StatusForbidden {
			t.Errorf("got status %d, want 403", rec.Code)
		}
	})

	t.Run("Incorrect Secret Token Header -> 403", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/webhooks/telegram/%s", ws.ID), strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "wrong-secret-token")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/webhooks/telegram/:workspace_id")
		c.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: ws.ID.String()},
		})

		err := h.Handle(c)
		if err != nil {
			t.Fatalf("Handle returned error: %v", err)
		}
		if rec.Code != http.StatusForbidden {
			t.Errorf("got status %d, want 403", rec.Code)
		}
	})

	t.Run("Valid Secret Token Header, Valid Message Update -> 200 and Upsert", func(t *testing.T) {
		body := `{"update_id":1000,"message":{"message_id":999,"chat":{"id":987654321}}}`
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/webhooks/telegram/%s", ws.ID), strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "my-secret-telegram-webhook-token")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/webhooks/telegram/:workspace_id")
		c.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: ws.ID.String()},
		})

		err := h.Handle(c)
		if err != nil {
			t.Fatalf("Handle returned error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("got status %d, want 200", rec.Code)
		}

		// Verify upsert in DB
		sess, err := sessRepo.Get(ctx, ws.ID, "987654321", "telegram")
		if err != nil {
			t.Fatalf("failed to retrieve upserted session: %v", err)
		}
		if time.Since(sess.LastInboundAt) > 10*time.Second {
			t.Errorf("expected LastInboundAt to be recent, got: %v", sess.LastInboundAt)
		}
	})

	t.Run("Valid Secret Token Header, Photo Inbound with PII disabled -> 200", func(t *testing.T) {
		body := `{"update_id":1001,"message":{"message_id":1000,"chat":{"id":987654321},"text":"Caption text","photo":[{"file_id":"photo_id_abc","file_size":5000}],"location":{"latitude":-23.5,"longitude":-46.6}}}`
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/webhooks/telegram/%s", ws.ID), strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "my-secret-telegram-webhook-token")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/webhooks/telegram/:workspace_id")
		c.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: ws.ID.String()},
		})

		// S3 Client Setup
		s3Client, err := storage.NewS3Client("http://localhost:9000", "us-east-1", "minioadmin", "minioadmin", "omnigo-bucket", true)
		if err != nil {
			t.Fatalf("failed to init S3: %v", err)
		}

		// Configure mock Telegram getFile & download server
		tgMockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "getFile") {
				_, _ = w.Write([]byte(`{"ok":true,"result":{"file_path":"photos/file_0.jpg"}}`))
			} else if strings.Contains(r.URL.Path, "photos/file_0.jpg") {
				_, _ = w.Write([]byte{0xff, 0xd8, 0xff, 0xe0}) // JPEG header
			}
		}))
		defer tgMockServer.Close()

		h.s3Client = s3Client
		h.telegramBaseURL = tgMockServer.URL

		err = h.Handle(c)
		if err != nil {
			t.Fatalf("Handle returned error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("got status %d, want 200", rec.Code)
		}
	})
}
