package chatwoot_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pablojhp.pergo/internal/integration/chatwoot"
)

func TestChatwootClient(t *testing.T) {
	t.Run("SearchContact_Found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/accounts/1/contacts/search" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			if r.URL.Query().Get("q") != "some-uuid" {
				t.Errorf("unexpected query q: %s", r.URL.Query().Get("q"))
			}
			if r.Header.Get("api_access_token") != "test-token" {
				t.Errorf("unexpected token: %s", r.Header.Get("api_access_token"))
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"payload": [
					{
						"id": 123,
						"identifier": "some-uuid"
					}
				]
			}`))
		}))
		defer server.Close()

		client := chatwoot.NewChatwootClient(server.URL, "test-token", 1, server.Client())
		id, err := client.SearchContact(context.Background(), "some-uuid")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != 123 {
			t.Errorf("expected ID 123, got %d", id)
		}
	})

	t.Run("SearchContact_NotFound", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"payload": []
			}`))
		}))
		defer server.Close()

		client := chatwoot.NewChatwootClient(server.URL, "test-token", 1, server.Client())
		_, err := client.SearchContact(context.Background(), "some-uuid")
		if err != chatwoot.ErrNotFound {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("CreateContact", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("unexpected method: %s", r.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{
				"payload": {
					"contact": {
						"id": 456
					}
				}
			}`))
		}))
		defer server.Close()

		client := chatwoot.NewChatwootClient(server.URL, "test-token", 1, server.Client())
		id, err := client.CreateContact(context.Background(), "some-uuid", "John Doe", "+123", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != 456 {
			t.Errorf("expected ID 456, got %d", id)
		}
	})

	t.Run("UpdateContact", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPut {
				t.Errorf("unexpected method: %s", r.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"payload": {
					"contact": {
						"id": 456
					}
				}
			}`))
		}))
		defer server.Close()

		client := chatwoot.NewChatwootClient(server.URL, "test-token", 1, server.Client())
		id, err := client.UpdateContact(context.Background(), 456, "John Doe", "+123", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != 456 {
			t.Errorf("expected ID 456, got %d", id)
		}
	})

	t.Run("CreateConversation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"id": 789
			}`))
		}))
		defer server.Close()

		client := chatwoot.NewChatwootClient(server.URL, "test-token", 1, server.Client())
		id, err := client.CreateConversation(context.Background(), 456, 2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != 789 {
			t.Errorf("expected ID 789, got %d", id)
		}
	})

	t.Run("PostMessage_Success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := chatwoot.NewChatwootClient(server.URL, "test-token", 1, server.Client())
		err := client.PostMessage(context.Background(), 789, "hello", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("PostMessage_NotFound", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		client := chatwoot.NewChatwootClient(server.URL, "test-token", 1, server.Client())
		err := client.PostMessage(context.Background(), 789, "hello", false)
		if err != chatwoot.ErrNotFound {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})
}
