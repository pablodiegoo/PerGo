package queue

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/webhook"
)

func TestSignPayload(t *testing.T) {
	payload := []byte(`{"hello":"world"}`)
	secret := []byte("secret")
	timestamp := "1700000000"

	// Sign payload
	signatureHeader := webhook.SignPayload(payload, secret, timestamp)

	// Verify prefix matches expected format
	expectedPrefix := "t=1700000000,v1="
	if !strings.HasPrefix(signatureHeader, expectedPrefix) {
		t.Fatalf("expected signature header to start with %q, got %q", expectedPrefix, signatureHeader)
	}

	// Verify exact signature against manually computed value
	// Signed content: 1700000000.{"hello":"world"}
	expectedSigHex := "654f06c856baf080af3fa272934823257a542d35cf1f88099338f850a60601a4"
	expectedHeader := expectedPrefix + expectedSigHex
	if signatureHeader != expectedHeader {
		t.Errorf("SignPayload = %q, want %q", signatureHeader, expectedHeader)
	}
}
func TestWebhookWorker_Integration(t *testing.T) {
	nc := connectNATS(t)
	pool := getTestPool(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Delete stream to ensure clean test state
	js, err := jetstream.New(nc)
	if err == nil {
		_ = js.DeleteStream(ctx, "WEBHOOKS")
	}

	// 1. Setup repository
	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(i)
	}
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	dlqRepo := repository.NewWebhookDLQRepository(pool, enc)

	// 2. Setup workspaces
	wsRepo := repository.NewWorkspaceRepository(pool)
	ws, err := wsRepo.Create(ctx, "webhook_worker_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	// 3. Setup mock HTTP Webhook Target
	var receivedPayload []byte
	var receivedHeaders http.Header
	var mu sync.Mutex
	receivedCount := 0

	var serverURL string
	var serverHandler http.HandlerFunc = func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		receivedCount++
		receivedHeaders = r.Header
		var err error
		receivedPayload, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}

	testServer := httptest.NewServer(serverHandler)
	defer testServer.Close()
	serverURL = testServer.URL

	// 4. Save Webhook Config for Workspace
	webhookSecret := []byte("my-webhook-signing-secret")
	err = dlqRepo.SaveConfig(ctx, ws.ID, serverURL, webhookSecret)
	if err != nil {
		t.Fatalf("failed to save webhook config: %v", err)
	}

	// 5. Instantiate and Start WebhookWorker
	dispatcher := webhook.NewDefaultDispatcher(dlqRepo, wsRepo, nil)
	worker, err := NewWebhookWorker(ctx, nc, dispatcher)
	if err != nil {
		t.Fatalf("failed to start webhook worker: %v", err)
	}
	worker.SetWorkspaceRepository(wsRepo)
	defer worker.Stop()

	// 6. Publish outbound webhook event
	traceID := "trace-webhook-999"
	messageID := uuid.New().String()
	event := WebhookEvent{
		Event:       "sent",
		TraceID:     traceID,
		MessageID:   messageID,
		Channel:     "whatsapp",
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		WorkspaceID: ws.ID.String(),
	}

	eventData, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed to marshal event: %v", err)
	}

	publisher := NewJetStreamPublisher(nc)
	err = publisher.Publish(ctx, "webhooks.events", eventData, traceID)
	if err != nil {
		t.Fatalf("failed to publish webhook event: %v", err)
	}

	// 7. Wait for delivery to mock server
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		count := receivedCount
		mu.Unlock()
		if count > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	mu.Lock()
	count := receivedCount
	payloadBytes := receivedPayload
	headers := receivedHeaders
	mu.Unlock()

	if count == 0 {
		t.Fatal("webhook was not delivered to mock server within timeout")
	}

	// 8. Verify payload
	var receivedEvent WebhookEvent
	err = json.Unmarshal(payloadBytes, &receivedEvent)
	if err != nil {
		t.Fatalf("failed to unmarshal received event: %v", err)
	}
	if receivedEvent.TraceID != traceID || receivedEvent.Event != "sent" || receivedEvent.MessageID != messageID {
		t.Errorf("received event fields do not match: %+v", receivedEvent)
	}

	// 9. Verify Signature headers
	sigHeader := headers.Get("X-PerGo-Signature")
	if sigHeader == "" {
		t.Error("missing X-PerGo-Signature header in delivered webhook")
	} else {
		// Format: t=timestamp,v1=sig
		parts := strings.Split(sigHeader, ",")
		if len(parts) != 2 || !strings.HasPrefix(parts[0], "t=") || !strings.HasPrefix(parts[1], "v1=") {
			t.Errorf("invalid format for X-PerGo-Signature: %q", sigHeader)
		} else {
			ts := strings.TrimPrefix(parts[0], "t=")
			computedSig := webhook.SignPayload(payloadBytes, webhookSecret, ts)
			if sigHeader != computedSig {
				t.Errorf("received signature does not match computed: %q vs %q", sigHeader, computedSig)
			}
		}
	}
}

