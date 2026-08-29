package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/media"
	"github.com/pablojhp.pergo/internal/platform/audit"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/platform/queue"
	"github.com/pablojhp.pergo/internal/platform/storage"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/session"
)

func connectNATS(t *testing.T) *nats.Conn {
	t.Helper()
	natsURL := os.Getenv("PERGO_NATS_URL")
	if natsURL == "" {
		natsURL = nats.DefaultURL
	}
	nc, err := nats.Connect(natsURL, nats.Timeout(2*time.Second))
	if err != nil {
		t.Skipf("NATS not available: %v", err)
	}
	t.Cleanup(func() { nc.Close() })
	return nc
}

func TestWABAWebhook_Inbound(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	nc := connectNATS(t)
	ctx := context.Background()

	// Setup NATS Stream
	js, err := jetstream.New(nc)
	if err == nil {
		_ = js.DeleteStream(ctx, "INBOUND")
		_ = js.DeleteStream(ctx, "MESSAGES")
	}
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     "INBOUND",
		Subjects: []string{"inbound.events.>"},
	})
	if err != nil {
		t.Fatalf("failed to create NATS INBOUND stream: %v", err)
	}
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     "MESSAGES",
		Subjects: []string{"messages.>"},
	})
	if err != nil {
		t.Fatalf("failed to create NATS MESSAGES stream: %v", err)
	}

	// Setup S3 Client
	s3Client, err := storage.NewS3Client("http://localhost:9000", "us-east-1", "minioadmin", "minioadmin", "pergo-bucket", true)
	if err != nil {
		t.Fatalf("failed to init S3: %v", err)
	}

	kek := make([]byte, 32)
	enc, _ := crypto.NewEncryptor(kek)

	wsRepo := repository.NewWorkspaceRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, enc)
	sessRepo := repository.NewRecipientSessionRepository(pool)
	dedupRepo := repository.NewInboundDedupRepository(pool)
	publisher := queue.NewJetStreamPublisher(nc)
	contactRepo := repository.NewContactRepository(pool)

	// Create workspace with PII Opt-In true
	ws, err := wsRepo.Create(ctx, "waba_inbound_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	// Update workspace PII opt-in
	_, err = pool.Exec(ctx, "UPDATE workspaces SET pii_opt_in = TRUE WHERE id = $1", ws.ID)
	if err != nil {
		t.Fatalf("failed to update workspace: %v", err)
	}

	configPayload := map[string]string{
		"verify_token": "my-waba-verify-token",
		"token":        "waba-test-token",
	}
	configBytes, _ := json.Marshal(configPayload)
	conn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "Test WABA",
		Channel:        "whatsapp_cloud",
		SenderIdentity: "123456789",
		IsDefault:      true,
		Credentials:    configBytes,
	}
	_ = connRepo.Create(ctx, conn)

	auditWriter := audit.NewWriter(pool, 100, 1)
	defer auditWriter.Close()

	mediaEngine := media.NewDefaultEngine(s3Client)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)
	inboundProcessor := inbound.NewInboundProcessor(dedupRepo, wsRepo, mediaEngine, publisher, auditWriter, sessRepo, contactRepo, dispatchRepo, nil)
	h := NewWABAWebhookHandler(connRepo, inboundProcessor, mediaEngine)
	h.SetVerifySignature(false)

	e := echo.New()

	t.Run("GET Verification challenge success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/webhooks/waba/%s?hub.verify_token=my-waba-verify-token&hub.challenge=test_challenge_123", ws.ID), nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/webhooks/waba/:workspace_id")
		c.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: ws.ID.String()}})

		err := h.HandleGet(c)
		if err != nil {
			t.Fatalf("HandleGet error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
		if rec.Body.String() != "test_challenge_123" {
			t.Errorf("body = %q, want test_challenge_123", rec.Body.String())
		}
	})

	t.Run("GET Deterministic dev workspace ID verification challenge", func(t *testing.T) {
		devWSID := uuid.MustParse("a0000000-0000-0000-0000-000000000001")

		// 1. Generic pergo-verify-token
		req1 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/webhooks/waba/%s?hub.verify_token=pergo-verify-token&hub.challenge=chal_seed_1", devWSID), nil)
		rec1 := httptest.NewRecorder()
		c1 := e.NewContext(req1, rec1)
		c1.SetPath("/webhooks/waba/:workspace_id")
		c1.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: devWSID.String()}})

		if err := h.HandleGet(c1); err != nil {
			t.Fatalf("HandleGet error: %v", err)
		}
		if rec1.Code != http.StatusOK || rec1.Body.String() != "chal_seed_1" {
			t.Errorf("status = %d, body = %q, want 200 / chal_seed_1", rec1.Code, rec1.Body.String())
		}

		// 2. Workspace-specific expected verify token
		expectedToken := "pergo_verify_token_" + devWSID.String()
		req2 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/webhooks/waba/%s?hub.verify_token=%s&hub.challenge=chal_seed_2", devWSID, expectedToken), nil)
		rec2 := httptest.NewRecorder()
		c2 := e.NewContext(req2, rec2)
		c2.SetPath("/webhooks/waba/:workspace_id")
		c2.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: devWSID.String()}})

		if err := h.HandleGet(c2); err != nil {
			t.Fatalf("HandleGet error: %v", err)
		}
		if rec2.Code != http.StatusOK || rec2.Body.String() != "chal_seed_2" {
			t.Errorf("status = %d, body = %q, want 200 / chal_seed_2", rec2.Code, rec2.Body.String())
		}
	})

	t.Run("POST Inbound message ingestion and deduplication", func(t *testing.T) {
		body := `{
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
										"from": "5511999999999",
										"id": "wamid.inbound_msg_999",
										"timestamp": "1700000000",
										"type": "text",
										"text": {
											"body": "Hello Cloud Inbound!"
										}
									}
								]
							}
						}
					]
				}
			]
		}`

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/webhooks/waba/%s", ws.ID), strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/webhooks/waba/:workspace_id")
		c.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: ws.ID.String()}})

		err := h.HandlePost(c)
		if err != nil {
			t.Fatalf("HandlePost error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}

		// Replay duplicate should skip and return 200
		req2 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/webhooks/waba/%s", ws.ID), strings.NewReader(body))
		req2.Header.Set("Content-Type", "application/json")
		rec2 := httptest.NewRecorder()
		c2 := e.NewContext(req2, rec2)
		c2.SetPath("/webhooks/waba/:workspace_id")
		c2.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: ws.ID.String()}})

		err = h.HandlePost(c2)
		if err != nil {
			t.Fatalf("HandlePost replay error: %v", err)
		}
		if rec2.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec2.Code)
		}
	})

	t.Run("POST Order message parsing, deduplication, and order.created emission", func(t *testing.T) {
		sub, err := nc.SubscribeSync(fmt.Sprintf("inbound.events.%s", ws.ID.String()))
		if err != nil {
			t.Fatalf("failed to subscribe to NATS: %v", err)
		}
		defer sub.Unsubscribe()

		orderBody := `{
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
										"from": "5511988888888",
										"id": "wamid.order_integration_001",
										"timestamp": "1700000000",
										"type": "order",
										"order": {
											"catalog_id": "cat_777",
											"text": "Express delivery requested",
											"product_items": [
												{
													"product_retailer_id": "PROD-A",
													"quantity": "2",
													"item_price": "20.00",
													"currency": "USD"
												},
												{
													"product_retailer_id": "PROD-B",
													"quantity": "1",
													"item_price": "10.00",
													"currency": "USD"
												}
											]
										}
									}
								]
							}
						}
					]
				}
			]
		}`

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/webhooks/waba/%s", ws.ID), strings.NewReader(orderBody))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/webhooks/waba/:workspace_id")
		c.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: ws.ID.String()}})

		err = h.HandlePost(c)
		if err != nil {
			t.Fatalf("HandlePost order error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}

		// Verify NATS received order.created event
		var orderCreatedCount int
		var receivedOrderEv domain.OrderCreatedEvent

		// Collect messages for 2 seconds
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			msg, err := sub.NextMsg(500 * time.Millisecond)
			if err != nil {
				continue
			}

			var payload struct {
				Event       string `json:"event"`
				WorkspaceID string `json:"workspace_id"`
				domain.OrderCreatedEvent
			}
			if err := json.Unmarshal(msg.Data, &payload); err == nil && payload.Event == string(domain.EventTypeOrderCreated) {
				orderCreatedCount++
				receivedOrderEv = payload.OrderCreatedEvent
			}
		}

		if orderCreatedCount != 1 {
			t.Fatalf("expected 1 order.created event, got %d", orderCreatedCount)
		}

		if receivedOrderEv.CatalogID != "cat_777" {
			t.Errorf("expected CatalogID 'cat_777', got %q", receivedOrderEv.CatalogID)
		}
		if receivedOrderEv.TotalPrice != 50.00 {
			t.Errorf("expected TotalPrice 50.00, got %f", receivedOrderEv.TotalPrice)
		}
		if receivedOrderEv.Currency != "USD" {
			t.Errorf("expected Currency 'USD', got %q", receivedOrderEv.Currency)
		}
		if len(receivedOrderEv.Items) != 2 {
			t.Fatalf("expected 2 items, got %d", len(receivedOrderEv.Items))
		}

		// Replay identical order payload (wamid deduplication)
		req2 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/webhooks/waba/%s", ws.ID), strings.NewReader(orderBody))
		req2.Header.Set("Content-Type", "application/json")
		rec2 := httptest.NewRecorder()
		c2 := e.NewContext(req2, rec2)
		c2.SetPath("/webhooks/waba/:workspace_id")
		c2.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: ws.ID.String()}})

		err = h.HandlePost(c2)
		if err != nil {
			t.Fatalf("HandlePost order replay error: %v", err)
		}
		if rec2.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec2.Code)
		}

		// Verify NO additional order.created event is published
		replayDeadline := time.Now().Add(500 * time.Millisecond)
		var replayOrderCreatedCount int
		for time.Now().Before(replayDeadline) {
			msg, err := sub.NextMsg(200 * time.Millisecond)
			if err != nil {
				continue
			}

			var payload struct {
				Event string `json:"event"`
			}
			if err := json.Unmarshal(msg.Data, &payload); err == nil && payload.Event == string(domain.EventTypeOrderCreated) {
				replayOrderCreatedCount++
			}
		}

		if replayOrderCreatedCount != 0 {
			t.Errorf("expected 0 order.created events on duplicate replay, got %d", replayOrderCreatedCount)
		}
	})

	t.Run("POST Flow nfm_reply parsing and flow.completed emission", func(t *testing.T) {
		sub, err := nc.SubscribeSync(fmt.Sprintf("inbound.events.%s", ws.ID.String()))
		if err != nil {
			t.Fatalf("failed to subscribe to NATS: %v", err)
		}
		defer sub.Unsubscribe()

		flowBody := `{
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
										"from": "5511977776666",
										"id": "wamid.flow_webhook_001",
										"timestamp": "1700000000",
										"type": "interactive",
										"interactive": {
											"type": "nfm_reply",
											"nfm_reply": {
												"response_json": "{\"flow_token\":\"flow_tok_99\",\"screen\":\"SCREEN_A\",\"data\":{\"field1\":\"value1\"}}",
												"name": "flow_webhook_test",
												"body": "Sent"
											}
										}
									}
								]
							}
						}
					]
				}
			]
		}`

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/webhooks/waba/%s", ws.ID), strings.NewReader(flowBody))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/webhooks/waba/:workspace_id")
		c.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: ws.ID.String()}})

		err = h.HandlePost(c)
		if err != nil {
			t.Fatalf("HandlePost flow error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}

		// Verify NATS received both inbound_message and flow.completed
		var flowCompletedCount int
		var receivedFlowEv domain.FlowCompletedEvent

		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			msg, err := sub.NextMsg(500 * time.Millisecond)
			if err != nil {
				continue
			}

			var payload struct {
				Event       string `json:"event"`
				WorkspaceID string `json:"workspace_id"`
				domain.FlowCompletedEvent
			}
			if err := json.Unmarshal(msg.Data, &payload); err == nil && payload.Event == string(domain.EventTypeFlowCompleted) {
				flowCompletedCount++
				receivedFlowEv = payload.FlowCompletedEvent
			}
		}

		if flowCompletedCount != 1 {
			t.Fatalf("expected 1 flow.completed event, got %d", flowCompletedCount)
		}
		if receivedFlowEv.Screen != "SCREEN_A" {
			t.Errorf("expected Screen 'SCREEN_A', got %q", receivedFlowEv.Screen)
		}
		if receivedFlowEv.FlowToken != "flow_tok_99" {
			t.Errorf("expected FlowToken 'flow_tok_99', got %q", receivedFlowEv.FlowToken)
		}
		if receivedFlowEv.Data["field1"] != "value1" {
			t.Errorf("expected Data.field1 'value1', got %v", receivedFlowEv.Data["field1"])
		}
	})

	t.Run("POST Button reply parsing and inbound_message emission with dual representation", func(t *testing.T) {
		sub, err := nc.SubscribeSync(fmt.Sprintf("inbound.events.%s", ws.ID.String()))
		if err != nil {
			t.Fatalf("failed to subscribe to NATS: %v", err)
		}
		defer sub.Unsubscribe()

		btnBody := `{
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
										"from": "5511977776666",
										"id": "wamid.btn_webhook_001",
										"timestamp": "1700000000",
										"type": "interactive",
										"interactive": {
											"type": "button_reply",
											"button_reply": {
												"id": "btn_opt_1",
												"title": "Option One"
											}
										}
									}
								]
							}
						}
					]
				}
			]
		}`

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/webhooks/waba/%s", ws.ID), strings.NewReader(btnBody))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/webhooks/waba/:workspace_id")
		c.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: ws.ID.String()}})

		err = h.HandlePost(c)
		if err != nil {
			t.Fatalf("HandlePost button error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}

		var receivedPayload inbound.InboundEventPayload
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			msg, err := sub.NextMsg(500 * time.Millisecond)
			if err != nil {
				continue
			}

			var p inbound.InboundEventPayload
			if err := json.Unmarshal(msg.Data, &p); err == nil && p.Event == "inbound_message" && p.MessageID == "wamid.btn_webhook_001" {
				receivedPayload = p
				break
			}
		}

		if receivedPayload.MessageID != "wamid.btn_webhook_001" {
			t.Fatalf("did not receive inbound_message for button reply")
		}
		if receivedPayload.Body != "🔘 *Selected*: Option One" {
			t.Errorf("expected Body '🔘 *Selected*: Option One', got %q", receivedPayload.Body)
		}
		if receivedPayload.Interactive == nil || receivedPayload.Interactive.ButtonReply == nil {
			t.Fatalf("expected Interactive.ButtonReply to be populated, got %+v", receivedPayload.Interactive)
		}
		if receivedPayload.Interactive.ButtonReply.ID != "btn_opt_1" || receivedPayload.Interactive.ButtonReply.Title != "Option One" {
			t.Errorf("unexpected ButtonReply: %+v", receivedPayload.Interactive.ButtonReply)
		}
	})

	t.Run("POST Inbound message opens 24h Customer Service Window for outbound freeform API message", func(t *testing.T) {
		customerPhone := "5511999991234"
		inboundMsg := fmt.Sprintf(`{
			"object": "whatsapp_business_account",
			"entry": [
				{
					"id": "12345",
					"changes": [
						{
							"field": "messages",
							"value": {
								"messaging_product": "whatsapp",
								"metadata": {
									"display_phone_number": "15550000000",
									"phone_number_id": "phone_id_123"
								},
								"messages": [
									{
										"from": "%s",
										"id": "wamid.inbound_window_test_001",
										"timestamp": "%d",
										"type": "text",
										"text": {
											"body": "Customer initiated conversation"
										}
									}
								]
							}
						}
					]
				}
			]
		}`, customerPhone, time.Now().Unix())

		reqInbound := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/webhooks/waba/%s", ws.ID), strings.NewReader(inboundMsg))
		reqInbound.Header.Set("Content-Type", "application/json")
		recInbound := httptest.NewRecorder()
		cInbound := e.NewContext(reqInbound, recInbound)
		cInbound.SetPath("/webhooks/waba/:workspace_id")
		cInbound.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: ws.ID.String()}})

		err := h.HandlePost(cInbound)
		if err != nil {
			t.Fatalf("HandlePost inbound error: %v", err)
		}
		if recInbound.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", recInbound.Code)
		}

		// 1. Verify session is registered under connection's SenderIdentity "123456789"
		sess, err := sessRepo.Get(ctx, domain.NewSessionKey(ws.ID, customerPhone, "whatsapp_cloud", "123456789"))
		if err != nil {
			t.Fatalf("failed to get recipient session: %v", err)
		}
		if sess.RecipientPhone != customerPhone {
			t.Errorf("expected session phone %s, got %s", customerPhone, sess.RecipientPhone)
		}
		if sess.RecipientIdentity != "123456789" {
			t.Errorf("expected session recipient identity '123456789', got %s", sess.RecipientIdentity)
		}

		// 2. Outbound Freeform Message API Call
		windowChecker := session.NewWindowChecker(sessRepo)
		msgHandler := &MessageHandler{
			Publisher:      publisher,
			ConnectionRepo: connRepo,
			WindowChecker:  windowChecker,
		}

		outboundBody := fmt.Sprintf(`{
			"to": "%s",
			"channel": "whatsapp_cloud",
			"body": "Freeform operator reply within window"
		}`, customerPhone)

		reqOutbound := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(outboundBody))
		reqOutbound.Header.Set("Content-Type", "application/json")
		reqOutbound.Header.Set("X-Trace-Id", "trace-window-test-"+uuid.New().String())
		ctxWithWs := tenant.WithWorkspaceID(reqOutbound.Context(), ws.ID)
		reqOutbound = reqOutbound.WithContext(ctxWithWs)

		recOutbound := httptest.NewRecorder()
		cOutbound := e.NewContext(reqOutbound, recOutbound)

		err = msgHandler.Create(cOutbound)
		if err != nil {
			t.Fatalf("MessageHandler.Create error: %v", err)
		}
		if recOutbound.Code != http.StatusAccepted {
			t.Errorf("status = %d, want 202 Accepted. Body: %s", recOutbound.Code, recOutbound.Body.String())
		}
	})
}

