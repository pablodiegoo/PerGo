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
	"github.com/pablojhp.pergo/api"
	"github.com/pablojhp.pergo/internal/api/handler"
	"github.com/pablojhp.pergo/internal/api/handler/admin"
	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/pages"
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
		assert.Contains(t, body, `data-url="/api/openapi.json"`)
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
		assert.Contains(t, body, "version: 2.0.0")
		assert.Contains(t, body, "/api/v1/waba/flows/data-exchange:")
		assert.Contains(t, body, "/api/v1/connections/{id}/flow-public-key:")
		assert.Contains(t, body, "FlowDataExchangeRequest:")
		assert.Contains(t, body, "FlowPublicKeyResponse:")
	})

	// 3. Verify GET /api/openapi.json delivers valid OpenAPI 3.1 JSON document
	t.Run("GET /api/openapi.json delivers OpenAPI 3.1 JSON specification", func(t *testing.T) {
		for _, p := range []string{"/api/openapi.json", "/docs/openapi.json", "/openapi.json"} {
			req := httptest.NewRequest(http.MethodGet, p, nil)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code, "endpoint %s must return 200 OK", p)
			assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
			body := rec.Body.String()
			assert.Contains(t, body, `"openapi": "3.1.0"`)
			assert.Contains(t, body, `"version": "2.0.0"`)
			assert.Contains(t, body, `"PerGo Omnichannel CPaaS API"`)
			assert.Contains(t, body, "Quickstart: Time-To-First-Message")
			assert.Contains(t, body, `"/api/v1/workspaces/{id}/retention":`)
		}
	})

	// 4. Verify GET /docs/scalar.js delivers embedded offline Scalar JavaScript bundle
	t.Run("GET /docs/scalar.js delivers compiled binary assets", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/docs/scalar.js", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Header().Get("Content-Type"), "javascript")
		assert.Greater(t, rec.Body.Len(), 50000, "Scalar standalone JS must be fully embedded and > 50KB")
	})

	// 5. Verify GET /llms.txt and /llms-full.txt deliver embedded curated and full agent indexes
	t.Run("GET /llms.txt and /llms-full.txt deliver embedded agent markdown assets", func(t *testing.T) {
		reqLLMs := httptest.NewRequest(http.MethodGet, "/llms.txt", nil)
		recLLMs := httptest.NewRecorder()
		e.ServeHTTP(recLLMs, reqLLMs)

		assert.Equal(t, http.StatusOK, recLLMs.Code)
		assert.Equal(t, "text/markdown; charset=utf-8", recLLMs.Header().Get("Content-Type"))
		assert.Contains(t, recLLMs.Body.String(), "# PerGo Omnichannel CPaaS Gateway")
		assert.Contains(t, recLLMs.Body.String(), "## Documentação Principal")
		assert.Contains(t, recLLMs.Body.String(), "## Optional")

		reqFull := httptest.NewRequest(http.MethodGet, "/llms-full.txt", nil)
		recFull := httptest.NewRecorder()
		e.ServeHTTP(recFull, reqFull)

		assert.Equal(t, http.StatusOK, recFull.Code)
		assert.Equal(t, "text/markdown; charset=utf-8", recFull.Header().Get("Content-Type"))
		assert.Greater(t, recFull.Body.Len(), 5000)
		assert.Contains(t, recFull.Body.String(), "# PerGo Omnichannel CPaaS - Full Developer Documentation")
	})

	// 6. Verify documentation, OpenAPI, and llms.txt endpoints bypass AuthMiddleware without credentials
	t.Run("Documentation endpoints bypass AuthMiddleware anonymously", func(t *testing.T) {
		authEcho := echo.New()
		authEcho.Use(mw.AuthMiddleware(nil))
		docsHandler.RegisterRoutes(authEcho)

		publicPaths := []string{
			"/docs",
			"/docs/",
			"/docs/openapi.yaml",
			"/openapi.yaml",
			"/api/openapi.yaml",
			"/docs/openapi.json",
			"/openapi.json",
			"/api/openapi.json",
			"/docs/scalar.js",
			"/llms.txt",
			"/llms-full.txt",
		}

		for _, p := range publicPaths {
			req := httptest.NewRequest(http.MethodGet, p, nil)
			rec := httptest.NewRecorder()
			authEcho.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code, "path %s must be anonymously accessible through AuthMiddleware", p)
		}
	})
}

func TestDocsE2E_AgentDiscoveryHeadersAndMetadata(t *testing.T) {
	e := echo.New()

	docsHandler := handler.NewDocsHandler()
	docsHandler.RegisterRoutes(e)

	healthHandler := &handler.HealthHandler{}
	healthHandler.RegisterRoutes(e)

	e.GET("/", func(c *echo.Context) error {
		return mw.Render(c, http.StatusOK, pages.Landing())
	}, mw.AgentDiscoveryMiddleware())

	const expectedLink = `</llms.txt>; rel="describedby", </llms-full.txt>; rel="alternate"; type="text/markdown"`

	// 1. Verify RFC 8288 Link header across public entrypoints
	t.Run("Public endpoints return RFC 8288 Link header", func(t *testing.T) {
		endpoints := []string{"/", "/healthz", "/readyz", "/docs", "/docs/"}
		for _, ep := range endpoints {
			req := httptest.NewRequest(http.MethodGet, ep, nil)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			assert.Equal(t, expectedLink, rec.Header().Get("Link"), "endpoint %s must return RFC 8288 discovery Link header", ep)
		}
	})

	// 2. Verify Landing page HTML contains discovery <link> tags in <head>
	t.Run("Landing page HTML <head> includes discovery link tags", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
		body := rec.Body.String()
		assert.Contains(t, body, `<link rel="describedby" href="/llms.txt"`)
		assert.Contains(t, body, `<link rel="alternate" type="text/markdown" href="/llms-full.txt" title="Full Documentation for LLMs"`)
	})

	// 3. Verify Developer documentation portal HTML contains discovery <link> tags in <head>
	t.Run("Developer documentation portal HTML <head> includes discovery link tags", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/docs", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
		body := rec.Body.String()
		assert.Contains(t, body, `<link rel="describedby" href="/llms.txt" />`)
		assert.Contains(t, body, `<link rel="alternate" type="text/markdown" href="/llms-full.txt" title="Full Documentation for LLMs" />`)
	})
}

