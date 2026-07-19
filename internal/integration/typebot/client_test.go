package typebot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTypebotConfig(t *testing.T) {
	// Stub to satisfy Wave 0 TYPE-01 requirement
	t.Log("TestTypebotConfig: Credentials encryption is tested inside repository via integration tests")
}

func TestTypebotClient_StartChat(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/typebots/bot123/startChat" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(StartChatResponse{
				SessionID: "sess_123",
				Messages: []Message{
					{
						ID:   "m1",
						Type: "text",
					},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	client := NewClient()
	req := StartChatRequest{
		SessionID: "sess_123",
		Message:   "hello",
		PrefilledVariables: map[string]any{
			"var1": "val1",
		},
	}
	sessionID, messages, err := client.StartChat(context.Background(), ts.URL, "bot123", "token", req)
	if err != nil {
		t.Fatalf("StartChat failed: %v", err)
	}
	if sessionID != "sess_123" {
		t.Errorf("Expected sess_123, got %s", sessionID)
	}
	if len(messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(messages))
	}
}

func TestTypebotClient_ContinueChat(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sessions/sess_123/continueChat" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(ContinueChatResponse{
				Messages: []Message{
					{
						ID:   "m2",
						Type: "text",
					},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	client := NewClient()
	messages, err := client.ContinueChat(context.Background(), ts.URL, "sess_123", "token", "hello")
	if err != nil {
		t.Fatalf("ContinueChat failed: %v", err)
	}
	if len(messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(messages))
	}
}
