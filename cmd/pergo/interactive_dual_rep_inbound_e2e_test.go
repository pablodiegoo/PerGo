package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/handler"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/integration/chatwoot"
	"github.com/pablojhp.pergo/internal/integration/typebot"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/repository"
)

type recordedNATSEvent struct {
	Subject string
	Data    []byte
}

type spyNATSPublisher struct {
	mu     sync.Mutex
	events []recordedNATSEvent
}

func (s *spyNATSPublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, recordedNATSEvent{
		Subject: subject,
		Data:    data,
	})
	return nil
}

func (s *spyNATSPublisher) getEvents() []recordedNATSEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	copied := make([]recordedNATSEvent, len(s.events))
	copy(copied, s.events)
	return copied
}

func (s *spyNATSPublisher) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = nil
}

func waitForCondition(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

func TestInteractiveDualRepresentation_Inbound_E2E(t *testing.T) {
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
	integrationRepo := repository.NewIntegrationRepository(pool, enc)
	mappingRepo := repository.NewChatwootMappingRepository(pool)
	typebotSessionRepo := repository.NewTypebotSessionRepository(pool)

	ws, err := wsRepo.Create(ctx, "interactive_dual_rep_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	// Generate RSA Keypair for Flow Decryption test
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	privKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privKey),
	})

	wabaSenderIdentity := "+5511888889999"
	wabaCreds, _ := json.Marshal(map[string]string{
		"token":           "waba_token_123",
		"verify_token":    "waba_verify_123",
		"private_key":     string(privKeyPEM),
		"phone_number_id": "waba_phone_id_123",
	})
	wabaConn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "WABA Interactive Connection",
		Channel:        "whatsapp_cloud",
		SenderIdentity: wabaSenderIdentity,
		Credentials:    wabaCreds,
		Status:         "connected",
		IsDefault:      true,
	}
	if err := connRepo.Create(ctx, wabaConn); err != nil {
		t.Fatalf("failed to create WABA connection: %v", err)
	}

	// 1. Setup Mock Chatwoot Server
	var cwMu sync.Mutex
	var chatwootReceivedMessages []string
	chatwootServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/contacts/search"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"payload": []}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/contacts"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"payload": {"contact": {"id": 1001}}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/conversations"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id": 2001}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/messages"):
			var body struct {
				Content string `json:"content"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			cwMu.Lock()
			chatwootReceivedMessages = append(chatwootReceivedMessages, body.Content)
			cwMu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id": 3001}`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer chatwootServer.Close()

	cwCfg, _ := json.Marshal(map[string]interface{}{
		"api_url":      chatwootServer.URL,
		"access_token": "cw_token",
		"inbox_id":     1,
		"account_id":   1,
	})
	_ = integrationRepo.Save(ctx, &repository.Integration{
		ID:          uuid.New(),
		WorkspaceID: ws.ID,
		Name:        "Chatwoot",
		Provider:    "chatwoot",
		Active:      true,
		Config:      cwCfg,
	})

	// 2. Setup Mock Typebot Server
	var tbMu sync.Mutex
	var typebotReceivedMessages []string
	var typebotReceivedMeta []map[string]any
	typebotServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body typebot.StartChatRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		tbMu.Lock()
		typebotReceivedMessages = append(typebotReceivedMessages, body.Message)
		if meta, ok := body.PrefilledVariables["pergo_metadata"].(map[string]any); ok {
			typebotReceivedMeta = append(typebotReceivedMeta, meta)
		}
		tbMu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"sessionId": "tb-sess-e2e", "messages": []}`))
	}))
	defer typebotServer.Close()

	tbCfg, _ := json.Marshal(typebot.Config{
		APIURL: typebotServer.URL,
		Bots: []typebot.BotConfig{
			{
				ConnectionID: wabaConn.ID.String(),
				BotID:        "bot_main",
				PublicToken:  "tok_main",
				IsDefault:    true,
			},
		},
	})
	_ = integrationRepo.Save(ctx, &repository.Integration{
		ID:          uuid.New(),
		WorkspaceID: ws.ID,
		Name:        "Typebot",
		Provider:    "typebot",
		Active:      true,
		Config:      tbCfg,
	})

	natsSpy := &spyNATSPublisher{}
	cwSyncer := chatwoot.NewChatwootSyncer(integrationRepo, mappingRepo, chatwootServer.Client())
	tbForwarder := typebot.NewForwarder(typebotSessionRepo, integrationRepo, natsSpy)
	inboundRouter := inbound.NewDefaultRouter(cwSyncer, tbForwarder)

	inboundProc := inbound.NewInboundProcessor(
		dedupRepo,
		wsRepo,
		nil,
		natsSpy,
		nil,
		sessRepo,
		contactRepo,
		dispatchRepo,
		inboundRouter,
	)

	wabaHandler := handler.NewWABAWebhookHandler(connRepo, inboundProc, nil)
	wabaHandler.SetVerifySignature(false)

	e := echo.New()

	t.Run("button_reply delivers markdown to Chatwoot, clean title to Typebot, and dual rep to NATS", func(t *testing.T) {
		cwMu.Lock()
		chatwootReceivedMessages = nil
		cwMu.Unlock()

		tbMu.Lock()
		typebotReceivedMessages = nil
		typebotReceivedMeta = nil
		tbMu.Unlock()

		natsSpy.clear()

		btnPhone := "5511999990001"
		btnContact, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp_cloud", btnPhone, "Btn User", "", btnPhone)
		if err != nil {
			t.Fatalf("failed to resolve contact: %v", err)
		}
		_ = contactRepo.UpdateBotState(ctx, ws.ID, btnContact.ID, true, nil)

		btnPayload := fmt.Sprintf(`{
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
										"id": "wamid.btn_e2e_001",
										"timestamp": "1700000000",
										"type": "interactive",
										"interactive": {
											"type": "button_reply",
											"button_reply": {
												"id": "btn_meeting_confirm",
												"title": "Confirm Meeting"
											}
										}
									}
								]
							}
						}
					]
				}
			]
		}`, btnPhone)

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/webhooks/waba/%s", ws.ID), strings.NewReader(btnPayload))
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

		// 1. Verify Chatwoot got formatted Markdown
		expectedCW := "🔘 *Selected*: Confirm Meeting"
		ok := waitForCondition(2*time.Second, func() bool {
			cwMu.Lock()
			defer cwMu.Unlock()
			return len(chatwootReceivedMessages) > 0 && chatwootReceivedMessages[0] == expectedCW
		})
		if !ok {
			cwMu.Lock()
			t.Errorf("Chatwoot did not receive expected message %q within timeout. Got: %+v", expectedCW, chatwootReceivedMessages)
			cwMu.Unlock()
		}

		// 2. Verify Typebot got clean title and metadata
		ok = waitForCondition(2*time.Second, func() bool {
			tbMu.Lock()
			defer tbMu.Unlock()
			return len(typebotReceivedMessages) > 0 && typebotReceivedMessages[0] == "Confirm Meeting"
		})
		if !ok {
			tbMu.Lock()
			t.Errorf("Typebot did not receive clean message within timeout. Got: %+v", typebotReceivedMessages)
			tbMu.Unlock()
		}
		tbMu.Lock()
		if len(typebotReceivedMeta) == 0 || typebotReceivedMeta[0]["button_id"] != "btn_meeting_confirm" {
			t.Errorf("expected Typebot pergo_metadata button_id, got %+v", typebotReceivedMeta)
		}
		tbMu.Unlock()

		// 3. Verify NATS received inbound_message with Interactive.ButtonReply
		var foundNATSInbound bool
		for _, ev := range natsSpy.getEvents() {
			var p inbound.InboundEventPayload
			if err := json.Unmarshal(ev.Data, &p); err == nil && p.Event == "inbound_message" {
				foundNATSInbound = true
				if p.Body != expectedCW {
					t.Errorf("expected NATS Body %q, got %q", expectedCW, p.Body)
				}
				if p.Interactive == nil || p.Interactive.ButtonReply == nil {
					t.Fatalf("expected NATS Interactive.ButtonReply, got %+v", p.Interactive)
				}
				if p.Interactive.ButtonReply.ID != "btn_meeting_confirm" {
					t.Errorf("expected ButtonReply.ID 'btn_meeting_confirm', got %q", p.Interactive.ButtonReply.ID)
				}
			}
		}
		if !foundNATSInbound {
			t.Errorf("expected NATS inbound_message event")
		}
	})

	t.Run("encrypted nfm_reply delivers decrypted markdown to Chatwoot and flow.completed to NATS", func(t *testing.T) {
		cwMu.Lock()
		chatwootReceivedMessages = nil
		cwMu.Unlock()

		natsSpy.clear()

		flowPhone := "5511999990002"
		flowContact, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp_cloud", flowPhone, "Flow User", "", flowPhone)
		if err != nil {
			t.Fatalf("failed to resolve contact: %v", err)
		}
		_ = contactRepo.UpdateBotState(ctx, ws.ID, flowContact.ID, true, nil)

		// Encrypt response data using RSA + AES-128-GCM
		aesKey := make([]byte, 16)
		for i := range aesKey {
			aesKey[i] = byte(i + 42)
		}
		iv := make([]byte, 12)
		for i := range iv {
			iv[i] = byte(i + 7)
		}

		flowPlaintext := `{"screen":"RATING_SCREEN","data":{"score":10,"review":"Excellent Service"}}`
		ciphertext, tag, err := crypto.EncryptAES128GCM(aesKey, iv, []byte(flowPlaintext))
		if err != nil {
			t.Fatalf("AES encryption failed: %v", err)
		}
		fullCipher := append(ciphertext, tag...)

		encAESKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, &privKey.PublicKey, aesKey, nil)
		if err != nil {
			t.Fatalf("RSA encryption failed: %v", err)
		}

		flowJSON, _ := json.Marshal(map[string]string{
			"flow_token":          "tok_rating_123",
			"encrypted_flow_data": base64.StdEncoding.EncodeToString(fullCipher),
			"encrypted_aes_key":   base64.StdEncoding.EncodeToString(encAESKey),
			"initial_vector":      base64.StdEncoding.EncodeToString(iv),
		})

		flowPayload := fmt.Sprintf(`{
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
										"id": "wamid.flow_e2e_001",
										"timestamp": "1700000000",
										"type": "interactive",
										"interactive": {
											"type": "nfm_reply",
											"nfm_reply": {
												"name": "rating_flow",
												"body": "Sent",
												"response_json": %q
											}
										}
									}
								]
							}
						}
					]
				}
			]
		}`, flowPhone, string(flowJSON))

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/webhooks/waba/%s", ws.ID), strings.NewReader(flowPayload))
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

		// 1. Verify Chatwoot received decrypted markdown summary
		expectedCW := "📄 *Form Submitted*\nScreen: RATING_SCREEN\n- review: Excellent Service\n- score: 10"
		ok := waitForCondition(2*time.Second, func() bool {
			cwMu.Lock()
			defer cwMu.Unlock()
			return len(chatwootReceivedMessages) > 0 && chatwootReceivedMessages[0] == expectedCW
		})
		if !ok {
			cwMu.Lock()
			t.Errorf("Chatwoot did not receive expected decrypted message %q within timeout. Got: %+v", expectedCW, chatwootReceivedMessages)
			cwMu.Unlock()
		}

		// 2. Verify NATS received flow.completed domain event
		var foundFlowCompleted bool
		for _, ev := range natsSpy.getEvents() {
			var p struct {
				Event     string                 `json:"event"`
				Screen    string                 `json:"screen"`
				FlowToken string                 `json:"flow_token"`
				ContactID string                 `json:"contact_id"`
				Data      map[string]interface{} `json:"data"`
			}
			if err := json.Unmarshal(ev.Data, &p); err == nil && p.Event == string(domain.EventTypeFlowCompleted) {
				foundFlowCompleted = true
				if p.Screen != "RATING_SCREEN" {
					t.Errorf("expected Screen 'RATING_SCREEN', got %q", p.Screen)
				}
				if p.FlowToken != "tok_rating_123" {
					t.Errorf("expected FlowToken 'tok_rating_123', got %q", p.FlowToken)
				}
				if p.Data["review"] != "Excellent Service" {
					t.Errorf("expected Data.review 'Excellent Service', got %v", p.Data["review"])
				}
			}
		}
		if !foundFlowCompleted {
			t.Errorf("expected NATS flow.completed event")
		}
	})
}
