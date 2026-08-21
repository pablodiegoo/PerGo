package admin_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/api/handler/admin"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestWorkspaceHandler_WebhookSecret(t *testing.T) {
	dbURL := testDBURL
	if dbURL == "" {
		dbURL = os.Getenv("PERGO_DATABASE_URL")
	}
	if dbURL == "" {
		t.Skip("testcontainers postgres not available")
	}

	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed to create pgxpool: %v", err)
	}
	defer pool.Close()

	wsRepo := repository.NewWorkspaceRepository(pool)
	apiKeyRepo := repository.NewAPIKeyRepository(pool)
	handler := &admin.WorkspaceHandler{
		Repo:    wsRepo,
		APIKeys: apiKeyRepo,
	}

	ws, err := wsRepo.Create(ctx, "wh_handler_ws_"+uuid.New().String()[:8])
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	e := echo.New()

	t.Run("Get initial empty secret", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/workspaces/%s/webhook-secret", ws.ID), nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/workspaces/:id/webhook-secret")
		c.SetPathValues(echo.PathValues{{Name: "id", Value: ws.ID.String()}})

		if err := handler.GetWebhookSecret(c); err != nil {
			t.Fatalf("GetWebhookSecret failed: %v", err)
		}

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}
		var resp map[string]string
		json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp["webhook_secret"] != "" {
			t.Errorf("expected empty secret, got %q", resp["webhook_secret"])
		}
		if resp["workspace_id"] != ws.ID.String() {
			t.Errorf("expected workspace ID %q, got %q", ws.ID.String(), resp["workspace_id"])
		}
	})

	t.Run("Generate random 64-character hex secret", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/workspaces/%s/webhook-secret", ws.ID), nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/workspaces/:workspace_id/webhook-secret")
		c.SetPathValues(echo.PathValues{{Name: "workspace_id", Value: ws.ID.String()}})

		if err := handler.GenerateWebhookSecret(c); err != nil {
			t.Fatalf("GenerateWebhookSecret failed: %v", err)
		}

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}
		var resp map[string]string
		json.Unmarshal(rec.Body.Bytes(), &resp)
		sec := resp["webhook_secret"]
		if len(sec) != 64 {
			t.Errorf("expected 64-character hex secret, got length %d (%q)", len(sec), sec)
		}

		// Verify persisted in DB
		freshWS, err := wsRepo.GetByID(ctx, ws.ID)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if freshWS.WebhookSecret == nil || *freshWS.WebhookSecret != sec {
			t.Errorf("expected persisted secret %q, got %v", sec, freshWS.WebhookSecret)
		}
	})

	t.Run("Resolve workspace ID from context (API key mode)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/workspaces/webhook-secret", nil)
		// Inject workspace into context
		req = req.WithContext(tenant.WithWorkspaceID(req.Context(), ws.ID))
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/workspaces/webhook-secret")

		if err := handler.GenerateWebhookSecret(c); err != nil {
			t.Fatalf("GenerateWebhookSecret failed: %v", err)
		}

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}
		var resp map[string]string
		json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp["workspace_id"] != ws.ID.String() {
			t.Errorf("expected workspace ID %q, got %q", ws.ID.String(), resp["workspace_id"])
		}
	})

	t.Run("Set custom secret via JSON payload", func(t *testing.T) {
		custom := "my-very-custom-secret-key-12345678"
		body := fmt.Sprintf(`{"webhook_secret":%q}`, custom)
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/workspaces/%s/webhook-secret", ws.ID), strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/workspaces/:id/webhook-secret")
		c.SetPathValues(echo.PathValues{{Name: "id", Value: ws.ID.String()}})

		if err := handler.GenerateWebhookSecret(c); err != nil {
			t.Fatalf("GenerateWebhookSecret with custom secret failed: %v", err)
		}

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}
		var resp map[string]string
		json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp["webhook_secret"] != custom {
			t.Errorf("expected custom secret %q, got %q", custom, resp["webhook_secret"])
		}

		// Verify Get returns updated custom secret
		getReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/workspaces/%s/webhook-secret", ws.ID), nil)
		getRec := httptest.NewRecorder()
		getC := e.NewContext(getReq, getRec)
		getC.SetPath("/workspaces/:id/webhook-secret")
		getC.SetPathValues(echo.PathValues{{Name: "id", Value: ws.ID.String()}})

		if err := handler.GetWebhookSecret(getC); err != nil {
			t.Fatalf("GetWebhookSecret failed: %v", err)
		}
		var getResp map[string]string
		json.Unmarshal(getRec.Body.Bytes(), &getResp)
		if getResp["webhook_secret"] != custom {
			t.Errorf("expected custom secret %q on GET, got %q", custom, getResp["webhook_secret"])
		}
	})

	t.Run("Missing or invalid workspace ID returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/workspaces/invalid-uuid/webhook-secret", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/workspaces/:id/webhook-secret")
		c.SetPathValues(echo.PathValues{{Name: "id", Value: "invalid-uuid"}})

		if err := handler.GetWebhookSecret(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request, got %d", rec.Code)
		}
	})
}

func TestWorkspaceHandler_SetFlowWebhookURL(t *testing.T) {
	dbURL := testDBURL
	if dbURL == "" {
		dbURL = os.Getenv("PERGO_DATABASE_URL")
	}
	if dbURL == "" {
		t.Skip("testcontainers postgres not available")
	}

	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed to create pgxpool: %v", err)
	}
	defer pool.Close()

	wsRepo := repository.NewWorkspaceRepository(pool)
	apiKeyRepo := repository.NewAPIKeyRepository(pool)
	handler := &admin.WorkspaceHandler{
		Repo:    wsRepo,
		APIKeys: apiKeyRepo,
	}

	ws, err := wsRepo.Create(ctx, "flow_handler_ws_"+uuid.New().String()[:8])
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	e := echo.New()

	t.Run("Set flow webhook URL via JSON", func(t *testing.T) {
		flowURL := "https://backend.example.com/flow/endpoint"
		body := fmt.Sprintf(`{"flow_webhook_url": "%s"}`, flowURL)
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/workspaces/%s/flow-webhook-url", ws.ID), strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/workspaces/:id/flow-webhook-url")
		c.SetPathValues(echo.PathValues{{Name: "id", Value: ws.ID.String()}})

		if err := handler.SetFlowWebhookURL(c); err != nil {
			t.Fatalf("SetFlowWebhookURL failed: %v", err)
		}

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}

		fetched, err := wsRepo.GetByID(ctx, ws.ID)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if fetched.FlowWebhookURL == nil || *fetched.FlowWebhookURL != flowURL {
			t.Errorf("expected flow_webhook_url %q, got %v", flowURL, fetched.FlowWebhookURL)
		}
	})

	t.Run("Set flow webhook URL via Form and HTMX", func(t *testing.T) {
		flowURL := "https://crm.partner.io/flows"
		form := strings.NewReader(fmt.Sprintf("flow_webhook_url=%s", flowURL))
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/workspaces/%s/flow-webhook-url", ws.ID), form)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/workspaces/:id/flow-webhook-url")
		c.SetPathValues(echo.PathValues{{Name: "id", Value: ws.ID.String()}})

		if err := handler.SetFlowWebhookURL(c); err != nil {
			t.Fatalf("SetFlowWebhookURL failed: %v", err)
		}

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "flow-webhook-card") {
			t.Errorf("expected rendered HTMX card in response, got %s", rec.Body.String())
		}
	})
}

