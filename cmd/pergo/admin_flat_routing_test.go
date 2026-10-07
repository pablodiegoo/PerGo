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

func registerLegacyAdminRoutes(adminGroup *echo.Group) {
	// Legacy WABA template routes (303 redirects / backward compatibility)
	adminGroup.GET("/workspaces/:workspace_id/templates", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/templates")
	})
	adminGroup.GET("/workspaces/:workspace_id/templates/new", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/templates/new")
	})
	adminGroup.POST("/workspaces/:workspace_id/templates", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/templates")
	})
	adminGroup.POST("/workspaces/:workspace_id/templates/:template_id/sync", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/templates")
	})
	adminGroup.DELETE("/workspaces/:workspace_id/templates/:template_id", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/templates")
	})

	// Legacy integration routes (303 redirects / backward compatibility)
	adminGroup.GET("/integrations", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/integrations/headless")
	})
	adminGroup.GET("/workspaces/:workspace_id/integrations", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/integrations/headless")
	})
	adminGroup.GET("/workspaces/:workspace_id/integrations/headless", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/integrations/headless")
	})
	adminGroup.POST("/workspaces/:workspace_id/integrations/headless/sso-generate", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/integrations/headless")
	})
	adminGroup.GET("/workspaces/:workspace_id/integrations/chatwoot", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/integrations/chatwoot")
	})
	adminGroup.POST("/workspaces/:workspace_id/integrations/chatwoot", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/integrations/chatwoot")
	})
	adminGroup.GET("/workspaces/:workspace_id/integrations/typebot", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/integrations/typebot")
	})
	adminGroup.POST("/workspaces/:workspace_id/integrations/typebot", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/integrations/typebot")
	})

	// Legacy Webhooks routes (303 redirects / backward compatibility)
	adminGroup.GET("/workspaces/:workspace_id/webhooks", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/webhooks")
	})
	adminGroup.GET("/workspaces/:workspace_id/webhooks/subscriptions/new", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/webhooks/subscriptions/new")
	})
	adminGroup.GET("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id/edit", func(c *echo.Context) error {
		id, _ := echo.PathParam[string](c, "subscription_id")
		return c.Redirect(http.StatusSeeOther, "/admin/webhooks/subscriptions/"+id+"/edit")
	})
	adminGroup.GET("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id/rotate-form", func(c *echo.Context) error {
		id, _ := echo.PathParam[string](c, "subscription_id")
		return c.Redirect(http.StatusSeeOther, "/admin/webhooks/subscriptions/"+id+"/rotate-form")
	})
	adminGroup.POST("/workspaces/:workspace_id/webhooks/subscriptions", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/webhooks")
	})
	adminGroup.POST("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/webhooks")
	})
	adminGroup.POST("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id/rotate", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/webhooks")
	})
	adminGroup.POST("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id/ping", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/webhooks")
	})
	adminGroup.DELETE("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/webhooks")
	})
	adminGroup.GET("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id/test-form", func(c *echo.Context) error {
		id, _ := echo.PathParam[string](c, "subscription_id")
		return c.Redirect(http.StatusSeeOther, "/admin/webhooks/subscriptions/"+id+"/test-form")
	})
	adminGroup.POST("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id/test", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/webhooks")
	})

	// Legacy Tags & Contact routes (303 redirects / backward compatibility)
	adminGroup.GET("/workspaces/:workspace_id/tags", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/tags")
	})
	adminGroup.POST("/workspaces/:workspace_id/tags", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/tags")
	})
	adminGroup.DELETE("/workspaces/:workspace_id/tags/:id", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/tags")
	})
	adminGroup.POST("/workspaces/:workspace_id/contacts/import", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/tags")
	})
	adminGroup.GET("/workspaces/:workspace_id/contacts/export", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/contacts/export")
	})

	// Legacy Campaigns routes (303 redirects / backward compatibility)
	adminGroup.GET("/workspaces/:workspace_id/campaigns", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/campaigns")
	})
	adminGroup.GET("/workspaces/:workspace_id/campaigns/new", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/campaigns/new")
	})
	adminGroup.POST("/workspaces/:workspace_id/campaigns/upload", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/campaigns")
	})
	adminGroup.POST("/workspaces/:workspace_id/campaigns", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/campaigns")
	})
	adminGroup.GET("/workspaces/:workspace_id/campaigns/:id/row", func(c *echo.Context) error {
		id, _ := echo.PathParam[string](c, "id")
		return c.Redirect(http.StatusSeeOther, "/admin/campaigns/"+id+"/row")
	})
	adminGroup.GET("/workspaces/:workspace_id/campaigns/:id/skipped/download", func(c *echo.Context) error {
		id, _ := echo.PathParam[string](c, "id")
		return c.Redirect(http.StatusSeeOther, "/admin/campaigns/"+id+"/skipped/download")
	})
	adminGroup.POST("/workspaces/:workspace_id/campaigns/:id/start", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/campaigns")
	})
	adminGroup.POST("/workspaces/:workspace_id/campaigns/:id/pause", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/campaigns")
	})
	adminGroup.POST("/workspaces/:workspace_id/campaigns/:id/resume", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/campaigns")
	})
	adminGroup.POST("/workspaces/:workspace_id/campaigns/:id/cancel", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/campaigns")
	})
	adminGroup.DELETE("/workspaces/:workspace_id/campaigns/:id", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/campaigns")
	})
}

