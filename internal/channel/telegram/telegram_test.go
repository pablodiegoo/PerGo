package telegram

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/platform/storage"
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
	connectionsRepo := repository.NewConnectionRepository(pool, enc)
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
	connID := uuid.New()
	err = connectionsRepo.Create(ctx, &repository.Connection{
		ID:             connID,
		WorkspaceID:    ws.ID,
		Name:           "Telegram",
		Channel:        "telegram",
		SenderIdentity: "@test_bot",
		Status:         "active",
		Credentials:    configBytes,
	})
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

		adapter := NewTelegramAdapter(connectionsRepo, server.Client(), nil)
		adapter.SetBaseURL(server.URL)

		payload := &channel.MessagePayload{
			ConnectionID:   connID,
			SenderIdentity: "@test_bot",
			To:             "987654321",
			Body:           "Hello Telegram!",
		}

		_, err := adapter.Dispatch(tenantCtx, payload)
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

		adapter := NewTelegramAdapter(connectionsRepo, server.Client(), nil)
		adapter.SetBaseURL(server.URL)

		_, err := adapter.Dispatch(tenantCtx, &channel.MessagePayload{
			ConnectionID:   connID,
			SenderIdentity: "@test_bot",
			To:             "invalid_chat",
			Body:           "hi",
		})
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

		adapter := NewTelegramAdapter(connectionsRepo, server.Client(), nil)
		adapter.SetBaseURL(server.URL)

		_, err := adapter.Dispatch(tenantCtx, &channel.MessagePayload{
			ConnectionID:   connID,
			SenderIdentity: "@test_bot",
			To:             "blocked_chat",
			Body:           "hi",
		})
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

		adapter := NewTelegramAdapter(connectionsRepo, server.Client(), nil)
		adapter.SetBaseURL(server.URL)

		_, err := adapter.Dispatch(tenantCtx, &channel.MessagePayload{
			ConnectionID:   connID,
			SenderIdentity: "@test_bot",
			To:             "987654321",
			Body:           "hi",
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if channel.IsTerminal(err) {
			t.Errorf("expected error to be transient, got terminal: %v", err)
		}
	})

	t.Run("Success Send Media (Telegram)", func(t *testing.T) {
		// Setup S3 Client and upload mock file
		s3Client, err := storage.NewS3Client("http://localhost:9000", "us-east-1", "minioadmin", "minioadmin", "pergo-bucket", true)
		if err != nil {
			t.Fatalf("failed to init s3 client: %v", err)
		}

		key := ws.ID.String() + "/hash123.png"
		fileData := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
		err = s3Client.Upload(context.Background(), key, fileData, "image/png")
		if err != nil {
			t.Fatalf("failed to upload mock file to S3: %v", err)
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify endpoint path
			if r.URL.Path != "/bot123456:ABC-DEF_test_token/sendPhoto" {
				t.Errorf("path = %q, want /bot123456:ABC-DEF_test_token/sendPhoto", r.URL.Path)
			}

			// Verify multipart payload has photo block, caption and chat_id
			err := r.ParseMultipartForm(10 << 20)
			if err != nil {
				t.Fatalf("failed to parse multipart form: %v", err)
			}

			if r.FormValue("chat_id") != "987654321" {
				t.Errorf("expected chat_id 987654321, got %s", r.FormValue("chat_id"))
			}

			if r.FormValue("caption") != "Test Caption" {
				t.Errorf("expected caption Test Caption, got %s", r.FormValue("caption"))
			}

			file, header, err := r.FormFile("photo")
			if err != nil {
				t.Fatalf("failed to read photo file from multipart form: %v", err)
			}
			defer file.Close()

			if header.Filename != "custom_filename.png" {
				t.Errorf("expected filename custom_filename.png, got %s", header.Filename)
			}

			uploadedData, _ := io.ReadAll(file)
			if string(uploadedData) != string(fileData) {
				t.Errorf("expected uploaded data %s, got %s", string(fileData), string(uploadedData))
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":12345}}`))
		}))
		defer server.Close()

		adapter := NewTelegramAdapter(connectionsRepo, server.Client(), s3Client)
		adapter.SetBaseURL(server.URL)

		payload := &channel.MessagePayload{
			ConnectionID:   connID,
			SenderIdentity: "@test_bot",
			To:             "987654321",
			Media: &domain.Media{
				MediaURL:  "/media/" + ws.ID.String() + "/hash123.png",
				MediaType: "image",
				Caption:   "Test Caption",
				Filename:  "custom_filename.png",
			},
		}

		_, err = adapter.Dispatch(tenantCtx, payload)
		if err != nil {
			t.Fatalf("expected nil error on success, got: %v", err)
		}
	})

	t.Run("Success Send Audio", func(t *testing.T) {
		s3Client, err := storage.NewS3Client("http://localhost:9000", "us-east-1", "minioadmin", "minioadmin", "pergo-bucket", true)
		if err != nil {
			t.Fatalf("failed to init s3 client: %v", err)
		}

		key := ws.ID.String() + "/audio123.mp3"
		fileData := []byte("fake-mp3-audio-bytes")
		err = s3Client.Upload(context.Background(), key, fileData, "audio/mpeg")
		if err != nil {
			t.Fatalf("failed to upload mock file to S3: %v", err)
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/bot123456:ABC-DEF_test_token/sendAudio" {
				t.Errorf("path = %q, want /bot123456:ABC-DEF_test_token/sendAudio", r.URL.Path)
			}

			err := r.ParseMultipartForm(10 << 20)
			if err != nil {
				t.Fatalf("failed to parse multipart form: %v", err)
			}

			if r.FormValue("chat_id") != "987654321" {
				t.Errorf("expected chat_id 987654321, got %s", r.FormValue("chat_id"))
			}

			file, _, err := r.FormFile("audio")
			if err != nil {
				t.Fatalf("failed to read audio file from multipart form: %v", err)
			}
			defer file.Close()

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":12346}}`))
		}))
		defer server.Close()

		adapter := NewTelegramAdapter(connectionsRepo, server.Client(), s3Client)
		adapter.SetBaseURL(server.URL)

		payload := &channel.MessagePayload{
			ConnectionID:   connID,
			SenderIdentity: "@test_bot",
			To:             "987654321",
			Media: &domain.Media{
				MediaURL:  "/media/" + ws.ID.String() + "/audio123.mp3",
				MediaType: "audio",
			},
		}

		_, err = adapter.Dispatch(tenantCtx, payload)
		if err != nil {
			t.Fatalf("expected nil error on success, got: %v", err)
		}
	})

	t.Run("Success Send Voice Note", func(t *testing.T) {
		s3Client, err := storage.NewS3Client("http://localhost:9000", "us-east-1", "minioadmin", "minioadmin", "pergo-bucket", true)
		if err != nil {
			t.Fatalf("failed to init s3 client: %v", err)
		}

		key := ws.ID.String() + "/voice123.ogg"
		fileData := []byte("fake-ogg-voice-bytes")
		err = s3Client.Upload(context.Background(), key, fileData, "audio/ogg; codecs=opus")
		if err != nil {
			t.Fatalf("failed to upload mock file to S3: %v", err)
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/bot123456:ABC-DEF_test_token/sendVoice" {
				t.Errorf("path = %q, want /bot123456:ABC-DEF_test_token/sendVoice", r.URL.Path)
			}

			err := r.ParseMultipartForm(10 << 20)
			if err != nil {
				t.Fatalf("failed to parse multipart form: %v", err)
			}

			if r.FormValue("chat_id") != "987654321" {
				t.Errorf("expected chat_id 987654321, got %s", r.FormValue("chat_id"))
			}

			file, _, err := r.FormFile("voice")
			if err != nil {
				t.Fatalf("failed to read voice file from multipart form: %v", err)
			}
			defer file.Close()

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":12347}}`))
		}))
		defer server.Close()

		adapter := NewTelegramAdapter(connectionsRepo, server.Client(), s3Client)
		adapter.SetBaseURL(server.URL)

		payload := &channel.MessagePayload{
			ConnectionID:   connID,
			SenderIdentity: "@test_bot",
			To:             "987654321",
			Media: &domain.Media{
				MediaURL:  "/media/" + ws.ID.String() + "/voice123.ogg",
				MediaType: "voice",
				PTT:       true,
			},
		}

		_, err = adapter.Dispatch(tenantCtx, payload)
		if err != nil {
			t.Fatalf("expected nil error on success, got: %v", err)
		}
	})

	t.Run("Success Send Interactive", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/bot123456:ABC-DEF_test_token/sendMessage" {
				t.Errorf("path = %q, want /bot123456:ABC-DEF_test_token/sendMessage", r.URL.Path)
			}

			bodyBytes, _ := io.ReadAll(r.Body)
			var req telegramMessageRequest
			if err := json.Unmarshal(bodyBytes, &req); err != nil {
				t.Fatalf("failed to unmarshal request body: %v", err)
			}

			if req.MessageThreadID != "42" {
				t.Errorf("expected message_thread_id 42, got %s", req.MessageThreadID)
			}

			if req.ReplyMarkup == nil || len(req.ReplyMarkup.InlineKeyboard) == 0 {
				t.Errorf("expected reply_markup with inline_keyboard, got %+v", req.ReplyMarkup)
			} else {
				btn := req.ReplyMarkup.InlineKeyboard[0][0]
				if btn.Text != "Click Me" || btn.CallbackData != "btn_1" {
					t.Errorf("unexpected button data: %+v", btn)
				}
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":12346}}`))
		}))
		defer server.Close()

		adapter := NewTelegramAdapter(connectionsRepo, server.Client(), nil)
		adapter.SetBaseURL(server.URL)

		payload := &channel.MessagePayload{
			ConnectionID:   connID,
			SenderIdentity: "@test_bot",
			To:             "987654321",
			Body:           "Hello with Buttons!",
			Metadata: map[string]string{
				"thread_id": "42",
			},
			Interactive: &domain.Interactive{
				Type: "button",
				Action: domain.Action{
					Buttons: []domain.Button{
						{
							Type: "reply",
							Reply: domain.Reply{
								ID:    "btn_1",
								Title: "Click Me",
							},
						},
					},
				},
			},
		}

		_, err := adapter.Dispatch(tenantCtx, payload)
		if err != nil {
			t.Fatalf("expected nil error on success, got: %v", err)
		}
	})

	t.Run("Telegram Interactive List Degrade to Inline Keyboard and Menu", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bodyBytes, _ := io.ReadAll(r.Body)
			var req telegramMessageRequest
			if err := json.Unmarshal(bodyBytes, &req); err != nil {
				t.Fatalf("failed to unmarshal request body: %v", err)
			}

			if !strings.Contains(req.Text, "Menu Header") || !strings.Contains(req.Text, "Choose an option") {
				t.Errorf("expected text to contain Header and Body, got: %s", req.Text)
			}

			if req.ReplyMarkup == nil || len(req.ReplyMarkup.InlineKeyboard) != 2 {
				t.Fatalf("expected 2 inline keyboard rows, got: %+v", req.ReplyMarkup)
			}

			if req.ReplyMarkup.InlineKeyboard[0][0].Text != "Option 1" || req.ReplyMarkup.InlineKeyboard[0][0].CallbackData != "opt_1" {
				t.Errorf("unexpected button 0: %+v", req.ReplyMarkup.InlineKeyboard[0][0])
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":12347}}`))
		}))
		defer server.Close()

		adapter := NewTelegramAdapter(connectionsRepo, server.Client(), nil)
		adapter.SetBaseURL(server.URL)

		payload := &channel.MessagePayload{
			ConnectionID:     connID,
			SenderIdentity:   "@test_bot",
			To:               "987654321",
			FallbackBehavior: "degrade",
			Interactive: &domain.Interactive{
				Type:   "list",
				Header: &domain.TextContent{Text: "Menu Header"},
				Body:   domain.TextContent{Text: "Choose an option"},
				Action: domain.Action{
					Button: "Options",
					Sections: []domain.Section{
						{
							Title: "Main Options",
							Rows: []domain.Row{
								{ID: "opt_1", Title: "Option 1"},
								{ID: "opt_2", Title: "Option 2"},
							},
						},
					},
				},
			},
		}

		_, err := adapter.Dispatch(tenantCtx, payload)
		if err != nil {
			t.Fatalf("expected nil error on degrade, got: %v", err)
		}
	})

	t.Run("Telegram Interactive List Fail on fallback_behavior fail", func(t *testing.T) {
		adapter := NewTelegramAdapter(connectionsRepo, nil, nil)

		payload := &channel.MessagePayload{
			ConnectionID:     connID,
			SenderIdentity:   "@test_bot",
			To:               "987654321",
			FallbackBehavior: "fail",
			Interactive: &domain.Interactive{
				Type: "list",
				Body: domain.TextContent{Text: "Choose an option"},
				Action: domain.Action{
					Button: "Options",
					Sections: []domain.Section{
						{
							Title: "Main Options",
							Rows: []domain.Row{
								{ID: "opt_1", Title: "Option 1"},
							},
						},
					},
				},
			},
		}

		_, err := adapter.Dispatch(tenantCtx, payload)
		if err == nil {
			t.Fatal("expected error on fallback_behavior fail for list, got nil")
		}
		if !channel.IsTerminal(err) {
			t.Errorf("expected terminal error, got: %v", err)
		}
	})

	t.Run("Telegram Interactive Flow Degrade", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bodyBytes, _ := io.ReadAll(r.Body)
			var req telegramMessageRequest
			if err := json.Unmarshal(bodyBytes, &req); err != nil {
				t.Fatalf("failed to unmarshal request body: %v", err)
			}

			if !strings.Contains(req.Text, "Survey Title") || !strings.Contains(req.Text, "Please fill survey") {
				t.Errorf("expected text to contain survey, got: %s", req.Text)
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":12348}}`))
		}))
		defer server.Close()

		adapter := NewTelegramAdapter(connectionsRepo, server.Client(), nil)
		adapter.SetBaseURL(server.URL)

		payload := &channel.MessagePayload{
			ConnectionID:     connID,
			SenderIdentity:   "@test_bot",
			To:               "987654321",
			FallbackBehavior: "degrade",
			Interactive: &domain.Interactive{
				Type:   "flow",
				Header: &domain.TextContent{Text: "Survey Title"},
				Body:   domain.TextContent{Text: "Please fill survey"},
				Action: domain.Action{
					FlowCTA: "Open Survey",
					FlowID:  "flow_1",
				},
			},
		}

		_, err := adapter.Dispatch(tenantCtx, payload)
		if err != nil {
			t.Fatalf("expected nil error on degrade, got: %v", err)
		}
	})

	t.Run("Telegram Interactive Flow Fail on fallback_behavior fail", func(t *testing.T) {
		adapter := NewTelegramAdapter(connectionsRepo, nil, nil)

		payload := &channel.MessagePayload{
			ConnectionID:     connID,
			SenderIdentity:   "@test_bot",
			To:               "987654321",
			FallbackBehavior: "fail",
			Interactive: &domain.Interactive{
				Type: "flow",
				Body: domain.TextContent{Text: "Please fill survey"},
				Action: domain.Action{
					FlowCTA: "Open Survey",
					FlowID:  "flow_1",
				},
			},
		}

		_, err := adapter.Dispatch(tenantCtx, payload)
		if err == nil {
			t.Fatal("expected error on fallback_behavior fail for flow, got nil")
		}
		if !channel.IsTerminal(err) {
			t.Errorf("expected terminal error, got: %v", err)
		}
	})

	t.Run("Telegram Response Body > 5MB Returns Error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			// Write > 5MB payload
			bigData := make([]byte, 5*1024*1024+100)
			_, _ = w.Write(bigData)
		}))
		defer server.Close()

		adapter := NewTelegramAdapter(connectionsRepo, server.Client(), nil)
		adapter.SetBaseURL(server.URL)

		_, err := adapter.Dispatch(tenantCtx, &channel.MessagePayload{
			ConnectionID:   connID,
			SenderIdentity: "@test_bot",
			To:             "987654321",
			Body:           "Too large response test",
		})
		if err == nil {
			t.Fatal("expected error for response body > 5MB, got nil")
		}
		if !strings.Contains(err.Error(), "exceeds 5MB limit") {
			t.Errorf("expected error containing 'exceeds 5MB limit', got: %v", err)
		}
	})

	t.Run("Telegram S3 Media Download Failure Error Wrapping", func(t *testing.T) {
		s3Client, err := storage.NewS3Client("http://localhost:9000", "us-east-1", "minioadmin", "minioadmin", "pergo-bucket", true)
		if err != nil {
			t.Fatalf("failed to init s3 client: %v", err)
		}

		adapter := NewTelegramAdapter(connectionsRepo, nil, s3Client)

		payload := &channel.MessagePayload{
			ConnectionID:   connID,
			SenderIdentity: "@test_bot",
			To:             "987654321",
			Media: &domain.Media{
				MediaURL:  "/media/" + ws.ID.String() + "/nonexistent.png",
				MediaType: "image",
			},
		}

		_, err = adapter.Dispatch(tenantCtx, payload)
		if err == nil {
			t.Fatal("expected error for nonexistent S3 key, got nil")
		}
		if !errors.Is(err, ErrTelegramMediaRetryable) {
			t.Errorf("expected error to wrap ErrTelegramMediaRetryable, got: %v", err)
		}
		if unwrapped := errors.Unwrap(err); unwrapped != ErrTelegramMediaRetryable {
			t.Errorf("expected errors.Unwrap(err) to equal ErrTelegramMediaRetryable, got: %v", unwrapped)
		}
	})
}
