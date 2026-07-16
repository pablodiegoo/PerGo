package webhook_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/webhook"
)

type mockConfigStore struct {
	cfg       *repository.WebhookConfig
	getConfig func(workspaceID uuid.UUID) (*repository.WebhookConfig, error)
	inserted  []struct {
		workspaceID    uuid.UUID
		subscriptionID uuid.UUID
		traceID        string
		messageID      string
		eventType      string
		payload        []byte
		url            string
		attempts       int
		failureReason  *string
	}
}

func (m *mockConfigStore) GetConfig(ctx context.Context, workspaceID uuid.UUID) (*repository.WebhookConfig, error) {
	if m.getConfig != nil {
		return m.getConfig(workspaceID)
	}
	return m.cfg, nil
}

func (m *mockConfigStore) InsertDLQ(
	ctx context.Context,
	workspaceID uuid.UUID,
	subscriptionID uuid.UUID,
	traceID, messageID, eventType string,
	payload []byte,
	url string,
	attempts int,
	failureReason *string,
) error {
	m.inserted = append(m.inserted, struct {
		workspaceID    uuid.UUID
		subscriptionID uuid.UUID
		traceID        string
		messageID      string
		eventType      string
		payload        []byte
		url            string
		attempts       int
		failureReason  *string
	}{workspaceID, subscriptionID, traceID, messageID, eventType, payload, url, attempts, failureReason})
	return nil
}

type mockWorkspaceStore struct {
	ws *repository.Workspace
}

func (m *mockWorkspaceStore) GetByID(ctx context.Context, id uuid.UUID) (*repository.Workspace, error) {
	return m.ws, nil
}

type mockHTTPClient struct {
	requests []*http.Request
	response *http.Response
	err      error
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.err != nil {
		return nil, m.err
	}
	m.requests = append(m.requests, req)
	if m.response != nil {
		return m.response, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader([]byte(""))),
	}, nil
}

func TestDefaultDispatcher_Dispatch(t *testing.T) {
	wsID := uuid.New()
	cfg := &repository.WebhookConfig{
		URL:    "https://api.myweb.com/events",
		Secret: []byte("signing-secret-key-123"),
	}

	t.Run("Normal dispatch signs and sends HTTP request", func(t *testing.T) {
		configStore := &mockConfigStore{cfg: cfg}
		httpClient := &mockHTTPClient{}
		d := webhook.NewDefaultDispatcher(configStore, nil, httpClient)

		event := map[string]string{
			"event":        "message.sent",
			"trace_id":     "trace-1",
			"message_id":   "msg-1",
			"workspace_id": wsID.String(),
		}
		rawBytes, _ := json.Marshal(event)

		err := d.Dispatch(context.Background(), "outbound", rawBytes)
		if err != nil {
			t.Fatalf("Dispatch failed: %v", err)
		}

		if len(httpClient.requests) != 1 {
			t.Fatalf("expected 1 HTTP request, got %d", len(httpClient.requests))
		}
		req := httpClient.requests[0]
		if req.URL.String() != cfg.URL {
			t.Errorf("got URL %s, want %s", req.URL, cfg.URL)
		}
		if req.Header.Get("X-Trace-ID") != "trace-1" {
			t.Errorf("got X-Trace-ID %q, want trace-1", req.Header.Get("X-Trace-ID"))
		}
		if req.Header.Get("X-PerGo-Signature") == "" {
			t.Errorf("expected signature header to be present")
		}
	})

	t.Run("Compliance checks redact PII on inbound events when workspace opted-out", func(t *testing.T) {
		configStore := &mockConfigStore{cfg: cfg}
		workspaceStore := &mockWorkspaceStore{
			ws: &repository.Workspace{ID: wsID, PIIOptIn: false},
		}
		httpClient := &mockHTTPClient{}
		d := webhook.NewDefaultDispatcher(configStore, workspaceStore, httpClient)

		inboundEvent := map[string]any{
			"event":        "message.received",
			"trace_id":     "trace-2",
			"message_id":   "msg-2",
			"workspace_id": wsID.String(),
			"from":         "+5511999998888",
			"body":         "Sensitive message content",
			"location": map[string]any{
				"latitude":  12.34,
				"longitude": 56.78,
			},
		}
		rawBytes, _ := json.Marshal(inboundEvent)

		err := d.Dispatch(context.Background(), "inbound", rawBytes)
		if err != nil {
			t.Fatalf("Dispatch failed: %v", err)
		}

		req := httpClient.requests[0]
		bodyBytes, _ := io.ReadAll(req.Body)
		var sentEvent map[string]any
		json.Unmarshal(bodyBytes, &sentEvent)

		// Verification: 'from' should be hashed, 'location' should be nil
		fromVal := sentEvent["from"].(string)
		if fromVal == "+5511999998888" {
			t.Errorf("expected 'from' number to be hashed and redacted, but got original %q", fromVal)
		}
		if len(fromVal) != 64 {
			t.Errorf("expected SHA-256 hex string (64 characters) for 'from', got length %d", len(fromVal))
		}
		if sentEvent["location"] != nil {
			t.Errorf("expected location to be stripped/nil, got %v", sentEvent["location"])
		}
	})

	t.Run("Non-2xx HTTP responses wrap HTTPError", func(t *testing.T) {
		configStore := &mockConfigStore{cfg: cfg}
		httpClient := &mockHTTPClient{
			response: &http.Response{
				StatusCode: 502,
				Status:     "502 Bad Gateway",
				Body:       io.NopCloser(bytes.NewReader([]byte(""))),
			},
		}
		d := webhook.NewDefaultDispatcher(configStore, nil, httpClient)

		event := map[string]string{
			"event":        "message.sent",
			"workspace_id": wsID.String(),
		}
		rawBytes, _ := json.Marshal(event)

		err := d.Dispatch(context.Background(), "outbound", rawBytes)
		var httpErr *webhook.HTTPError
		if !errors.As(err, &httpErr) {
			t.Fatalf("expected HTTPError, got %v", err)
		}
		if httpErr.StatusCode != 502 {
			t.Errorf("got status code %d, want 502", httpErr.StatusCode)
		}
	})
}

func TestDefaultDispatcher_WriteToDLQ(t *testing.T) {
	wsID := uuid.New()
	cfg := &repository.WebhookConfig{
		ID:  uuid.New(),
		URL: "https://api.myweb.com/events",
	}

	configStore := &mockConfigStore{cfg: cfg}
	d := webhook.NewDefaultDispatcher(configStore, nil, nil)

	payload := []byte(`{"event":"test"}`)
	err := d.WriteToDLQ(context.Background(), wsID, "trace-123", "msg-123", "message.sent", payload, 3, "something went wrong")
	if err != nil {
		t.Fatalf("WriteToDLQ failed: %v", err)
	}

	if len(configStore.inserted) != 1 {
		t.Fatalf("expected 1 DLQ insertion, got %d", len(configStore.inserted))
	}
	ins := configStore.inserted[0]
	if ins.workspaceID != wsID {
		t.Errorf("got workspace ID %s, want %s", ins.workspaceID, wsID)
	}
	if ins.subscriptionID != cfg.ID {
		t.Errorf("got subscription ID %s, want %s", ins.subscriptionID, cfg.ID)
	}
	if ins.url != cfg.URL {
		t.Errorf("got url %s, want %s", ins.url, cfg.URL)
	}
	if *ins.failureReason != "something went wrong" {
		t.Errorf("got failure reason %s", *ins.failureReason)
	}
}