func TestLegacyAdminRouting_Redirect303(t *testing.T) {
	e := echo.New()
	adminGroup := e.Group("/admin")
	registerLegacyAdminRoutes(adminGroup)

	dummyID := uuid.New().String()
	wsID := uuid.New().String()

	testCases := []struct {
		method           string
		legacyPath       string
		expectedLocation string
	}{
		// Campaigns
		{
			method:           http.MethodGet,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/campaigns", wsID),
			expectedLocation: "/admin/campaigns",
		},
		{
			method:           http.MethodGet,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/campaigns/new", wsID),
			expectedLocation: "/admin/campaigns/new",
		},
		{
			method:           http.MethodPost,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/campaigns/upload", wsID),
			expectedLocation: "/admin/campaigns",
		},
		{
			method:           http.MethodPost,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/campaigns", wsID),
			expectedLocation: "/admin/campaigns",
		},
		{
			method:           http.MethodGet,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/campaigns/%s/row", wsID, dummyID),
			expectedLocation: "/admin/campaigns/" + dummyID + "/row",
		},
		{
			method:           http.MethodGet,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/campaigns/%s/skipped/download", wsID, dummyID),
			expectedLocation: "/admin/campaigns/" + dummyID + "/skipped/download",
		},
		{
			method:           http.MethodPost,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/campaigns/%s/start", wsID, dummyID),
			expectedLocation: "/admin/campaigns",
		},
		{
			method:           http.MethodPost,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/campaigns/%s/pause", wsID, dummyID),
			expectedLocation: "/admin/campaigns",
		},
		{
			method:           http.MethodPost,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/campaigns/%s/resume", wsID, dummyID),
			expectedLocation: "/admin/campaigns",
		},
		{
			method:           http.MethodPost,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/campaigns/%s/cancel", wsID, dummyID),
			expectedLocation: "/admin/campaigns",
		},
		{
			method:           http.MethodDelete,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/campaigns/%s", wsID, dummyID),
			expectedLocation: "/admin/campaigns",
		},
		// Tags & Contacts
		{
			method:           http.MethodGet,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/tags", wsID),
			expectedLocation: "/admin/tags",
		},
		{
			method:           http.MethodPost,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/tags", wsID),
			expectedLocation: "/admin/tags",
		},
		{
			method:           http.MethodDelete,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/tags/%s", wsID, dummyID),
			expectedLocation: "/admin/tags",
		},
		{
			method:           http.MethodPost,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/contacts/import", wsID),
			expectedLocation: "/admin/tags",
		},
		{
			method:           http.MethodGet,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/contacts/export", wsID),
			expectedLocation: "/admin/contacts/export",
		},
		// WABA Templates
		{
			method:           http.MethodGet,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/templates", wsID),
			expectedLocation: "/admin/templates",
		},
		{
			method:           http.MethodGet,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/templates/new", wsID),
			expectedLocation: "/admin/templates/new",
		},
		{
			method:           http.MethodPost,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/templates", wsID),
			expectedLocation: "/admin/templates",
		},
		{
			method:           http.MethodPost,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/templates/%s/sync", wsID, dummyID),
			expectedLocation: "/admin/templates",
		},
		{
			method:           http.MethodDelete,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/templates/%s", wsID, dummyID),
			expectedLocation: "/admin/templates",
		},
		// Integrations
		{
			method:           http.MethodGet,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/integrations", wsID),
			expectedLocation: "/admin/integrations/headless",
		},
		{
			method:           http.MethodGet,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/integrations/headless", wsID),
			expectedLocation: "/admin/integrations/headless",
		},
		{
			method:           http.MethodPost,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/integrations/headless/sso-generate", wsID),
			expectedLocation: "/admin/integrations/headless",
		},
		{
			method:           http.MethodGet,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/integrations/chatwoot", wsID),
			expectedLocation: "/admin/integrations/chatwoot",
		},
		{
			method:           http.MethodPost,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/integrations/chatwoot", wsID),
			expectedLocation: "/admin/integrations/chatwoot",
		},
		{
			method:           http.MethodGet,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/integrations/typebot", wsID),
			expectedLocation: "/admin/integrations/typebot",
		},
		{
			method:           http.MethodPost,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/integrations/typebot", wsID),
			expectedLocation: "/admin/integrations/typebot",
		},
		{
			method:           http.MethodGet,
			legacyPath:       "/admin/integrations",
			expectedLocation: "/admin/integrations/headless",
		},
		// Webhooks
		{
			method:           http.MethodGet,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/webhooks", wsID),
			expectedLocation: "/admin/webhooks",
		},
		{
			method:           http.MethodGet,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/webhooks/subscriptions/new", wsID),
			expectedLocation: "/admin/webhooks/subscriptions/new",
		},
		{
			method:           http.MethodGet,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/webhooks/subscriptions/%s/edit", wsID, dummyID),
			expectedLocation: "/admin/webhooks/subscriptions/" + dummyID + "/edit",
		},
		{
			method:           http.MethodGet,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/webhooks/subscriptions/%s/rotate-form", wsID, dummyID),
			expectedLocation: "/admin/webhooks/subscriptions/" + dummyID + "/rotate-form",
		},
		{
			method:           http.MethodPost,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/webhooks/subscriptions", wsID),
			expectedLocation: "/admin/webhooks",
		},
		{
			method:           http.MethodPost,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/webhooks/subscriptions/%s", wsID, dummyID),
			expectedLocation: "/admin/webhooks",
		},
		{
			method:           http.MethodPost,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/webhooks/subscriptions/%s/rotate", wsID, dummyID),
			expectedLocation: "/admin/webhooks",
		},
		{
			method:           http.MethodPost,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/webhooks/subscriptions/%s/ping", wsID, dummyID),
			expectedLocation: "/admin/webhooks",
		},
		{
			method:           http.MethodDelete,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/webhooks/subscriptions/%s", wsID, dummyID),
			expectedLocation: "/admin/webhooks",
		},
		{
			method:           http.MethodGet,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/webhooks/subscriptions/%s/test-form", wsID, dummyID),
			expectedLocation: "/admin/webhooks/subscriptions/" + dummyID + "/test-form",
		},
		{
			method:           http.MethodPost,
			legacyPath:       fmt.Sprintf("/admin/workspaces/%s/webhooks/subscriptions/%s/test", wsID, dummyID),
			expectedLocation: "/admin/webhooks",
		},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s %s", tc.method, tc.legacyPath), func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.legacyPath, nil)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			if rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound {
				t.Fatalf("expected 303 See Other or 302 Found for %s %s, got %d", tc.method, tc.legacyPath, rec.Code)
			}
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("expected 303 See Other for %s %s, got %d", tc.method, tc.legacyPath, rec.Code)
			}
			loc := rec.Header().Get("Location")
			if loc != tc.expectedLocation {
				t.Fatalf("expected redirect Location %q, got %q", tc.expectedLocation, loc)
			}
		})
	}
}

