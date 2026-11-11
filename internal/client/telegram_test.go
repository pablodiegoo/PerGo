package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pablojhp.pergo/internal/client"
)

func TestHTTPTelegramBotClient_ValidateToken(t *testing.T) {
	ctx := context.Background()

	t.Run("Valid token", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/botvalid_token/getMe" {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok": true, "result": {"username": "my_test_bot"}}`))
		}))
		defer ts.Close()

		c := client.NewTelegramBotClient(ts.Client(), ts.URL)
		username, err := c.ValidateToken(ctx, "valid_token")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if username != "@my_test_bot" {
			t.Errorf("expected '@my_test_bot', got %q", username)
		}
	})

	t.Run("Unauthorized token", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer ts.Close()

		c := client.NewTelegramBotClient(ts.Client(), ts.URL)
		_, err := c.ValidateToken(ctx, "bad_token")
		if err == nil {
			t.Fatal("expected error for unauthorized token, got nil")
		}
	})
}

func TestHTTPTelegramBotClient_RegisterWebhook(t *testing.T) {
	ctx := context.Background()

	t.Run("Successful registration", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/botvalid_token/setWebhook" {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok": true, "description": "Webhook was set"}`))
		}))
		defer ts.Close()

		c := client.NewTelegramBotClient(ts.Client(), ts.URL)
		err := c.RegisterWebhook(ctx, "valid_token", "https://example.com/webhook", "secret123")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("Failed registration from Telegram API", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok": false, "description": "Bad webhook: HTTPS url must be provided"}`))
		}))
		defer ts.Close()

		c := client.NewTelegramBotClient(ts.Client(), ts.URL)
		err := c.RegisterWebhook(ctx, "valid_token", "http://insecure.com", "secret123")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestHTTPTelegramBotClient_DeleteWebhookAndGetUpdates(t *testing.T) {
	ctx := context.Background()

	var deleted bool
	var lastOffset string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/bottest_tok/deleteWebhook":
			deleted = true
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		case "/bottest_tok/getUpdates":
			lastOffset = r.URL.Query().Get("offset")
			_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":101,"message":{"message_id":1,"chat":{"id":99},"text":"hi"}},{"update_id":102,"message":{"message_id":2,"chat":{"id":99},"text":"there"}}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	c := client.NewTelegramBotClient(ts.Client(), ts.URL)
	if err := c.DeleteWebhook(ctx, "test_tok"); err != nil {
		t.Fatalf("DeleteWebhook failed: %v", err)
	}
	if !deleted {
		t.Fatal("expected deleteWebhook endpoint to be called")
	}

	updates, nextOffset, err := c.GetUpdates(ctx, "test_tok", 50, 5)
	if err != nil {
		t.Fatalf("GetUpdates failed: %v", err)
	}
	if lastOffset != "50" {
		t.Fatalf("expected query offset '50', got %q", lastOffset)
	}
	if len(updates) != 2 {
		t.Fatalf("expected 2 updates, got %d", len(updates))
	}
	if nextOffset != 103 {
		t.Fatalf("expected nextOffset 103, got %d", nextOffset)
	}
}
