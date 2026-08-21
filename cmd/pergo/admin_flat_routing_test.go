package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/handler/admin"
	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestLegacyAdminRouting_Redirect302(t *testing.T) {
	e := echo.New()
	adminGroup := e.Group("/admin")

	dummyID := uuid.New().String()
	wsID := uuid.New().String()

	// Legacy 302 Redirect Routes matching cmd/pergo/main.go
	adminGroup.GET("/workspaces/:workspace_id/templates", func(c *echo.Context) error {
		return c.Redirect(http.StatusFound, "/admin/templates")
	})
	adminGroup.GET("/workspaces/:workspace_id/templates/new", func(c *echo.Context) error {
		return c.Redirect(http.StatusFound, "/admin/templates/new")
	})

	adminGroup.GET("/integrations", func(c *echo.Context) error {
		return c.Redirect(http.StatusFound, "/admin/integrations/headless")
	})
	adminGroup.GET("/workspaces/:workspace_id/integrations", func(c *echo.Context) error {
		return c.Redirect(http.StatusFound, "/admin/integrations/headless")
	})
	adminGroup.GET("/workspaces/:workspace_id/integrations/headless", func(c *echo.Context) error {
		return c.Redirect(http.StatusFound, "/admin/integrations/headless")
	})
	adminGroup.GET("/workspaces/:workspace_id/integrations/chatwoot", func(c *echo.Context) error {
		return c.Redirect(http.StatusFound, "/admin/integrations/chatwoot")
	})
	adminGroup.GET("/workspaces/:workspace_id/integrations/typebot", func(c *echo.Context) error {
		return c.Redirect(http.StatusFound, "/admin/integrations/typebot")
	})

	adminGroup.GET("/workspaces/:workspace_id/webhooks", func(c *echo.Context) error {
		return c.Redirect(http.StatusFound, "/admin/webhooks")
	})
	adminGroup.GET("/workspaces/:workspace_id/webhooks/subscriptions/new", func(c *echo.Context) error {
		return c.Redirect(http.StatusFound, "/admin/webhooks/subscriptions/new")
	})
	adminGroup.GET("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id/edit", func(c *echo.Context) error {
		id, _ := echo.PathParam[string](c, "subscription_id")
		return c.Redirect(http.StatusFound, "/admin/webhooks/subscriptions/"+id+"/edit")
	})
	adminGroup.GET("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id/rotate-form", func(c *echo.Context) error {
		id, _ := echo.PathParam[string](c, "subscription_id")
		return c.Redirect(http.StatusFound, "/admin/webhooks/subscriptions/"+id+"/rotate-form")
	})
	adminGroup.GET("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id/test-form", func(c *echo.Context) error {
		id, _ := echo.PathParam[string](c, "subscription_id")
		return c.Redirect(http.StatusFound, "/admin/webhooks/subscriptions/"+id+"/test-form")
	})

	adminGroup.GET("/workspaces/:workspace_id/tags", func(c *echo.Context) error {
		return c.Redirect(http.StatusFound, "/admin/tags")
	})
	adminGroup.GET("/workspaces/:workspace_id/contacts/export", func(c *echo.Context) error {
		return c.Redirect(http.StatusFound, "/admin/contacts/export")
	})

	adminGroup.GET("/workspaces/:workspace_id/campaigns", func(c *echo.Context) error {
		return c.Redirect(http.StatusFound, "/admin/campaigns")
	})
	adminGroup.GET("/workspaces/:workspace_id/campaigns/new", func(c *echo.Context) error {
		return c.Redirect(http.StatusFound, "/admin/campaigns/new")
	})
	adminGroup.GET("/workspaces/:workspace_id/campaigns/:id/row", func(c *echo.Context) error {
		id, _ := echo.PathParam[string](c, "id")
		return c.Redirect(http.StatusFound, "/admin/campaigns/"+id+"/row")
	})
	adminGroup.GET("/workspaces/:workspace_id/campaigns/:id/skipped/download", func(c *echo.Context) error {
		id, _ := echo.PathParam[string](c, "id")
		return c.Redirect(http.StatusFound, "/admin/campaigns/"+id+"/skipped/download")
	})

	testCases := []struct {
		legacyPath       string
		expectedLocation string
	}{
		{
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/campaigns", wsID),
			expectedLocation: "/admin/campaigns",
		},
		{
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/campaigns/new", wsID),
			expectedLocation: "/admin/campaigns/new",
		},
		{
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/campaigns/%s/row", wsID, dummyID),
			expectedLocation: "/admin/campaigns/" + dummyID + "/row",
		},
		{
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/campaigns/%s/skipped/download", wsID, dummyID),
			expectedLocation: "/admin/campaigns/" + dummyID + "/skipped/download",
		},
		{
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/tags", wsID),
			expectedLocation: "/admin/tags",
		},
		{
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/contacts/export", wsID),
			expectedLocation: "/admin/contacts/export",
		},
		{
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/templates", wsID),
			expectedLocation: "/admin/templates",
		},
		{
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/templates/new", wsID),
			expectedLocation: "/admin/templates/new",
		},
		{
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/integrations", wsID),
			expectedLocation: "/admin/integrations/headless",
		},
		{
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/integrations/headless", wsID),
			expectedLocation: "/admin/integrations/headless",
		},
		{
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/integrations/chatwoot", wsID),
			expectedLocation: "/admin/integrations/chatwoot",
		},
		{
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/integrations/typebot", wsID),
			expectedLocation: "/admin/integrations/typebot",
		},
		{
			legacyPath:       "/admin/integrations",
			expectedLocation: "/admin/integrations/headless",
		},
		{
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/webhooks", wsID),
			expectedLocation: "/admin/webhooks",
		},
		{
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/webhooks/subscriptions/new", wsID),
			expectedLocation: "/admin/webhooks/subscriptions/new",
		},
		{
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/webhooks/subscriptions/%s/edit", wsID, dummyID),
			expectedLocation: "/admin/webhooks/subscriptions/" + dummyID + "/edit",
		},
		{
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/webhooks/subscriptions/%s/rotate-form", wsID, dummyID),
			expectedLocation: "/admin/webhooks/subscriptions/" + dummyID + "/rotate-form",
		},
		{
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/webhooks/subscriptions/%s/test-form", wsID, dummyID),
			expectedLocation: "/admin/webhooks/subscriptions/" + dummyID + "/test-form",
		},
	}

	for _, tc := range testCases {
		t.Run("Redirect "+tc.legacyPath, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.legacyPath, nil)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			if rec.Code != http.StatusFound {
				t.Fatalf("expected 302 Found for %s, got %d", tc.legacyPath, rec.Code)
			}
			loc := rec.Header().Get("Location")
			if loc != tc.expectedLocation {
				t.Fatalf("expected redirect Location %q, got %q", tc.expectedLocation, loc)
			}
		})
	}
}