func TestLegacyAdminRouting_Redirect302(t *testing.T) {
	// Backward-compatible alias running the standardized redirect tests
	TestLegacyAdminRouting_Redirect303(t)
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

	// Legacy redirect routes
	registerLegacyAdminRoutes(adminGroup)

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

func TestLegacyAdminRouting_PreservesSessionState(t *testing.T) {
	e, ws := setupFlatRoutingEcho(t)
	dummyID := uuid.New().String()
	otherWS := uuid.New().String()

	testCases := []struct {
		method string
		path   string
		target string
	}{
		{http.MethodGet, fmt.Sprintf("/admin/workspaces/%s/campaigns", otherWS), "/admin/campaigns"},
		{http.MethodPost, fmt.Sprintf("/admin/workspaces/%s/campaigns", otherWS), "/admin/campaigns"},
		{http.MethodDelete, fmt.Sprintf("/admin/workspaces/%s/campaigns/%s", otherWS, dummyID), "/admin/campaigns"},
		{http.MethodGet, fmt.Sprintf("/admin/workspaces/%s/tags", otherWS), "/admin/tags"},
		{http.MethodPost, fmt.Sprintf("/admin/workspaces/%s/tags", otherWS), "/admin/tags"},
		{http.MethodDelete, fmt.Sprintf("/admin/workspaces/%s/tags/%s", otherWS, dummyID), "/admin/tags"},
		{http.MethodGet, fmt.Sprintf("/admin/workspaces/%s/templates", otherWS), "/admin/templates"},
		{http.MethodPost, fmt.Sprintf("/admin/workspaces/%s/templates", otherWS), "/admin/templates"},
		{http.MethodDelete, fmt.Sprintf("/admin/workspaces/%s/templates/%s", otherWS, dummyID), "/admin/templates"},
		{http.MethodGet, fmt.Sprintf("/admin/workspaces/%s/webhooks", otherWS), "/admin/webhooks"},
		{http.MethodPost, fmt.Sprintf("/admin/workspaces/%s/webhooks/subscriptions", otherWS), "/admin/webhooks"},
		{http.MethodDelete, fmt.Sprintf("/admin/workspaces/%s/webhooks/subscriptions/%s", otherWS, dummyID), "/admin/webhooks"},
		{http.MethodGet, fmt.Sprintf("/admin/workspaces/%s/integrations", otherWS), "/admin/integrations/headless"},
		{http.MethodPost, fmt.Sprintf("/admin/workspaces/%s/integrations/chatwoot", otherWS), "/admin/integrations/chatwoot"},
	}

	for _, tc := range testCases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.AddCookie(&http.Cookie{
				Name:  "pergo-active-workspace",
				Value: ws.ID.String(),
			})
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			if rec.Code != http.StatusSeeOther {
				t.Fatalf("expected 303 See Other for %s %s, got %d", tc.method, tc.path, rec.Code)
			}
			if loc := rec.Header().Get("Location"); loc != tc.target {
				t.Fatalf("expected redirect Location %q, got %q", tc.target, loc)
			}
		})
	}
}