func TestDocsE2E_ContentNegotiation(t *testing.T) {
	e := echo.New()

	docsHandler := handler.NewDocsHandler()
	docsHandler.RegisterRoutes(e)

	e.GET("/", func(c *echo.Context) error {
		return mw.Render(c, http.StatusOK, pages.Landing())
	}, mw.AgentDiscoveryMiddleware(), mw.ContentNegotiationInterceptor(api.LLMsTxt))

	t.Run("GET /docs with Accept: text/markdown returns full markdown documentation", func(t *testing.T) {
		endpoints := []string{"/docs", "/docs/"}
		for _, ep := range endpoints {
			req := httptest.NewRequest(http.MethodGet, ep, nil)
			req.Header.Set("Accept", "text/markdown")
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, "text/markdown; charset=utf-8", rec.Header().Get("Content-Type"))
			assert.Equal(t, "Accept, Accept-Encoding", rec.Header().Get("Vary"))
			assert.NotEmpty(t, rec.Header().Get("Link"))
			body := rec.Body.String()
			assert.Contains(t, body, "# PerGo Omnichannel CPaaS - Full Developer Documentation")
			assert.NotContains(t, body, "<html")
			assert.NotContains(t, body, "<script")
		}
	})

	t.Run("GET / with Accept: text/markdown returns raw markdown summary", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Accept", "text/markdown")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "text/markdown; charset=utf-8", rec.Header().Get("Content-Type"))
		assert.Equal(t, "Accept, Accept-Encoding", rec.Header().Get("Vary"))
		assert.NotEmpty(t, rec.Header().Get("Link"))
		body := rec.Body.String()
		assert.Contains(t, body, "# PerGo Omnichannel CPaaS Gateway")
		assert.NotContains(t, body, "<html")
		assert.NotContains(t, body, "<script")
	})

	t.Run("Standard browser requests (Accept: text/html) return HTML with Vary header", func(t *testing.T) {
		// Test landing page /
		reqLanding := httptest.NewRequest(http.MethodGet, "/", nil)
		reqLanding.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		recLanding := httptest.NewRecorder()
		e.ServeHTTP(recLanding, reqLanding)

		assert.Equal(t, http.StatusOK, recLanding.Code)
		assert.Contains(t, recLanding.Header().Get("Content-Type"), "text/html")
		assert.Equal(t, "Accept, Accept-Encoding", recLanding.Header().Get("Vary"))
		assert.Contains(t, recLanding.Body.String(), "<html")

		// Test docs portal /docs
		reqDocs := httptest.NewRequest(http.MethodGet, "/docs", nil)
		reqDocs.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		recDocs := httptest.NewRecorder()
		e.ServeHTTP(recDocs, reqDocs)

		assert.Equal(t, http.StatusOK, recDocs.Code)
		assert.Contains(t, recDocs.Header().Get("Content-Type"), "text/html")
		assert.Equal(t, "Accept, Accept-Encoding", recDocs.Header().Get("Vary"))
		assert.Contains(t, recDocs.Body.String(), "<title>PerGo API Reference</title>")
	})

	t.Run("Accept header permutations on /", func(t *testing.T) {
		tests := []struct {
			name           string
			accept         string
			expectMarkdown bool
		}{
			{
				name:           "text/markdown; charset=utf-8",
				accept:         "text/markdown; charset=utf-8",
				expectMarkdown: true,
			},
			{
				name:           "text/markdown preferred over html",
				accept:         "text/markdown;q=1.0, text/html;q=0.8",
				expectMarkdown: true,
			},
			{
				name:           "html preferred over markdown",
				accept:         "text/html;q=1.0, text/markdown;q=0.5",
				expectMarkdown: false,
			},
			{
				name:           "wildcard */*",
				accept:         "*/*",
				expectMarkdown: false,
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.Header.Set("Accept", tc.accept)
				rec := httptest.NewRecorder()
				e.ServeHTTP(rec, req)

				assert.Equal(t, http.StatusOK, rec.Code)
				assert.Equal(t, "Accept, Accept-Encoding", rec.Header().Get("Vary"))
				if tc.expectMarkdown {
					assert.Equal(t, "text/markdown; charset=utf-8", rec.Header().Get("Content-Type"))
					assert.Contains(t, rec.Body.String(), "# PerGo Omnichannel CPaaS Gateway")
				} else {
					assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
					assert.Contains(t, rec.Body.String(), "<html")
				}
			})
		}
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
