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

type mockSubscriptionStore struct {
	sub    *repository.WebhookSubscription
	getSub func(id uuid.UUID) (*repository.WebhookSubscription, error)
}

func (m *mockSubscriptionStore) Get(ctx context.Context, id uuid.UUID) (*repository.WebhookSubscription, error) {
	if m.getSub != nil {
		return m.getSub(id)
	}
	if m.sub != nil {
		return m.sub, nil
	}
	return nil, repository.ErrWebhookSubscriptionNotFound
}

type mockDLQStore struct {
	inserted []struct {
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

func (m *mockDLQStore) InsertDLQ(
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
	subID := uuid.New()
	sub := &repository.WebhookSubscription{
		ID:          subID,
		WorkspaceID: wsID,
		URL:         "https://api.myweb.com/events",
		Secret:      []byte("signing-secret-key-123"),
		Active:      true,
	}

	t.Run("Normal dispatch signs and sends HTTP request", func(t *testing.T) {
		subStore := &mockSubscriptionStore{sub: sub}
		dlqStore := &mockDLQStore{}
		httpClient := &mockHTTPClient{}
		d := webhook.NewDefaultDispatcher(subStore, dlqStore, nil, httpClient, nil)

		event := map[string]string{
			"event":        "message.sent",
			"trace_id":     "trace-1",
			"message_id":   "msg-1",
			"workspace_id": wsID.String(),
		}
		rawBytes, _ := json.Marshal(event)

		task := webhook.WebhookDeliveryTask{
			ID:             uuid.New(),
			SubscriptionID: subID,
			WorkspaceID:    wsID,
			Event:          "message.sent",
			TraceID:        "trace-1",
			MessageID:      "msg-1",
			Payload:        rawBytes,
			Mode:           "outbound",
		}

		err := d.Dispatch(context.Background(), task)
		if err != nil {
			t.Fatalf("Dispatch failed: %v", err)
		}

		if len(httpClient.requests) != 1 {
			t.Fatalf("expected 1 HTTP request, got %d", len(httpClient.requests))
		}
		req := httpClient.requests[0]
		if req.URL.String() != sub.URL {
			t.Errorf("got URL %s, want %s", req.URL, sub.URL)
		}
		if req.Header.Get("X-Trace-ID") != "trace-1" {
			t.Errorf("got X-Trace-ID %q, want trace-1", req.Header.Get("X-Trace-ID"))
		}
		if req.Header.Get("X-PerGo-Signature") == "" {
			t.Errorf("expected signature header to be present")
		}
	})

	t.Run("Compliance checks redact PII on inbound events when workspace opted-out", func(t *testing.T) {
		subStore := &mockSubscriptionStore{sub: sub}
		dlqStore := &mockDLQStore{}
		workspaceStore := &mockWorkspaceStore{
			ws: &repository.Workspace{ID: wsID, PIIOptIn: false},
		}
		httpClient := &mockHTTPClient{}
		d := webhook.NewDefaultDispatcher(subStore, dlqStore, workspaceStore, httpClient, nil)

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

		task := webhook.WebhookDeliveryTask{
			ID:             uuid.New(),
			SubscriptionID: subID,
			WorkspaceID:    wsID,
			Event:          "message.received",
			TraceID:        "trace-2",
			MessageID:      "msg-2",
			Payload:        rawBytes,
			Mode:           "inbound",
		}

		err := d.Dispatch(context.Background(), task)
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
		subStore := &mockSubscriptionStore{sub: sub}
		dlqStore := &mockDLQStore{}
		httpClient := &mockHTTPClient{
			response: &http.Response{
				StatusCode: 502,
				Status:     "502 Bad Gateway",
				Body:       io.NopCloser(bytes.NewReader([]byte(""))),
			},
		}
		d := webhook.NewDefaultDispatcher(subStore, dlqStore, nil, httpClient, nil)

		event := map[string]string{
			"event":        "message.sent",
			"workspace_id": wsID.String(),
		}
		rawBytes, _ := json.Marshal(event)

		task := webhook.WebhookDeliveryTask{
			ID:             uuid.New(),
			SubscriptionID: subID,
			WorkspaceID:    wsID,
			Event:          "message.sent",
			Payload:        rawBytes,
			Mode:           "outbound",
		}

		err := d.Dispatch(context.Background(), task)
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
	subID := uuid.New()
	sub := &repository.WebhookSubscription{
		ID:          subID,
		WorkspaceID: wsID,
		URL:         "https://api.myweb.com/events",
		Secret:      []byte("signing-secret-key-123"),
		Active:      true,
	}

	subStore := &mockSubscriptionStore{sub: sub}
	dlqStore := &mockDLQStore{}
	d := webhook.NewDefaultDispatcher(subStore, dlqStore, nil, nil, nil)

	payload := []byte(`{"event":"test"}`)
	err := d.WriteToDLQ(context.Background(), wsID, subID, "trace-123", "msg-123", "message.sent", payload, 3, "something went wrong")
	if err != nil {
		t.Fatalf("WriteToDLQ failed: %v", err)
	}

	if len(dlqStore.inserted) != 1 {
		t.Fatalf("expected 1 DLQ insertion, got %d", len(dlqStore.inserted))
	}
	ins := dlqStore.inserted[0]
	if ins.workspaceID != wsID {
		t.Errorf("got workspace ID %s, want %s", ins.workspaceID, wsID)
	}
	if ins.subscriptionID != subID {
		t.Errorf("got subscription ID %s, want %s", ins.subscriptionID, subID)
	}
	if ins.url != sub.URL {
		t.Errorf("got url %s, want %s", ins.url, sub.URL)
	}
	if *ins.failureReason != "something went wrong" {
		t.Errorf("got failure reason %s", *ins.failureReason)
	}
}
