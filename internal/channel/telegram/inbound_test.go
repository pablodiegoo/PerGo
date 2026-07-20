package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestTelegramInboundAdapter_Parse(t *testing.T) {
	// Setup mock server to capture answerCallbackQuery
	var lastCallbackAck string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/botTEST_TOKEN/answerCallbackQuery" {
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
				lastCallbackAck = body["callback_query_id"]
			}
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	adapter := NewTelegramInboundAdapter(nil)
	adapter.SetBaseURL(server.URL)

	conn := &repository.Connection{
		ID:          uuid.New(),
		WorkspaceID: uuid.New(),
		Credentials: []byte(`{"token":"TEST_TOKEN","secret_token":"SECRET123","bot_username":"@test_bot"}`),
	}
	headers := map[string]string{
		"X-Telegram-Bot-Api-Secret-Token": "SECRET123",
	}

	t.Run("Normal Message with thread_id", func(t *testing.T) {
		payload := []byte(`{
			"update_id": 1,
			"message": {
				"message_id": 100,
				"message_thread_id": 42,
				"from": {"id": 123, "username": "user1"},
				"chat": {"id": 456, "type": "group"},
				"text": "Hello in thread"
			}
		}`)

		events, err := adapter.Parse(context.Background(), payload, headers, conn)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}
		ev := events[0]
		if ev.Metadata["thread_id"] != "42" {
			t.Errorf("expected thread_id=42, got %s", ev.Metadata["thread_id"])
		}
		if ev.Body != "Hello in thread" {
			t.Errorf("expected body 'Hello in thread', got %s", ev.Body)
		}
	})

	t.Run("Callback Query", func(t *testing.T) {
		lastCallbackAck = "" // reset
		payload := []byte(`{
			"update_id": 2,
			"callback_query": {
				"id": "cb_123",
				"from": {"id": 123, "username": "user1"},
				"message": {
					"message_id": 101,
					"message_thread_id": 42,
					"chat": {"id": 456, "type": "group"}
				},
				"data": "btn_action_1"
			}
		}`)

		events, err := adapter.Parse(context.Background(), payload, headers, conn)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}
		ev := events[0]
		if ev.Metadata["thread_id"] != "42" {
			t.Errorf("expected thread_id=42, got %s", ev.Metadata["thread_id"])
		}
		if ev.Interactive == nil {
			t.Fatalf("expected Interactive to be populated")
		}
		if ev.Interactive.Type != "button_reply" {
			t.Errorf("expected Interactive.Type=button_reply, got %s", ev.Interactive.Type)
		}
		if ev.Interactive.ButtonReply.ID != "btn_action_1" {
			t.Errorf("expected ButtonReply.ID=btn_action_1, got %s", ev.Interactive.ButtonReply.ID)
		}

		// wait a tiny bit for the async ack to complete
		time.Sleep(50 * time.Millisecond)
		if lastCallbackAck != "cb_123" {
			t.Errorf("expected answerCallbackQuery to be called with cb_123, got %s", lastCallbackAck)
		}
	})
}
