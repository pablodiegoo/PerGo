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
		_ = js.DeleteStream(ctx, "WEBHOOK_DELIVERIES")
		_ = js.DeleteStream(ctx, "INBOUND")
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

	// 4. Create Webhook Subscription for Workspace
	webhookSecret := []byte("my-webhook-signing-secret")
	subRepo := repository.NewWebhookSubscriptionRepository(pool, enc)
	_, err = subRepo.Create(ctx, ws.ID, serverURL, []string{"*"}, webhookSecret)
	if err != nil {
		t.Fatalf("failed to create webhook subscription: %v", err)
	}

	// 5. Instantiate and Start WebhookWorker
	dispatcher := webhook.NewDefaultDispatcher(subRepo, dlqRepo, wsRepo, testServer.Client(), nil)
	worker, err := NewWebhookWorker(ctx, nc, dispatcher, subRepo)
	if err != nil {
		t.Fatalf("failed to start webhook worker: %v", err)
	}
	worker.SetWorkspaceRepository(wsRepo)
	defer worker.Stop()

	// 6. Publish outbound webhook event
	traceID := "trace-webhook-" + uuid.New().String()
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
	deadline := time.Now().Add(15 * time.Second)
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
		_ = js.DeleteStream(ctx, "WEBHOOK_DELIVERIES")
		_ = js.DeleteStream(ctx, "INBOUND")
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

	subRepo := repository.NewWebhookSubscriptionRepository(pool, enc)
	_, err = subRepo.Create(ctx, ws.ID, testServer.URL, []string{"*"}, []byte("sec"))
	if err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	dispatcher := webhook.NewDefaultDispatcher(subRepo, dlqRepo, wsRepo, testServer.Client(), nil)
	worker, err := NewWebhookWorker(ctx, nc, dispatcher, subRepo)
	if err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}
	worker.SetWorkspaceRepository(wsRepo)
	defer worker.Stop()

	traceID := "trace-terminal-" + uuid.New().String()
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
	deadline := time.Now().Add(15 * time.Second)
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
		t.Errorf("stream name = %q, want WEBHOOKS", info.Config.Name)
	}
}

