package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/pablojhp.pergo/internal/platform/audit"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/queue"
	"github.com/pablojhp.pergo/internal/platform/storage"
	"github.com/pablojhp.pergo/internal/repository"
)

func connectNATS(t *testing.T) *nats.Conn {
	t.Helper()
	nc, err := nats.Connect(nats.DefaultURL, nats.Timeout(2*time.Second))
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
	}
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     "INBOUND",
		Subjects: []string{"inbound.events.>"},
	})
	if err != nil {
		t.Fatalf("failed to create NATS stream: %v", err)
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
		Credentials:    configBytes,
	}
	_ = connRepo.Create(ctx, conn)

	auditWriter := audit.NewWriter(pool, 100, 1)
	defer auditWriter.Close()

	h := NewWABAWebhookHandler(wsRepo, connRepo, sessRepo, dedupRepo, s3Client, publisher, auditWriter)

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
}
