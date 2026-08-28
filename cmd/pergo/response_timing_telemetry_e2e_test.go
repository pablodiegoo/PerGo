package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/handler"
	"github.com/pablojhp.pergo/internal/channel"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/queue"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/webhook"
)

type recordedWebhookDelivery struct {
	SignatureHeader string
	TraceIDHeader   string
	Body            []byte
	Payload         inbound.InboundEventPayload
}

type fakeTimingDispatcher struct {
	lastTo string
}

func (f *fakeTimingDispatcher) Dispatch(ctx context.Context, p *channel.MessagePayload) (string, error) {
	f.lastTo = p.To
	return "wamid.outbound_sim_123", nil
}

type fakeTimingDispatchMsg struct {
	acked bool
}

func (m *fakeTimingDispatchMsg) Data() []byte                       { return nil }
func (m *fakeTimingDispatchMsg) Headers() map[string]string         { return nil }
func (m *fakeTimingDispatchMsg) Ack() error                         { m.acked = true; return nil }
func (m *fakeTimingDispatchMsg) NakWithDelay(d time.Duration) error { return nil }

func TestResponseTimingTelemetry_E2E(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("skipping: PostgreSQL not available")
	}

	ctx := context.Background()
	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(i + 1)
	}
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	wsRepo := repository.NewWorkspaceRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, enc)
	sessRepo := repository.NewRecipientSessionRepository(pool)
	dedupRepo := repository.NewInboundDedupRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)
	subRepo := repository.NewWebhookSubscriptionRepository(pool, enc)
	dlqRepo := repository.NewWebhookDLQRepository(pool, enc)

	ws, err := wsRepo.Create(ctx, "timing_e2e_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	// 1. Setup Mock Webhook Endpoint for Subscriber (e.g. Ecoar AI / External CPaaS Consumer)
	var deliveriesMu sync.Mutex
	var deliveries []recordedWebhookDelivery

	webhookSecret := "sec_live_contract_timing_test_secret_32_bytes"
	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "cannot read body", http.StatusBadRequest)
			return
		}

		var payload inbound.InboundEventPayload
		_ = json.Unmarshal(body, &payload)

		deliveriesMu.Lock()
		deliveries = append(deliveries, recordedWebhookDelivery{
			SignatureHeader: r.Header.Get("X-PerGo-Signature"),
			TraceIDHeader:   r.Header.Get("X-Trace-ID"),
			Body:            body,
			Payload:         payload,
		})
		deliveriesMu.Unlock()

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer webhookServer.Close()

	// 2. Create active webhook subscription for workspace
	sub, err := subRepo.Create(ctx, ws.ID, webhookServer.URL, []string{"*"}, []byte(webhookSecret))
	if err != nil {
		t.Fatalf("failed to create webhook subscription: %v", err)
	}

	// 3. Setup Dispatch Orchestrator for Outbound Sending
	dispWA := &fakeTimingDispatcher{}
	channelRegistry := channel.NewRegistry(map[string]channel.Dispatcher{
		"whatsapp_cloud": dispWA,
	})

	orchestrator := queue.NewDispatchOrchestrator(channelRegistry, dispatchRepo, nil, nil, nil, contactRepo, 5, 60*time.Second)
	orchestrator.SetContactRepository(contactRepo)
	orchestrator.SetRecipientSessionRepository(sessRepo)

	// 4. Setup Webhook Dispatcher
	dispatcher := webhook.NewDefaultDispatcher(subRepo, dlqRepo, wsRepo, webhookServer.Client(), nil)

	// 5. Setup Inbound Processor wired to webhook dispatcher via inline publisher
	directPublisher := &directWebhookPublisher{
		subRepo:    subRepo,
		dispatcher: dispatcher,
	}

	inboundProc := inbound.NewInboundProcessor(
		dedupRepo,
		wsRepo,
		nil,
		directPublisher,
		nil,
		sessRepo,
		contactRepo,
		dispatchRepo,
		nil,
	)

	wabaCreds, _ := json.Marshal(map[string]string{
		"token":           "waba_token_123",
		"verify_token":    "waba_verify_123",
		"phone_number_id": "waba_phone_id_123",
	})
	wabaConn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "WABA Timing Connection",
		Channel:        "whatsapp_cloud",
		SenderIdentity: "+5511888887777",
		Credentials:    wabaCreds,
		Status:         "connected",
		IsDefault:      true,
	}
	if err := connRepo.Create(ctx, wabaConn); err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}

	wabaHandler := handler.NewWABAWebhookHandler(connRepo, inboundProc, nil)
	wabaHandler.SetVerifySignature(false)
	e := echo.New()

	t.Run("Full Turn Loop: Outbound message records timing, Inbound response exposes timing.response_latency_ms in signed webhook", func(t *testing.T) {
		deliveriesMu.Lock()
		deliveries = nil
		deliveriesMu.Unlock()

		contactPhone := "5511999995555"
		senderPhone := "+5511888887777"

		// Step A: Send outbound message to contact
		outboundTraceID := "outbound_turn_trace_" + uuid.New().String()
		qMsg := &domain.QueueMessage{
			WorkspaceID:    ws.ID,
			TraceID:        outboundTraceID,
			To:             contactPhone,
			Channel:        "whatsapp_cloud",
			SenderIdentity: senderPhone,
			Body:           "Olá! Você consome produtos orgânicos regularmente?",
		}

		msg := &fakeTimingDispatchMsg{}
		err := orchestrator.Process(ctx, msg, qMsg, 0)
		if err != nil {
			t.Fatalf("outbound dispatch failed: %v", err)
		}

		// Verify session record has last_outbound_at
		sess, err := sessRepo.Get(ctx, ws.ID, contactPhone, "whatsapp_cloud", senderPhone)
		if err != nil {
			t.Fatalf("failed to get recipient session: %v", err)
		}
		if sess.LastOutboundAt == nil {
			t.Fatal("expected LastOutboundAt on recipient session")
		}

		// Adjust LastOutboundAt in database to simulate a 3200ms thinking/reading time
		simulatedOutboundAt := time.Now().UTC().Add(-3200 * time.Millisecond)
		err = sessRepo.RecordOutbound(ctx, ws.ID, contactPhone, "whatsapp_cloud", senderPhone, simulatedOutboundAt)
		if err != nil {
			t.Fatalf("failed to update simulated outbound time: %v", err)
		}

		// Step B: Inbound response arrives from contact
		inboundPayload := fmt.Sprintf(`{
			"object": "whatsapp_business_account",
			"entry": [
				{
					"id": "12345",
					"changes": [
						{
							"field": "messages",
							"value": {
								"messaging_product": "whatsapp",
								"messages": [
									{
										"from": %q,
										"id": "wamid.inbound_timing_001",
										"timestamp": "1700000000",
										"type": "text",
										"text": {
											"body": "Sim, costumo comprar semanalmente na feira do bairro."
										}
									}
								]
							}
						}
					]
				}
			]
		}`, contactPhone)

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/webhooks/waba/%s", ws.ID), strings.NewReader(inboundPayload))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/webhooks/waba/:workspace_id")
		c.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: ws.ID.String()}})

		err = wabaHandler.HandlePost(c)
		if err != nil {
			t.Fatalf("HandlePost error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}

		// Step C: Verify delivery to Subscriber HTTP webhook server
		deliveriesMu.Lock()
		numDeliveries := len(deliveries)
		var lastDelivery recordedWebhookDelivery
		if numDeliveries > 0 {
			lastDelivery = deliveries[numDeliveries-1]
		}
		deliveriesMu.Unlock()

		if numDeliveries == 0 {
			t.Fatal("expected webhook delivery to subscriber endpoint, got 0")
		}

		// 1. Verify HMAC-SHA256 signature
		if lastDelivery.SignatureHeader == "" {
			t.Fatal("expected X-PerGo-Signature header on webhook request")
		}
		if !webhook.VerifyPerGoSignature(lastDelivery.Body, lastDelivery.SignatureHeader, webhookSecret) {
			t.Errorf("X-PerGo-Signature verification failed for delivered body: %s", string(lastDelivery.Body))
		}

		// 2. Verify Timing telemetry in payload
		if lastDelivery.Payload.Timing == nil {
			t.Fatalf("expected Timing object in delivered payload, got nil. Payload: %s", string(lastDelivery.Body))
		}
		if lastDelivery.Payload.Timing.ResponseLatencyMS == nil {
			t.Fatalf("expected ResponseLatencyMS in Timing object, got nil")
		}

		latency := *lastDelivery.Payload.Timing.ResponseLatencyMS
		if latency < 3000 || latency > 5000 {
			t.Errorf("expected response latency ~3200ms, got %d ms", latency)
		}

		if lastDelivery.Payload.Timing.LastOutboundAt == "" {
			t.Error("expected LastOutboundAt to be populated")
		}

		// 3. Verify JSON structure contains "timing": {"response_latency_ms": ...}
		var rawDelivered map[string]any
		if err := json.Unmarshal(lastDelivery.Body, &rawDelivered); err != nil {
			t.Fatalf("failed to unmarshal raw delivered body: %v", err)
		}
		rawTiming, ok := rawDelivered["timing"].(map[string]any)
		if !ok || rawTiming == nil {
			t.Fatalf("expected 'timing' field in JSON, got %v", rawDelivered["timing"])
		}
		if int64(rawTiming["response_latency_ms"].(float64)) != latency {
			t.Errorf("mismatch in response_latency_ms in JSON: got %v, want %d", rawTiming["response_latency_ms"], latency)
		}
	})

	t.Run("Initial inbound message without prior outbound omits timing from signed webhook", func(t *testing.T) {
		deliveriesMu.Lock()
		deliveries = nil
		deliveriesMu.Unlock()

		freshPhone := "5511999997777"

		inboundPayload := fmt.Sprintf(`{
			"object": "whatsapp_business_account",
			"entry": [
				{
					"id": "12345",
					"changes": [
						{
							"field": "messages",
							"value": {
								"messaging_product": "whatsapp",
								"messages": [
									{
										"from": %q,
										"id": "wamid.inbound_initial_001",
										"timestamp": "1700000000",
										"type": "text",
										"text": {
											"body": "Gostaria de saber mais sobre os estudos."
										}
									}
								]
							}
						}
					]
				}
			]
		}`, freshPhone)

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/webhooks/waba/%s", ws.ID), strings.NewReader(inboundPayload))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/webhooks/waba/:workspace_id")
		c.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: ws.ID.String()}})

		err = wabaHandler.HandlePost(c)
		if err != nil {
			t.Fatalf("HandlePost error: %v", err)
		}

		deliveriesMu.Lock()
		numDeliveries := len(deliveries)
		var lastDelivery recordedWebhookDelivery
		if numDeliveries > 0 {
			lastDelivery = deliveries[numDeliveries-1]
		}
		deliveriesMu.Unlock()

		if numDeliveries == 0 {
			t.Fatal("expected webhook delivery to subscriber endpoint")
		}

		// Verify signature is valid
		if !webhook.VerifyPerGoSignature(lastDelivery.Body, lastDelivery.SignatureHeader, webhookSecret) {
			t.Errorf("X-PerGo-Signature verification failed")
		}

		// Verify timing is omitted
		if lastDelivery.Payload.Timing != nil {
			t.Errorf("expected Timing to be nil, got %+v", lastDelivery.Payload.Timing)
		}

		var rawDelivered map[string]any
		if err := json.Unmarshal(lastDelivery.Body, &rawDelivered); err != nil {
			t.Fatalf("failed to unmarshal raw delivered body: %v", err)
		}
		if _, exists := rawDelivered["timing"]; exists {
			t.Errorf("expected 'timing' field to be completely omitted from JSON, got %v", rawDelivered["timing"])
		}
	})

	_ = sub
}

// directWebhookPublisher bridges InboundProcessor NATS publishing directly to WebhookDispatcher in test.
type directWebhookPublisher struct {
	subRepo    *repository.WebhookSubscriptionRepository
	dispatcher webhook.WebhookDispatcher
}

func (p *directWebhookPublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	if strings.HasPrefix(subject, "inbound.events.") {
		wsIDStr := strings.TrimPrefix(subject, "inbound.events.")
		wsID, err := uuid.Parse(wsIDStr)
		if err != nil {
			return err
		}

		subs, err := p.subRepo.ListByWorkspace(ctx, wsID)
		if err != nil {
			return err
		}

		var pld struct {
			Event     string `json:"event"`
			MessageID string `json:"message_id"`
		}
		_ = json.Unmarshal(data, &pld)

		for _, sub := range subs {
			if !sub.Active {
				continue
			}
			task := webhook.WebhookDeliveryTask{
				ID:             uuid.New(),
				SubscriptionID: sub.ID,
				WorkspaceID:    wsID,
				Event:          pld.Event,
				TraceID:        traceID,
				MessageID:      pld.MessageID,
				Payload:        data,
				Mode:           "inbound",
			}
			_ = p.dispatcher.Dispatch(ctx, task)
		}
	}
	return nil
}