func TestWebhookWorker_Inbound(t *testing.T) {
	nc := connectNATS(t)
	pool := getTestPool(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Delete stream to ensure clean test state
	js, err := jetstream.New(nc)
	if err == nil {
		_ = js.DeleteStream(ctx, "WEBHOOKS")
		_ = js.DeleteStream(ctx, "WEBHOOK_DELIVERIES")
		_ = js.DeleteStream(ctx, "INBOUND")
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
	ws, err := wsRepo.Create(ctx, "webhook_worker_ws_inbound_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	// Mock server that asserts delivery
	var receivedPayload []byte
	var mu sync.Mutex
	delivered := false

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		delivered = true
		receivedPayload, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer testServer.Close()

	// 1. Create Webhook Subscription
	subRepo := repository.NewWebhookSubscriptionRepository(pool, enc)
	_, err = subRepo.Create(ctx, ws.ID, testServer.URL, []string{"*"}, []byte("secret-inbound"))
	if err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	// 2. Start WebhookWorker
	dispatcher := webhook.NewDefaultDispatcher(subRepo, dlqRepo, wsRepo, testServer.Client(), nil)
	worker, err := NewWebhookWorker(ctx, nc, dispatcher, subRepo)
	if err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}
	worker.SetWorkspaceRepository(wsRepo)
	defer worker.Stop()

	// 3. Publish inbound event with PII details (location & contacts)
	traceID := "trace-inbound-" + uuid.New().String()
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
		Body:        "Secret location ping",
		Location: map[string]float64{
			"latitude":  -23.5505,
			"longitude": -46.6333,
		},
		Contacts: []string{"John Doe"},
	}

	eventData, _ := json.Marshal(event)
	publisher := NewJetStreamPublisher(nc)
	err = publisher.Publish(ctx, "inbound.events.messages", eventData, traceID)
	if err != nil {
		t.Fatalf("failed to publish inbound event: %v", err)
	}

	// 4. Wait for delivery
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		done := delivered
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	mu.Lock()
	done := delivered
	payload := receivedPayload
	mu.Unlock()

	if !done {
		t.Fatal("webhook was not delivered")
	}

	// 5. Verify payload was sanitized: From is SHA256 hashed, Location/Contacts are stripped
	var res map[string]any
	_ = json.Unmarshal(payload, &res)

	if res["location"] != nil {
		t.Errorf("expected location to be stripped, got %v", res["location"])
	}
	if res["contacts"] != nil {
		t.Errorf("expected contacts to be stripped, got %v", res["contacts"])
	}

	// Verify SHA-256 hash of "5511999999999"
	expectedFromHash := "a869177964cc68954ffec997bbad30769f8a5a6fdc60f296ddbc60b9347dc416"
	if res["from"] != expectedFromHash {
		t.Errorf("from = %v, want hashed %s", res["from"], expectedFromHash)
	}
}

func TestWebhookWorker_ConnectionStatusDelivery(t *testing.T) {
	nc := connectNATS(t)
	pool := getTestPool(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Delete stream to ensure clean test state
	js, err := jetstream.New(nc)
	if err == nil {
		_ = js.DeleteStream(ctx, "WEBHOOKS")
		_ = js.DeleteStream(ctx, "WEBHOOK_DELIVERIES")
		_ = js.DeleteStream(ctx, "INBOUND")
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
	ws, err := wsRepo.Create(ctx, "webhook_conn_status_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	// Mock server that asserts delivery
	var receivedPayload []byte
	var receivedHeaders http.Header
	var mu sync.Mutex
	delivered := false

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		delivered = true
		receivedPayload, _ = io.ReadAll(r.Body)
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer testServer.Close()

	// 1. Create Webhook Subscription matching "connection.status"
	subRepo := repository.NewWebhookSubscriptionRepository(pool, enc)
	_, err = subRepo.Create(ctx, ws.ID, testServer.URL, []string{"connection.status"}, []byte("secret-conn"))
	if err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	// 4. Start WebhookWorker
	dispatcher := webhook.NewDefaultDispatcher(subRepo, dlqRepo, wsRepo, testServer.Client(), nil)
	worker, err := NewWebhookWorker(ctx, nc, dispatcher, subRepo)
	if err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}
	worker.SetWorkspaceRepository(wsRepo)
	defer worker.Stop()

	// 5. Publish connection.status event to webhooks.events
	traceID := "trace-conn-status-" + uuid.New().String()
	connID := uuid.New()
	event := struct {
		Event          string `json:"event"`
		TraceID        string `json:"trace_id"`
		WorkspaceID    string `json:"workspace_id"`
		ConnectionID   string `json:"connection_id"`
		Channel        string `json:"channel"`
		SenderIdentity string `json:"sender_identity"`
		Status         string `json:"status"`
		Timestamp      string `json:"timestamp"`
	}{
		Event:          "connection.status",
		TraceID:        traceID,
		WorkspaceID:    ws.ID.String(),
		ConnectionID:   connID.String(),
		Channel:        "whatsapp",
		SenderIdentity: "+5511999998888",
		Status:         "connected",
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
	}

	eventData, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed to marshal event: %v", err)
	}

	publisher := NewJetStreamPublisher(nc)
	err = publisher.Publish(ctx, "webhooks.events", eventData, traceID)
	if err != nil {
		t.Fatalf("publish error: %v", err)
	}

	// 6. Wait for delivery
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		done := delivered
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	mu.Lock()
	done := delivered
	payload := receivedPayload
	headers := receivedHeaders
	mu.Unlock()

	if !done {
		t.Fatal("connection.status webhook was not delivered")
	}

	// 7. Verify delivered payload structure and signature
	if headers.Get("X-PerGo-Signature") == "" {
		t.Error("expected X-PerGo-Signature header on delivered webhook")
	}
	if headers.Get("X-Trace-ID") != traceID {
		t.Errorf("expected X-Trace-ID %q, got %q", traceID, headers.Get("X-Trace-ID"))
	}

	var deliveredEvent struct {
		Event          string `json:"event"`
		TraceID        string `json:"trace_id"`
		WorkspaceID    string `json:"workspace_id"`
		ConnectionID   string `json:"connection_id"`
		Channel        string `json:"channel"`
		SenderIdentity string `json:"sender_identity"`
		Status         string `json:"status"`
		Timestamp      string `json:"timestamp"`
	}
	if err := json.Unmarshal(payload, &deliveredEvent); err != nil {
		t.Fatalf("failed to unmarshal delivered payload: %v", err)
	}

	if deliveredEvent.Event != "connection.status" {
		t.Errorf("expected event 'connection.status', got %q", deliveredEvent.Event)
	}
	if deliveredEvent.ConnectionID != connID.String() {
		t.Errorf("expected connection_id %q, got %q", connID.String(), deliveredEvent.ConnectionID)
	}
	if deliveredEvent.Channel != "whatsapp" {
		t.Errorf("expected channel 'whatsapp', got %q", deliveredEvent.Channel)
	}
	if deliveredEvent.SenderIdentity != "+5511999998888" {
		t.Errorf("expected sender_identity '+5511999998888', got %q", deliveredEvent.SenderIdentity)
	}
	if deliveredEvent.Status != "connected" {
		t.Errorf("expected status 'connected', got %q", deliveredEvent.Status)
	}
}
