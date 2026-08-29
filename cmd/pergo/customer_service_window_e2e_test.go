package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/handler"
	"github.com/pablojhp.pergo/internal/api/handler/admin"
	mw "github.com/pablojhp.pergo/internal/api/middleware"
	whatsapp "github.com/pablojhp.pergo/internal/channel/whatsapp"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/outbound"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/session"
)

type mockQueuePublisher struct {
	published [][]byte
}

func (p *mockQueuePublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	p.published = append(p.published, data)
	return nil
}

func TestCustomerServiceWindow_MultiChannel_E2E(t *testing.T) {
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
	tmplRepo := repository.NewWABATemplateRepository(pool)

	ws, err := wsRepo.Create(ctx, "window_e2e_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	wabaSenderIdentity := "+5511888880001"
	waWebSenderIdentity := "+5511888880002"
	telegramSenderIdentity := "@pergo_e2e_bot"
	contactPhone := "5511999991111"

	// Create WABA Connection
	wabaCreds, _ := json.Marshal(map[string]string{
		"token":        "waba_test_token",
		"verify_token": "waba_verify_123",
	})
	wabaConn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "WABA Connection",
		Channel:        "whatsapp_cloud",
		SenderIdentity: wabaSenderIdentity,
		Credentials:    wabaCreds,
		Status:         "connected",
		IsDefault:      true,
	}
	if err := connRepo.Create(ctx, wabaConn); err != nil {
		t.Fatalf("failed to create WABA connection: %v", err)
	}

	// Create WhatsApp Web Connection
	waWebConn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "WhatsApp Web Connection",
		Channel:        "whatsapp",
		SenderIdentity: waWebSenderIdentity,
		Status:         "connected",
		IsDefault:      true,
	}
	if err := connRepo.Create(ctx, waWebConn); err != nil {
		t.Fatalf("failed to create WhatsApp Web connection: %v", err)
	}

	// Create Telegram Connection
	tgCreds, _ := json.Marshal(map[string]string{
		"token": "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
	})
	tgConn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "Telegram Connection",
		Channel:        "telegram",
		SenderIdentity: telegramSenderIdentity,
		Credentials:    tgCreds,
		Status:         "connected",
		IsDefault:      true,
	}
	if err := connRepo.Create(ctx, tgConn); err != nil {
		t.Fatalf("failed to create Telegram connection: %v", err)
	}

	// Create an Approved WABA Template with 2 parameters in BODY
	tmpl := &repository.WABATemplate{
		WorkspaceID:  ws.ID,
		ConnectionID: wabaConn.ID,
		Name:         "welcome_update",
		Language:     "pt_BR",
		Category:     "UTILITY",
		Status:       "APPROVED",
		Components:   json.RawMessage(`[{"type":"BODY","text":"Olá {{1}}, seu pedido {{2}} está a caminho."}]`),
	}
	if _, err := tmplRepo.Upsert(ctx, tmpl); err != nil {
		t.Fatalf("failed to create template: %v", err)
	}

	publisher := &mockQueuePublisher{}
	inboundProc := inbound.NewInboundProcessor(
		dedupRepo,
		wsRepo,
		nil,
		publisher,
		nil,
		sessRepo,
		contactRepo,
		dispatchRepo,
		nil,
	)

	windowChecker := session.NewWindowChecker(sessRepo)
	outboundProc := outbound.NewProcessor(nil, nil, connRepo, publisher)
	outboundProc.SetWindowChecker(windowChecker)
	outboundProc.SetTemplateRepository(tmplRepo)

	msgHandler := &handler.MessageHandler{
		ConnectionRepo: connRepo,
		Publisher:      publisher,
		WindowChecker:  windowChecker,
		Ingestor:       outboundProc,
	}

	e := echo.New()
	e.Use(mw.TraceMiddleware())
	msgHandler.RegisterRoutes(e)

	// =========================================================================
	// Scenario 1: Initial state (No inbound session) -> WABA freeform rejected
	// =========================================================================
	t.Run("WABA freeform fails with 422 SESSION_WINDOW_EXPIRED when no session exists", func(t *testing.T) {
		body := `{"to":"` + contactPhone + `","channel":"whatsapp_cloud","body":"Initial freeform outreach"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(tenant.WithWorkspaceID(mw.WithContext(ctx, "trc-init-1"), ws.ID))
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp struct {
			Code    string            `json:"code"`
			Message string            `json:"message"`
			Details map[string]string `json:"details"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal error: %v", err)
		}
		if resp.Code != "SESSION_WINDOW_EXPIRED" {
			t.Errorf("code = %q, want 'SESSION_WINDOW_EXPIRED'", resp.Code)
		}
		if resp.Details["hint"] != "Use type: template to reach this contact" {
			t.Errorf("hint = %q", resp.Details["hint"])
		}
	})

	// =========================================================================
	// Scenario 2: Multi-channel channels (WhatsApp Web and Telegram) bypass window
	// =========================================================================
	t.Run("WhatsApp Web (whatsmeow) freeform succeeds without session window", func(t *testing.T) {
		body := `{"to":"` + contactPhone + `","channel":"whatsapp","body":"WhatsApp Web outreach"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(tenant.WithWorkspaceID(mw.WithContext(ctx, "trc-waweb-1"), ws.ID))
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202 for WhatsApp Web, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Telegram freeform succeeds without session window", func(t *testing.T) {
		body := `{"to":"123456789","channel":"telegram","body":"Telegram outreach"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(tenant.WithWorkspaceID(mw.WithContext(ctx, "trc-tg-1"), ws.ID))
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202 for Telegram, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// =========================================================================
	// Scenario 3: Inbound WABA message opens 24h Customer Service Window
	// =========================================================================
	t.Run("Inbound WABA event opens 24h window and allows freeform reply", func(t *testing.T) {
		webhookPayload := `{
			"object": "whatsapp_business_account",
			"entry": [{
				"id": "waba_acc_1",
				"changes": [{
					"field": "messages",
					"value": {
						"messaging_product": "whatsapp",
						"metadata": {
							"display_phone_number": "` + wabaSenderIdentity + `",
							"phone_number_id": "123456789"
						},
						"contacts": [{
							"profile": {"name": "E2E User"},
							"wa_id": "` + contactPhone + `"
						}],
						"messages": [{
							"from": "` + contactPhone + `",
							"id": "wamid.inbound_001",
							"timestamp": "` + fmt.Sprintf("%d", time.Now().Unix()) + `",
							"type": "text",
							"text": {"body": "Hello from customer"}
						}]
					}
				}]
			}]
		}`

		wabaAdapter := whatsapp.NewWABAInboundAdapter(nil)
		inboundEvents, err := wabaAdapter.Parse(ctx, []byte(webhookPayload), nil, wabaConn)
		if err != nil {
			t.Fatalf("failed to parse WABA webhook: %v", err)
		}
		if len(inboundEvents) != 1 {
			t.Fatalf("expected 1 inbound event, got %d", len(inboundEvents))
		}

		// Ensure connection's canonical sender identity was stamped
		if inboundEvents[0].To != wabaSenderIdentity {
			t.Fatalf("expected InboundEvent.To to be %s, got %s", wabaSenderIdentity, inboundEvents[0].To)
		}

		// Process inbound event
		if err := inboundProc.Process(ctx, inboundEvents[0]); err != nil {
			t.Fatalf("failed to process inbound event: %v", err)
		}

		// Verify WindowChecker confirms window is now open
		status, err := windowChecker.IsWindowOpen(ctx, domain.NewSessionKey(ws.ID, contactPhone, "whatsapp_cloud", wabaSenderIdentity), 0)
		if err != nil {
			t.Fatalf("failed to check window: %v", err)
		}
		if !status.Open {
			t.Fatalf("expected window to be open after inbound message")
		}
		if status.WindowDuration != 24*time.Hour {
			t.Errorf("expected 24h duration, got %v", status.WindowDuration)
		}

		// Dispatch freeform reply via API -> should return 202 Accepted
		body := `{"to":"` + contactPhone + `","channel":"whatsapp_cloud","body":"Freeform reply within 24h window"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(tenant.WithWorkspaceID(mw.WithContext(ctx, "trc-reply-1"), ws.ID))
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202 Accepted within 24h window, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// =========================================================================
	// Scenario 4: CTWA (Click-to-WhatsApp Ads) 72h window lifecycle
	// =========================================================================
	t.Run("CTWA entry point grants 72h window", func(t *testing.T) {
		ctwaPayload := `{
			"object": "whatsapp_business_account",
			"entry": [{
				"id": "waba_acc_1",
				"changes": [{
					"field": "messages",
					"value": {
						"messaging_product": "whatsapp",
						"metadata": {
							"display_phone_number": "` + wabaSenderIdentity + `",
							"phone_number_id": "123456789"
						},
						"contacts": [{
							"profile": {"name": "CTWA User"},
							"wa_id": "5511999992222"
						}],
						"messages": [{
							"from": "5511999992222",
							"id": "wamid.inbound_ctwa_002",
							"timestamp": "` + fmt.Sprintf("%d", time.Now().Unix()) + `",
							"type": "text",
							"text": {"body": "Saw your ad"},
							"referral": {
								"source_type": "ad",
								"source_id": "ad_12345"
							}
						}]
					}
				}]
			}]
		}`

		wabaAdapter := whatsapp.NewWABAInboundAdapter(nil)
		inboundEvents, err := wabaAdapter.Parse(ctx, []byte(ctwaPayload), nil, wabaConn)
		if err != nil {
			t.Fatalf("failed to parse CTWA webhook: %v", err)
		}
		if len(inboundEvents) != 1 {
			t.Fatalf("expected 1 inbound event, got %d", len(inboundEvents))
		}

		if err := inboundProc.Process(ctx, inboundEvents[0]); err != nil {
			t.Fatalf("failed to process CTWA event: %v", err)
		}

		// Update LastInboundAt to 48 hours ago to simulate time passage
		err = sessRepo.Upsert(ctx, domain.NewSessionKey(ws.ID, "5511999992222", "whatsapp_cloud", wabaSenderIdentity), time.Now().Add(-48*time.Hour), "ctwa")
		if err != nil {
			t.Fatalf("failed to update CTWA session: %v", err)
		}

		// Window should still be open at 48 hours because CTWA is 72h
		status, err := windowChecker.IsWindowOpen(ctx, domain.NewSessionKey(ws.ID, "5511999992222", "whatsapp_cloud", wabaSenderIdentity), 0)
		if err != nil {
			t.Fatalf("failed to check CTWA window: %v", err)
		}
		if !status.Open {
			t.Fatalf("expected CTWA window to be open at 48 hours")
		}
		if status.WindowDuration != 72*time.Hour {
			t.Errorf("expected 72h duration, got %v", status.WindowDuration)
		}

		// Outbound freeform message succeeds at 48h
		body := `{"to":"5511999992222","channel":"whatsapp_cloud","body":"Reply within 72h CTWA window"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(tenant.WithWorkspaceID(mw.WithContext(ctx, "trc-ctwa-reply"), ws.ID))
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202 Accepted for CTWA reply at 48h, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// =========================================================================
	// Scenario 5: Template Message Dispatched Outside Window
	// =========================================================================
	t.Run("Approved template message dispatches successfully outside 24h window", func(t *testing.T) {
		// Set session to expired
		err := sessRepo.Upsert(ctx, domain.NewSessionKey(ws.ID, contactPhone, "whatsapp_cloud", wabaSenderIdentity), time.Now().Add(-30*time.Hour), "standard")
		if err != nil {
			t.Fatalf("failed to expire session: %v", err)
		}

		body := `{
			"to": "` + contactPhone + `",
			"channel": "whatsapp_cloud",
			"template_name": "welcome_update",
			"language": "pt_BR",
			"components": [
				{
					"type": "body",
					"parameters": [
						{"type": "text", "text": "Carlos"},
						{"type": "text", "text": "Pedido #12345"}
					]
				}
			]
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(tenant.WithWorkspaceID(mw.WithContext(ctx, "trc-template-outbound"), ws.ID))
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202 Accepted for template message outside window, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// =========================================================================
	// Scenario 6: Admin Connection Test Execution
	// =========================================================================
	t.Run("Admin Connection Test execution with dynamic template parameters", func(t *testing.T) {
		deviceHandler := &admin.DeviceHandler{
			Connections:   connRepo,
			TemplatesRepo: tmplRepo,
			Publisher:     publisher,
		}

		adminEcho := echo.New()
		adminEcho.Use(mw.HTMXMiddleware())
		adminEcho.POST("/admin/devices/test", deviceHandler.RunTest)

		form := url.Values{}
		form.Set("connection_id", wabaConn.ID.String())
		form.Set("to", contactPhone)
		form.Set("is_template", "true")
		form.Set("template_name", "welcome_update")
		form.Set("language", "pt_BR")
		form.Set("param_1", "Alice")
		form.Set("param_2", "Doc #9988")

		postReq := httptest.NewRequest(http.MethodPost, "/admin/devices/test", strings.NewReader(form.Encode()))
		postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		postReq.AddCookie(&http.Cookie{Name: "pergo-active-workspace", Value: ws.ID.String()})
		postRec := httptest.NewRecorder()

		adminEcho.ServeHTTP(postRec, postReq)

		if postRec.Code != http.StatusOK {
			t.Fatalf("expected 200 for test execution, got %d: %s", postRec.Code, postRec.Body.String())
		}
		if !strings.Contains(postRec.Body.String(), "Mensagem enviada para a fila de saída") {
			t.Errorf("expected success notification in response, got: %s", postRec.Body.String())
		}

		// Verify published test message has dynamic template parameters
		if len(publisher.published) == 0 {
			t.Fatalf("expected message to be published to queue")
		}
		var qMsg domain.QueueMessage
		lastPublished := publisher.published[len(publisher.published)-1]
		if err := json.Unmarshal(lastPublished, &qMsg); err != nil {
			t.Fatalf("failed to unmarshal published test message: %v", err)
		}
		if qMsg.TemplateName != "welcome_update" {
			t.Errorf("expected TemplateName 'welcome_update', got %q", qMsg.TemplateName)
		}
		if qMsg.Language != "pt_BR" {
			t.Errorf("expected Language 'pt_BR', got %q", qMsg.Language)
		}
		if len(qMsg.Components) != 1 {
			t.Fatalf("expected 1 component, got %d", len(qMsg.Components))
		}
		params, err := outbound.NormalizeTemplateParams(qMsg.Components[0].Parameters)
		if err != nil {
			t.Fatalf("failed to normalize params: %v", err)
		}
		if len(params) != 2 || params[0].Text != "Alice" || params[1].Text != "Doc #9988" {
			t.Errorf("unexpected params: %+v", params)
		}
	})
}
