package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/api/handler"
	"github.com/pablojhp.pergo/internal/api/handler/admin"
	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDocsE2E_PortalAndAssetDelivery(t *testing.T) {
	e := echo.New()

	docsHandler := handler.NewDocsHandler()
	docsHandler.RegisterRoutes(e)

	// 1. Verify GET /docs returns Scalar UI HTML with zero external CDN script dependencies
	t.Run("GET /docs renders Scalar developer portal", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/docs", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
		body := rec.Body.String()
		assert.Contains(t, body, "<title>PerGo API Reference</title>")
		assert.Contains(t, body, `data-url="/docs/openapi.yaml"`)
		assert.Contains(t, body, `<script src="/docs/scalar.js"></script>`)
	})

	// 2. Verify GET /docs/openapi.yaml returns valid OpenAPI 3.1 YAML document
	t.Run("GET /docs/openapi.yaml delivers OpenAPI 3.1 specification", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/docs/openapi.yaml", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Header().Get("Content-Type"), "yaml")
		body := rec.Body.String()
		assert.Contains(t, body, "openapi: 3.1.0")
		assert.Contains(t, body, "PerGo Omnichannel CPaaS API")
		assert.Contains(t, body, "/api/v1/waba/flows/data-exchange:")
		assert.Contains(t, body, "/api/v1/connections/{id}/flow-public-key:")
		assert.Contains(t, body, "FlowDataExchangeRequest:")
		assert.Contains(t, body, "FlowPublicKeyResponse:")
	})

	// 3. Verify GET /docs/scalar.js delivers embedded offline Scalar JavaScript bundle
	t.Run("GET /docs/scalar.js delivers compiled binary assets", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/docs/scalar.js", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Header().Get("Content-Type"), "javascript")
		assert.Greater(t, rec.Body.Len(), 50000, "Scalar standalone JS must be fully embedded and > 50KB")
	})
}

func TestAdminDevelopersE2E_FullLifecycle(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("PostgreSQL testcontainers not available, skipping integration test")
	}

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	apiKeyRepo := repository.NewAPIKeyRepository(pool)

	ws, err := wsRepo.Create(ctx, "Dev Portal E2E WS "+uuid.New().String()[:6])
	require.NoError(t, err)
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	e := echo.New()
	e.Use(mw.HTMXMiddleware())
	e.Use(mw.ActiveWorkspaceMiddleware(wsRepo))

	adminGroup := e.Group("/admin")
	developerHandler := admin.NewDeveloperHandler(wsRepo, apiKeyRepo, []byte("dev-test-secret-32-bytes-e2e123"), "http://localhost:8080")

	adminGroup.GET("/developers", developerHandler.GetPortal)
	adminGroup.POST("/developers/keys", developerHandler.CreateAPIKey)
	adminGroup.DELETE("/developers/keys/:key_id", developerHandler.RevokeAPIKey)
	adminGroup.POST("/developers/webhook-secret/rotate", developerHandler.RotateWebhookSecret)
	adminGroup.POST("/developers/sandbox/test", developerHandler.SandboxTest)
	adminGroup.POST("/developers/sso-generate", developerHandler.GenerateSSO)

	cookie := &http.Cookie{
		Name:  "pergo-active-workspace",
		Value: ws.ID.String(),
	}

	// 1. GET /admin/developers
	t.Run("GET /admin/developers renders portal", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/developers", nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		assert.Contains(t, body, ws.Name)
		assert.Contains(t, body, "Sandbox de Teste de Payloads Interativos")
		assert.Contains(t, body, "Abrir Scalar Docs (/docs)")
	})

	// 2. POST /admin/developers/webhook-secret/rotate
	t.Run("POST /admin/developers/webhook-secret/rotate generates secret", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/admin/developers/webhook-secret/rotate", nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		assert.Contains(t, body, "webhook-sec-val")

		// Verify database
		freshWS, err := wsRepo.GetByID(ctx, ws.ID)
		require.NoError(t, err)
		require.NotNil(t, freshWS.WebhookSecret)
		assert.NotEmpty(t, *freshWS.WebhookSecret)
	})

	// 3. POST /admin/developers/keys and DELETE /admin/developers/keys/:key_id
	t.Run("API key generation and revocation in developer console", func(t *testing.T) {
		form := url.Values{}
		form.Set("name", "E2E Production CRM Key")
		req := httptest.NewRequest(http.MethodPost, "/admin/developers/keys", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		assert.Contains(t, body, "E2E Production CRM Key")
		assert.Contains(t, body, "new-key-raw-val")

		keys, err := apiKeyRepo.ListByWorkspace(ctx, ws.ID)
		require.NoError(t, err)
		require.Len(t, keys, 1)
		keyID := keys[0].ID

		// Revoke key
		reqRevoke := httptest.NewRequest(http.MethodDelete, "/admin/developers/keys/"+keyID.String(), nil)
		reqRevoke.AddCookie(cookie)
		recRevoke := httptest.NewRecorder()
		e.ServeHTTP(recRevoke, reqRevoke)

		assert.Equal(t, http.StatusOK, recRevoke.Code)
		keysAfter, err := apiKeyRepo.ListByWorkspace(ctx, ws.ID)
		require.NoError(t, err)
		require.NotNil(t, keysAfter[0].RevokedAt)
	})

	// 4. POST /admin/developers/sandbox/test for Interactive Messages
	t.Run("Interactive payload validation in sandbox", func(t *testing.T) {
		interactivePayload := `{
			"to": "+5511988887777",
			"channel": "whatsapp_cloud",
			"type": "interactive",
			"fallback_behavior": "degrade",
			"interactive": {
				"type": "button",
				"header": { "text": "Pesquisa de Satisfação" },
				"body": { "text": "Como você avalia nosso atendimento?" },
				"footer": { "text": "Sua opinião é fundamental" },
				"action": {
					"buttons": [
						{ "type": "reply", "reply": { "id": "btn_otimo", "title": "Ótimo" } },
						{ "type": "reply", "reply": { "id": "btn_bom", "title": "Bom" } }
					]
				}
			}
		}`

		form := url.Values{}
		form.Set("payload", interactivePayload)
		req := httptest.NewRequest(http.MethodPost, "/admin/developers/sandbox/test", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		assert.Contains(t, body, "Payload Válido")
		assert.Contains(t, body, "whatsapp_cloud")
		assert.Contains(t, body, "+5511988887777")
		assert.Contains(t, body, "Simulação de Degradação Automática")
		assert.Contains(t, body, "1. Ótimo")
		assert.Contains(t, body, "2. Bom")
	})
}