func TestWebhookWorker_TerminalErrorDLQ(t *testing.T) {
	nc := connectNATS(t)
	pool := getTestPool(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Delete stream to ensure clean test state
	js, err := jetstream.New(nc)
	if err == nil {
		_ = js.DeleteStream(ctx, "WEBHOOKS")
	}

	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(i)
	}
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	dlqRepo := repository.NewWebhookDLQRepository(pool, enc)

	wsRepo := repository.NewWorkspaceRepository(pool)
	ws, err := wsRepo.Create(ctx, "webhook_worker_ws_terminal_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	// Mock server that returns a terminal 404 Not Found error
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer testServer.Close()

	err = dlqRepo.SaveConfig(ctx, ws.ID, testServer.URL, []byte("sec"))
	if err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	dispatcher := webhook.NewDefaultDispatcher(dlqRepo, wsRepo, nil)
	worker, err := NewWebhookWorker(ctx, nc, dispatcher)
	if err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}
	worker.SetWorkspaceRepository(wsRepo)
	defer worker.Stop()

	traceID := "trace-terminal-888"
	messageID := uuid.New().String()
	event := WebhookEvent{
		Event:       "failed",
		TraceID:     traceID,
		MessageID:   messageID,
		Channel:     "whatsapp",
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		WorkspaceID: ws.ID.String(),
	}

	eventData, _ := json.Marshal(event)
	publisher := NewJetStreamPublisher(nc)
	err = publisher.Publish(ctx, "webhooks.events", eventData, traceID)
	if err != nil {
		t.Fatalf("failed to publish event: %v", err)
	}

	// Wait and check DLQ table
	deadline := time.Now().Add(5 * time.Second)
	var dlqItems []*repository.WebhookDLQ
	for time.Now().Before(deadline) {
		dlqItems, err = dlqRepo.ListDLQ(ctx, ws.ID, 10, 0)
		if err == nil && len(dlqItems) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if len(dlqItems) == 0 {
		t.Fatal("expected failed event to be written directly to the DLQ")
	}

	item := dlqItems[0]
	if item.TraceID != traceID || item.MessageID != messageID {
		t.Errorf("DLQ item fields mismatch: trace=%s, msg=%s", item.TraceID, item.MessageID)
	}
	if item.FailureReason == nil || !strings.Contains(*item.FailureReason, "404") {
		t.Errorf("expected fail reason to contain 404, got %v", item.FailureReason)
	}
}

func TestEnsureWebhookStream(t *testing.T) {
	nc := connectNATS(t)
	ctx := context.Background()

	stream, err := EnsureWebhookStream(ctx, nc)
	if err != nil {
		t.Fatalf("EnsureWebhookStream failed: %v", err)
	}

	info, err := stream.Info(ctx)
	if err != nil {
		t.Fatalf("stream.Info failed: %v", err)
	}

	if info.Config.Name != "WEBHOOKS" {
		t.Errorf("expected stream name WEBHOOKS, got %q", info.Config.Name)
	}
	if info.Config.Retention != jetstream.LimitsPolicy {
		t.Errorf("expected retention LimitsPolicy, got %v", info.Config.Retention)
	}
}

func TestWebhookWorker_Inbound(t *testing.T) {
	nc := connectNATS(t)
	pool := getTestPool(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Delete streams to ensure clean test state
	js, err := jetstream.New(nc)
	if err == nil {
		_ = js.DeleteStream(ctx, "INBOUND")
		_ = js.DeleteStream(ctx, "WEBHOOKS")
	}

	// 1. Setup repository
	kek := make([]byte, 32)
	enc, _ := crypto.NewEncryptor(kek)
	dlqRepo := repository.NewWebhookDLQRepository(pool, enc)
	wsRepo := repository.NewWorkspaceRepository(pool)

	// Create workspace with PII Opt-In false
	ws, err := wsRepo.Create(ctx, "inbound_redact_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	// 2. Setup mock webhook endpoint
	var receivedPayload []byte
	var mu sync.Mutex
	received := make(chan struct{}, 1)

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedPayload, _ = io.ReadAll(r.Body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		received <- struct{}{}
	}))
	defer testServer.Close()

	_ = dlqRepo.SaveConfig(ctx, ws.ID, testServer.URL, []byte("signing-secret"))

	dispatcher := webhook.NewDefaultDispatcher(dlqRepo, wsRepo, nil)
	worker, err := NewWebhookWorker(ctx, nc, dispatcher)
	if err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}
	worker.SetWorkspaceRepository(wsRepo)

	// 3. Publish inbound event with PII details (location & contacts)
	traceID := "trace-inbound-111"
	event := struct {
		Event       string `json:"event"`
		TraceID     string `json:"trace_id"`
		MessageID   string `json:"message_id"`
		Channel     string `json:"channel"`
		Timestamp   string `json:"timestamp"`
		WorkspaceID string `json:"workspace_id"`
		From        string `json:"from"`
		Body        string `json:"body,omitempty"`
		Location    any    `json:"location,omitempty"`
		Contacts    any    `json:"contacts,omitempty"`
	}{
		Event:       "inbound_message",
		TraceID:     traceID,
		MessageID:   "msg-999",
		Channel:     "whatsapp",
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		WorkspaceID: ws.ID.String(),
		From:        "5511999999999",
		Body:        "Hi",
		Location:    map[string]any{"latitude": -23.5},
		Contacts:    []any{map[string]any{"name": "Alice"}},
	}

	eventData, _ := json.Marshal(event)
	publisher := NewJetStreamPublisher(nc)
	err = publisher.Publish(ctx, "inbound.events."+ws.ID.String(), eventData, traceID)
	if err != nil {
		t.Fatalf("publish error: %v", err)
	}

	// 4. Wait for delivery
	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("webhook was not delivered")
	}

	mu.Lock()
	payload := receivedPayload
	mu.Unlock()

	// 5. Assert redaction (From hashed, Location & Contacts stripped)
	var result struct {
		From     string `json:"from"`
		Location any    `json:"location"`
		Contacts any    `json:"contacts"`
	}
	_ = json.Unmarshal(payload, &result)

	if result.From == "5511999999999" {
		t.Error("expected sender 'from' field to be hashed, but got plain value")
	}
	if len(result.From) != 64 {
		t.Errorf("expected 64-char hex hash value for from, got %q", result.From)
	}
	if result.Location != nil {
		t.Errorf("expected Location field to be stripped, got %v", result.Location)
	}
	if result.Contacts != nil {
		t.Errorf("expected Contacts field to be stripped, got %v", result.Contacts)
	}

	worker.Stop()
}
