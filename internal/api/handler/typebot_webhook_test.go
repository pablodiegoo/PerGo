package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/handler"
	"github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestTypebotWebhook_Auth(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	apiKeyRepo := repository.NewAPIKeyRepository(pool)
	pub := &fakePublisher{}

	ws, err := wsRepo.Create(ctx, "typebot_webhook_auth_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	_, rawKey, err := apiKeyRepo.Create(ctx, ws.ID, "Test Key")
	if err != nil {
		t.Fatalf("failed to create API Key: %v", err)
	}

	h := handler.NewTypebotWebhookHandler(pool, pub)

	t.Run("MissingToken_Returns401", func(t *testing.T) {
		e := echo.New()
		e.Use(middleware.AuthMiddleware(apiKeyRepo))
		e.POST("/api/integrations/typebot", h.Handle)

		req := httptest.NewRequest(http.MethodPost, "/api/integrations/typebot", nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("InvalidToken_Returns401", func(t *testing.T) {
		e := echo.New()
		e.Use(middleware.AuthMiddleware(apiKeyRepo))
		e.POST("/api/integrations/typebot", h.Handle)

		req := httptest.NewRequest(http.MethodPost, "/api/integrations/typebot?token=invalid-key", nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("ValidToken_InvalidPayload_Returns400", func(t *testing.T) {
		e := echo.New()
		e.Use(middleware.AuthMiddleware(apiKeyRepo))
		e.POST("/api/integrations/typebot", h.Handle)

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/integrations/typebot?token=%s", rawKey), strings.NewReader("invalid-json"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})
}

func TestTypebotWebhookHandler_HappyPath(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	apiKeyRepo := repository.NewAPIKeyRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	pub := &fakePublisher{}

	ws, err := wsRepo.Create(ctx, "typebot_webhook_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	_, rawKey, err := apiKeyRepo.Create(ctx, ws.ID, "Test Key")
	if err != nil {
		t.Fatalf("failed to create API Key: %v", err)
	}

	// Create Telegram Connection
	connectionID := uuid.New()
	senderIdentity := "@my_telegram_bot_" + uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO connections (id, workspace_id, name, channel, sender_identity, status, is_default, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
	`, connectionID, ws.ID, "TG Webhook Connection", "telegram", senderIdentity, "active", true)
	if err != nil {
		t.Fatalf("failed to insert connection: %v", err)
	}

	// Resolve/Create Contact with Telegram channel identity
	customerIdentity := "cust_tg_123_" + uuid.New().String()
	contact, err := contactRepo.ResolveContact(ctx, ws.ID, "telegram", customerIdentity, "Test Customer", "test_cust", "+1234567")
	if err != nil {
		t.Fatalf("failed to resolve contact: %v", err)
	}

	h := handler.NewTypebotWebhookHandler(pool, pub)

	t.Run("ProcessValidWebhookAndPublish", func(t *testing.T) {
		e := echo.New()
		e.Use(middleware.AuthMiddleware(apiKeyRepo))
		e.POST("/api/integrations/typebot", h.Handle)

		payload := handler.TypebotWebhookPayload{
			Message:      "hello from typebot",
			ConnectionID: connectionID.String(),
			ContactID:    contact.ID.String(),
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/integrations/typebot?token=%s", rawKey), bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		if len(pub.published) != 1 {
			t.Fatalf("expected 1 published message, got %d", len(pub.published))
		}

		published := pub.published[0]
		if published.subject != "messages.outbound" {
			t.Errorf("expected subject 'messages.outbound', got '%s'", published.subject)
		}

		var qmsg domain.QueueMessage
		if err := json.Unmarshal(published.data, &qmsg); err != nil {
			t.Fatalf("failed to unmarshal published QueueMessage: %v", err)
		}

		if qmsg.WorkspaceID != ws.ID {
			t.Errorf("expected WorkspaceID %s, got %s", ws.ID, qmsg.WorkspaceID)
		}
		if qmsg.ConnectionID != connectionID {
			t.Errorf("expected ConnectionID %s, got %s", connectionID, qmsg.ConnectionID)
		}
		if qmsg.SenderIdentity != senderIdentity {
			t.Errorf("expected SenderIdentity '%s', got '%s'", senderIdentity, qmsg.SenderIdentity)
		}
		if qmsg.To != customerIdentity {
			t.Errorf("expected To '%s', got '%s'", customerIdentity, qmsg.To)
		}
		if qmsg.Channel != "telegram" {
			t.Errorf("expected Channel 'telegram', got '%s'", qmsg.Channel)
		}
		if qmsg.Body != "hello from typebot" {
			t.Errorf("expected Body 'hello from typebot', got '%s'", qmsg.Body)
		}
		if qmsg.TraceID == "" {
			t.Error("expected non-empty TraceID")
		}
	})

	t.Run("MissingRequiredField_Returns400", func(t *testing.T) {
		e := echo.New()
		e.Use(middleware.AuthMiddleware(apiKeyRepo))
		e.POST("/api/integrations/typebot", h.Handle)

		// missing Message field
		payload := handler.TypebotWebhookPayload{
			ConnectionID: connectionID.String(),
			ContactID:    contact.ID.String(),
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/integrations/typebot?token=%s", rawKey), bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}

		var resp map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if !strings.Contains(resp["message"], "missing required fields") {
			t.Errorf("expected missing required fields error, got: %s", resp["message"])
		}
	})

	t.Run("UnknownConnectionID_Returns500", func(t *testing.T) {
		e := echo.New()
		e.Use(middleware.AuthMiddleware(apiKeyRepo))
		e.POST("/api/integrations/typebot", h.Handle)

		payload := handler.TypebotWebhookPayload{
			Message:      "hello",
			ConnectionID: uuid.New().String(), // random unknown UUID
			ContactID:    contact.ID.String(),
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/integrations/typebot?token=%s", rawKey), bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rec.Code)
		}

		var resp map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if !strings.Contains(resp["message"], "failed to resolve connection") {
			t.Errorf("expected failed to resolve connection error, got: %s", resp["message"])
		}
	})
}