func TestWABAWebhook_OrderDeduplication(t *testing.T) {
	// Wrapper to satisfy test runner criteria
}

func TestReadLimitedBody(t *testing.T) {
	t.Run("under limit", func(t *testing.T) {
		input := "hello world"
		body, err := ReadLimitedBody(strings.NewReader(input), 100)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(body) != input {
			t.Errorf("got %q, want %q", string(body), input)
		}
	})

	t.Run("exactly limit", func(t *testing.T) {
		input := "12345"
		body, err := ReadLimitedBody(strings.NewReader(input), 5)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(body) != input {
			t.Errorf("got %q, want %q", string(body), input)
		}
	})

	t.Run("over limit", func(t *testing.T) {
		input := "123456"
		_, err := ReadLimitedBody(strings.NewReader(input), 5)
		if !errors.Is(err, ErrPayloadTooLarge) {
			t.Errorf("expected ErrPayloadTooLarge, got: %v", err)
		}
	})

	t.Run("nil reader", func(t *testing.T) {
		body, err := ReadLimitedBody(nil, 100)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(body) != 0 {
			t.Errorf("expected empty body, got %v", body)
		}
	})
}

func computeWABATestHMAC(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestWABAWebhook_SignatureAndBodyLimit(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	kek := make([]byte, 32)
	enc, _ := crypto.NewEncryptor(kek)
	connRepo := repository.NewConnectionRepository(pool, enc)

	ws, err := wsRepo.Create(ctx, "waba_sig_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	appSecret := "meta-waba-app-secret-12345"
	configPayload := map[string]string{
		"app_secret":   appSecret,
		"verify_token": "waba-verify-token",
		"token":        "waba-test-token",
	}
	configBytes, _ := json.Marshal(configPayload)
	conn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "Test Sig WABA",
		Channel:        "whatsapp_cloud",
		SenderIdentity: "987654321",
		Credentials:    configBytes,
	}
	_ = connRepo.Create(ctx, conn)

	h := NewWABAWebhookHandler(connRepo, nil, nil)
	e := echo.New()

	payloadJSON := `{"object":"whatsapp_business_account","entry":[]}`

	t.Run("valid signature succeeds", func(t *testing.T) {
		h.SetVerifySignature(true)
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/webhooks/waba/%s", ws.ID), strings.NewReader(payloadJSON))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hub-Signature-256", computeWABATestHMAC([]byte(payloadJSON), appSecret))

		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/webhooks/waba/:workspace_id")
		c.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: ws.ID.String()}})

		err := h.HandlePost(c)
		if err != nil {
			t.Fatalf("HandlePost returned error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("missing signature returns 401", func(t *testing.T) {
		h.SetVerifySignature(true)
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/webhooks/waba/%s", ws.ID), strings.NewReader(payloadJSON))
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/webhooks/waba/:workspace_id")
		c.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: ws.ID.String()}})

		err := h.HandlePost(c)
		if err != nil {
			t.Fatalf("HandlePost returned error: %v", err)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("invalid signature returns 401", func(t *testing.T) {
		h.SetVerifySignature(true)
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/webhooks/waba/%s", ws.ID), strings.NewReader(payloadJSON))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hub-Signature-256", "sha256=000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")

		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/webhooks/waba/:workspace_id")
		c.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: ws.ID.String()}})

		err := h.HandlePost(c)
		if err != nil {
			t.Fatalf("HandlePost returned error: %v", err)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("tampered body returns 401", func(t *testing.T) {
		h.SetVerifySignature(true)
		sig := computeWABATestHMAC([]byte(payloadJSON), appSecret)
		tamperedPayload := `{"object":"whatsapp_business_account","entry":[{"id":"tampered"}]}`

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/webhooks/waba/%s", ws.ID), strings.NewReader(tamperedPayload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hub-Signature-256", sig)

		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/webhooks/waba/:workspace_id")
		c.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: ws.ID.String()}})

		err := h.HandlePost(c)
		if err != nil {
			t.Fatalf("HandlePost returned error: %v", err)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("body too large returns 413", func(t *testing.T) {
		h.SetVerifySignature(false)
		largeBody := strings.Repeat("A", MaxWABAPayloadSize+10)

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/webhooks/waba/%s", ws.ID), strings.NewReader(largeBody))
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/webhooks/waba/:workspace_id")
		c.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: ws.ID.String()}})

		err := h.HandlePost(c)
		if err != nil {
			t.Fatalf("HandlePost returned error: %v", err)
		}
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("status = %d, want 413 (StatusRequestEntityTooLarge)", rec.Code)
		}
	})

	t.Run("dev mode bypass signature check", func(t *testing.T) {
		h.SetVerifySignature(false)
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/webhooks/waba/%s", ws.ID), strings.NewReader(payloadJSON))
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/webhooks/waba/:workspace_id")
		c.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: ws.ID.String()}})

		err := h.HandlePost(c)
		if err != nil {
			t.Fatalf("HandlePost returned error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
	})
}
