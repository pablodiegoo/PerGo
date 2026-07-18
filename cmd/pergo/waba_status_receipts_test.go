package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/nats-io/nats.go"

	"github.com/pablojhp.pergo/internal/api/handler"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/platform/audit"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/queue"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestWABAStatusReceiptsEndToEnd(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("skipping: no PostgreSQL available")
	}

	ctx := context.Background()

	// Connect to NATS
	nc, err := nats.Connect(integrationNATSURL)
	if err != nil {
		t.Fatalf("failed to connect to NATS: %v", err)
	}
	defer nc.Close()

	// Ensure NATS streams
	_, err = queue.EnsureInboundStream(ctx, nc)
	if err != nil {
		t.Fatalf("failed to ensure INBOUND stream: %v", err)
	}
	_, err = queue.EnsureStream(ctx, nc)
	if err != nil {
		t.Fatalf("failed to ensure MESSAGES stream: %v", err)
	}

	wsRepo := repository.NewWorkspaceRepository(pool)
	kek := make([]byte, 32)
	enc, _ := crypto.NewEncryptor(kek)
	connRepo := repository.NewConnectionRepository(pool, enc)
	sessRepo := repository.NewRecipientSessionRepository(pool)
	dedupRepo := repository.NewInboundDedupRepository(pool)
	publisher := queue.NewJetStreamPublisher(nc)
	contactRepo := repository.NewContactRepository(pool)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)
	auditRepo := repository.NewAuditRepository(pool)

	// 1. Setup a Workspace
	ws, err := wsRepo.Create(ctx, "waba_status_integration_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	// 2. Setup a Connection (with WABA credentials)
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
		Credentials:    configBytes,
	}
	err = connRepo.Create(ctx, conn)
	if err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}

	// 3. Setup a Contact
	contact, err := contactRepo.ResolveContact(ctx, ws.ID, "whatsapp_cloud", "5511999990000", "Integration Tester", "", "")
	if err != nil {
		t.Fatalf("failed to resolve/create contact: %v", err)
	}

	// 4. Create a mock message dispatch in the database
	traceID := uuid.New().String()
	d, err := dispatchRepo.GetOrCreateDispatch(ctx, ws.ID, traceID, "whatsapp_cloud", nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to create dispatch: %v", err)
	}

	providerID := "wamid.test_status_123"
	err = dispatchRepo.UpdateProviderMessageID(ctx, d.ID, providerID)
	if err != nil {
		t.Fatalf("failed to update provider message id: %v", err)
	}

	// Insert outbound log with the same trace ID so ListThreadByContact lists it
	payloadOut := map[string]any{
		"request": map[string]any{
			"to":              "5511999990000",
			"channel":         "whatsapp_cloud",
			"sender_identity": "123456789",
			"body":            "End-To-End Status Test Outbound Message",
		},
		"status": "queued",
	}
	payloadBytesOut, _ := json.Marshal(payloadOut)
	_, err = pool.Exec(ctx, `
		INSERT INTO audit_logs (id, workspace_id, trace_id, event_type, payload, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, uuid.New(), ws.ID, traceID, "outbound_message", payloadBytesOut, time.Now().UTC())
	if err != nil {
		t.Fatalf("failed to insert outbound audit log: %v", err)
	}

	// 5. Setup handler
	auditWriter := audit.NewWriter(pool, 10, 1)
	defer auditWriter.Close()

	inboundProcessor := inbound.NewInboundProcessor(dedupRepo, wsRepo, nil, publisher, auditWriter, sessRepo, contactRepo, dispatchRepo, nil)
	h := handler.NewWABAWebhookHandler(connRepo, inboundProcessor, nil)

	e := echo.New()

	// 6. Simulate incoming status webhooks: delivered
	bodyDelivered := `{
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
								"display_phone_number": "123456789"
							},
							"statuses": [
								{
									"id": "wamid.test_status_123",
									"status": "delivered",
									"timestamp": "1700000000",
									"recipient_id": "5511999990000"
								}
							]
						}
					}
				]
			}
		]
	}`

	reqDelivered := httptest.NewRequest(http.MethodPost, "/webhooks/waba/"+ws.ID.String(), strings.NewReader(bodyDelivered))
	reqDelivered.Header.Set("Content-Type", "application/json")
	recDelivered := httptest.NewRecorder()
	cDelivered := e.NewContext(reqDelivered, recDelivered)
	cDelivered.SetPath("/webhooks/waba/:workspace_id")
	cDelivered.SetPathValues(echo.PathValues{
		{Name: "workspace_id", Value: ws.ID.String()},
	})

	err = h.HandlePost(cDelivered)
	if err != nil {
		t.Fatalf("HandlePost delivered failed: %v", err)
	}
	if recDelivered.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", recDelivered.Code, recDelivered.Body.String())
	}

	// Verify DB contains delivered
	dDelivered, err := dispatchRepo.GetByProviderMessageID(ctx, providerID)
	if err != nil {
		t.Fatalf("failed to retrieve dispatch: %v", err)
	}
	if dDelivered.Status != "delivered" {
		t.Errorf("expected status 'delivered', got %q", dDelivered.Status)
	}

	// 7. Simulate incoming status webhooks: read
	bodyRead := `{
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
								"display_phone_number": "123456789"
							},
							"statuses": [
								{
									"id": "wamid.test_status_123",
									"status": "read",
									"timestamp": "1700000001",
									"recipient_id": "5511999990000"
								}
							]
						}
					}
				]
			}
		]
	}`

	reqRead := httptest.NewRequest(http.MethodPost, "/webhooks/waba/"+ws.ID.String(), strings.NewReader(bodyRead))
	reqRead.Header.Set("Content-Type", "application/json")
	recRead := httptest.NewRecorder()
	cRead := e.NewContext(reqRead, recRead)
	cRead.SetPath("/webhooks/waba/:workspace_id")
	cRead.SetPathValues(echo.PathValues{
		{Name: "workspace_id", Value: ws.ID.String()},
	})

	err = h.HandlePost(cRead)
	if err != nil {
		t.Fatalf("HandlePost read failed: %v", err)
	}
	if recRead.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", recRead.Code, recRead.Body.String())
	}

	// Verify DB contains read
	dRead, err := dispatchRepo.GetByProviderMessageID(ctx, providerID)
	if err != nil {
		t.Fatalf("failed to retrieve dispatch: %v", err)
	}
	if dRead.Status != "read" {
		t.Errorf("expected status 'read', got %q", dRead.Status)
	}

	// 8. Call ListThreadByContact and verify dispatch status is returned
	thread, err := auditRepo.ListThreadByContact(ctx, ws.ID, contact.ID, nil)
	if err != nil {
		t.Fatalf("ListThreadByContact failed: %v", err)
	}
	if len(thread) != 1 {
		t.Fatalf("expected 1 message in thread, got %d", len(thread))
	}
	if thread[0].Status == nil {
		t.Errorf("expected Status not to be nil")
	} else if *thread[0].Status != "read" {
		t.Errorf("expected status 'read', got %q", *thread[0].Status)
	}
}