func setupFlatRoutingEcho(t *testing.T) (*echo.Echo, *repository.Workspace) {
	t.Helper()
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("PostgreSQL not available, skipping integration test")
	}

	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		t.Fatalf("failed to create sql.DB: %v", err)
	}
	_ = postgres.RunMigrations(db)
	db.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	ws, err := wsRepo.Create(ctx, "Flat Test WS "+uuid.New().String()[:6])
	if err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}

	kek := []byte("01234567890123456789012345678901")
	encryptor, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	connRepo := repository.NewConnectionRepository(pool, encryptor)
	tagRepo := repository.NewTagRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	campaignRepo := repository.NewCampaignRepository(pool)
	wabaTemplateRepo := repository.NewWABATemplateRepository(pool)
	webhookDLQRepo := repository.NewWebhookDLQRepository(pool, encryptor)
	webhookSubRepo := repository.NewWebhookSubscriptionRepository(pool, encryptor)
	integrationRepo := repository.NewIntegrationRepository(pool, encryptor)
	apiKeyRepo := repository.NewAPIKeyRepository(pool)

	e := echo.New()
	e.Use(mw.HTMXMiddleware())
	e.Use(mw.ActiveWorkspaceMiddleware(wsRepo))

	adminGroup := e.Group("/admin")

	// Handlers
	wabaTemplateHandler := admin.NewWABATemplateHandler(wabaTemplateRepo, connRepo)
	developerHandler := admin.NewDeveloperHandler(wsRepo, apiKeyRepo, []byte("testsecret"), "http://localhost:8080")
	chatwootAdminHandler := admin.NewChatwootAdminHandler(integrationRepo)
	typebotAdminHandler := admin.NewTypebotSettingsHandler(integrationRepo, connRepo)
	webhookHandler := admin.NewWebhookDLQHandler(webhookDLQRepo, webhookSubRepo, wsRepo, nil)
	tagAdminHandler := admin.NewTagAdminHandler(tagRepo, contactRepo, wsRepo)
	campaignHandler := admin.NewCampaignHandler(campaignRepo, wabaTemplateRepo, connRepo, tagRepo, nil)

	// Flat Routes
	adminGroup.GET("/templates", wabaTemplateHandler.List)
	adminGroup.POST("/templates", wabaTemplateHandler.Create)
	adminGroup.GET("/templates/new", wabaTemplateHandler.NewForm)
	adminGroup.POST("/templates/:template_id/sync", wabaTemplateHandler.Sync)
	adminGroup.DELETE("/templates/:template_id", wabaTemplateHandler.Delete)
	adminGroup.POST("/templates/preview", wabaTemplateHandler.Preview)

	adminGroup.GET("/developers", developerHandler.GetPortal)
	adminGroup.POST("/developers/keys", developerHandler.CreateAPIKey)
	adminGroup.DELETE("/developers/keys/:key_id", developerHandler.RevokeAPIKey)
	adminGroup.POST("/developers/webhook-secret/rotate", developerHandler.RotateWebhookSecret)
	adminGroup.POST("/developers/sandbox/test", developerHandler.SandboxTest)
	adminGroup.POST("/developers/sso-generate", developerHandler.GenerateSSO)

	adminGroup.GET("/integrations/headless", developerHandler.GetPortal)
	adminGroup.POST("/integrations/headless/sso-generate", developerHandler.GenerateSSO)
	adminGroup.GET("/integrations/chatwoot", chatwootAdminHandler.GetSettings)
	adminGroup.POST("/integrations/chatwoot", chatwootAdminHandler.PostSettings)
	adminGroup.GET("/integrations/typebot", typebotAdminHandler.GetSettings)
	adminGroup.POST("/integrations/typebot", typebotAdminHandler.PostSettings)

	adminGroup.GET("/webhooks", webhookHandler.Page)
	adminGroup.GET("/webhooks/subscriptions/new", webhookHandler.GetSubscriptionNewForm)
	adminGroup.GET("/webhooks/subscriptions/:subscription_id/edit", webhookHandler.GetSubscriptionEditForm)
	adminGroup.GET("/webhooks/subscriptions/:subscription_id/rotate-form", webhookHandler.GetRotateSecretForm)
	adminGroup.POST("/webhooks/subscriptions", webhookHandler.CreateSubscription)
	adminGroup.POST("/webhooks/subscriptions/:subscription_id", webhookHandler.UpdateSubscription)
	adminGroup.POST("/webhooks/subscriptions/:subscription_id/rotate", webhookHandler.RotateSubscriptionSecret)
	adminGroup.POST("/webhooks/subscriptions/:subscription_id/ping", webhookHandler.PingSubscription)
	adminGroup.DELETE("/webhooks/subscriptions/:subscription_id", webhookHandler.DeleteSubscription)
	adminGroup.GET("/webhooks/subscriptions/:subscription_id/test-form", webhookHandler.GetSubscriptionTestForm)
	adminGroup.POST("/webhooks/subscriptions/:subscription_id/test", webhookHandler.TestSubscription)

	adminGroup.GET("/tags", tagAdminHandler.Page)
	adminGroup.POST("/tags", tagAdminHandler.CreateTag)
	adminGroup.DELETE("/tags/:id", tagAdminHandler.DeleteTag)
	adminGroup.POST("/contacts/import", tagAdminHandler.ImportContactsCSV)
	adminGroup.GET("/contacts/export", tagAdminHandler.ExportContactsCSV)

	adminGroup.GET("/campaigns", campaignHandler.List)
	adminGroup.GET("/campaigns/new", campaignHandler.NewForm)
	adminGroup.POST("/campaigns/upload", campaignHandler.UploadCSV)
	adminGroup.POST("/campaigns", campaignHandler.Create)
	adminGroup.GET("/campaigns/:id/row", campaignHandler.GetRow)
	adminGroup.GET("/campaigns/:id/skipped/download", campaignHandler.DownloadSkipped)
	adminGroup.POST("/campaigns/:id/start", campaignHandler.Start)
	adminGroup.POST("/campaigns/:id/pause", campaignHandler.Pause)
	adminGroup.POST("/campaigns/:id/resume", campaignHandler.Resume)
	adminGroup.POST("/campaigns/:id/cancel", campaignHandler.Cancel)
	adminGroup.DELETE("/campaigns/:id", campaignHandler.Delete)

	return e, ws
}

func TestFlatAdminRouting_Render200(t *testing.T) {
	e, ws := setupFlatRoutingEcho(t)

	flatPaths := []string{
		"/admin/campaigns",
		"/admin/campaigns/new",
		"/admin/tags",
		"/admin/templates",
		"/admin/templates/new",
		"/admin/webhooks",
		"/admin/webhooks/subscriptions/new",
		"/admin/developers",
		"/admin/integrations/headless",
		"/admin/integrations/chatwoot",
		"/admin/integrations/typebot",
	}

	for _, path := range flatPaths {
		t.Run("GET "+path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.AddCookie(&http.Cookie{
				Name:  "pergo-active-workspace",
				Value: ws.ID.String(),
			})
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200 OK for %s, got %d. Body: %s", path, rec.Code, rec.Body.String())
			}
			if len(rec.Body.Bytes()) == 0 {
				t.Fatalf("expected non-empty response body for %s", path)
			}
		})
	}
}

func TestFlatAdminRouting_ContextResolutionWithoutParam(t *testing.T) {
	e, ws := setupFlatRoutingEcho(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/tags", nil)
	ctx := tenant.WithWorkspaceID(req.Context(), ws.ID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK via context resolution, got %d", rec.Code)
	}
}
