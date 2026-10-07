package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/handler/admin"
	"github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/repository"
)

type securityTestServer struct {
	e          *echo.Echo
	wsRepo     *repository.WorkspaceRepository
	apiKeyRepo *repository.APIKeyRepository
	masterKey  string
}

func setupWorkspaceSecurityTestServer(t *testing.T) *securityTestServer {
	t.Helper()
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("PostgreSQL not available, skipping security penetration test")
	}

	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		t.Fatalf("failed to create sql.DB: %v", err)
	}
	_ = postgres.RunMigrations(db)
	db.Close()

	encryptor, err := crypto.NewEncryptor(make([]byte, 32))
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	wsRepo := repository.NewWorkspaceRepository(pool)
	apiKeyRepo := repository.NewAPIKeyRepository(pool)
	tagRepo := repository.NewTagRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	campRepo := repository.NewCampaignRepository(pool)
	wabaRepo := repository.NewWABATemplateRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, encryptor)

	campHandler := admin.NewCampaignHandler(campRepo, wabaRepo, connRepo, tagRepo, nil)
	tagHandler := admin.NewTagAdminHandler(tagRepo, contactRepo, wsRepo)
	wsHandler := &admin.WorkspaceHandler{Repo: wsRepo, APIKeys: apiKeyRepo}

	masterKey := "master-security-test-secret-key-32-chars!!"

	e := echo.New()
	e.Use(middleware.TraceMiddleware())
	e.Use(middleware.AuthMiddleware(apiKeyRepo, masterKey))

	v1Group := e.Group("/api/v1")
	v1Group.Use(middleware.EnforceWorkspaceScope())

	// Scoped routes
	v1Group.POST("/workspaces/:workspace_id/campaigns", campHandler.APICreate)
	v1Group.GET("/workspaces/:workspace_id/campaigns", campHandler.APIList)
	v1Group.GET("/workspaces/:workspace_id/tags", tagHandler.ListTags)
	v1Group.POST("/workspaces/:workspace_id/tags", tagHandler.CreateTag)
	v1Group.POST("/workspaces/:workspace_id/contacts/import", tagHandler.ImportContactsCSV)
	v1Group.POST("/workspaces/:workspace_id/webhook-secret", wsHandler.GenerateWebhookSecret)

	// Flat routes
	v1Group.POST("/campaigns", campHandler.APICreate)
	v1Group.GET("/campaigns", campHandler.APIList)
	v1Group.GET("/tags", tagHandler.ListTags)
	v1Group.POST("/tags", tagHandler.CreateTag)
	v1Group.POST("/contacts/import", tagHandler.ImportContactsCSV)
	v1Group.POST("/workspaces/webhook-secret", wsHandler.GenerateWebhookSecret)

	return &securityTestServer{
		e:          e,
		wsRepo:     wsRepo,
		apiKeyRepo: apiKeyRepo,
		masterKey:  masterKey,
	}
}

// TestWorkspaceSecurity_CrossTenantPenetration verifies that a tenant with an API key
// for Workspace A cannot access any endpoint under Workspace B's path prefix.
// Any cross-tenant attempt must be blocked by EnforceWorkspaceScope with HTTP 403 WORKSPACE_ID_MISMATCH.
func TestWorkspaceSecurity_CrossTenantPenetration(t *testing.T) {
	srv := setupWorkspaceSecurityTestServer(t)
	ctx := context.Background()

	// 1. Create Workspace A and Workspace B
	wsA, err := srv.wsRepo.Create(ctx, "sec_ws_a_"+uuid.New().String()[:8])
	if err != nil {
		t.Fatalf("failed to create Workspace A: %v", err)
	}
	defer func() { _ = srv.wsRepo.Delete(ctx, wsA.ID) }()

	wsB, err := srv.wsRepo.Create(ctx, "sec_ws_b_"+uuid.New().String()[:8])
	if err != nil {
		t.Fatalf("failed to create Workspace B: %v", err)
	}
	defer func() { _ = srv.wsRepo.Delete(ctx, wsB.ID) }()

	// 2. Create API key for Workspace A
	_, apiKeyA, err := srv.apiKeyRepo.Create(ctx, wsA.ID, "key-tenant-a")
	if err != nil {
		t.Fatalf("failed to create API key for Workspace A: %v", err)
	}

	// 3. Define target penetration routes on Workspace B called by Tenant A
	testCases := []struct {
		name        string
		method      string
		targetURL   string
		contentType string
		body        io.Reader
	}{
		{
			name:        "POST /api/v1/workspaces/:workspace_id/campaigns",
			method:      http.MethodPost,
			targetURL:   fmt.Sprintf("/api/v1/workspaces/%s/campaigns", wsB.ID),
			contentType: "application/json",
			body:        strings.NewReader(`{"name":"cross-tenant-campaign","channel":"whatsapp","recipient_phones":["+5511999999999"]}`),
		},
		{
			name:        "GET /api/v1/workspaces/:workspace_id/campaigns",
			method:      http.MethodGet,
			targetURL:   fmt.Sprintf("/api/v1/workspaces/%s/campaigns", wsB.ID),
			contentType: "",
			body:        nil,
		},
		{
			name:        "GET /api/v1/workspaces/:workspace_id/tags",
			method:      http.MethodGet,
			targetURL:   fmt.Sprintf("/api/v1/workspaces/%s/tags", wsB.ID),
			contentType: "",
			body:        nil,
		},
		{
			name:        "POST /api/v1/workspaces/:workspace_id/tags",
			method:      http.MethodPost,
			targetURL:   fmt.Sprintf("/api/v1/workspaces/%s/tags", wsB.ID),
			contentType: "application/json",
			body:        strings.NewReader(`{"name":"cross-tenant-tag"}`),
		},
		{
			name:        "POST /api/v1/workspaces/:workspace_id/contacts/import",
			method:      http.MethodPost,
			targetURL:   fmt.Sprintf("/api/v1/workspaces/%s/contacts/import", wsB.ID),
			contentType: "multipart/form-data; boundary=---boundary",
			body:        strings.NewReader("---boundary\r\nContent-Disposition: form-data; name=\"file\"; filename=\"c.csv\"\r\n\r\nphone,name\r\n5511999999999,Target\r\n---boundary--\r\n"),
		},
		{
			name:        "POST /api/v1/workspaces/:workspace_id/webhook-secret",
			method:      http.MethodPost,
			targetURL:   fmt.Sprintf("/api/v1/workspaces/%s/webhook-secret", wsB.ID),
			contentType: "application/json",
			body:        strings.NewReader(`{"webhook_secret":"attacker-controlled-secret"}`),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.targetURL, tc.body)
			req.Header.Set("Authorization", "Bearer "+apiKeyA)
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}

			rec := httptest.NewRecorder()
			srv.e.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("expected HTTP 403 Forbidden for cross-tenant request, got %d: %s", rec.Code, rec.Body.String())
			}

			var resp map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode JSON error response: %v, raw: %s", err, rec.Body.String())
			}

			if resp["code"] != "WORKSPACE_ID_MISMATCH" {
				t.Errorf("expected error code %q, got %q", "WORKSPACE_ID_MISMATCH", resp["code"])
			}
			if resp["message"] != "authenticated workspace does not match URL parameter" {
				t.Errorf("expected error message %q, got %q", "authenticated workspace does not match URL parameter", resp["message"])
			}
		})
	}

	// Legitimate access to own workspace must NOT be blocked with 403 WORKSPACE_ID_MISMATCH
	t.Run("Legitimate Workspace A access to own scoped tags", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%s/tags", wsA.ID), nil)
		req.Header.Set("Authorization", "Bearer "+apiKeyA)

		rec := httptest.NewRecorder()
		srv.e.ServeHTTP(rec, req)

		if rec.Code == http.StatusForbidden {
			t.Fatalf("expected legitimate access to succeed, but got 403: %s", rec.Body.String())
		}
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// TestWorkspaceSecurity_MasterKeyOperatorScope verifies that requests authenticated
// via Master Key on flat routes require an explicit target workspace (via X-Workspace-ID or workspace_id query param).
// In the absence of a target workspace, EnforceWorkspaceScope must return HTTP 400 WORKSPACE_REQUIRED.
func TestWorkspaceSecurity_MasterKeyOperatorScope(t *testing.T) {
	srv := setupWorkspaceSecurityTestServer(t)
	ctx := context.Background()

	ws, err := srv.wsRepo.Create(ctx, "sec_ws_op_"+uuid.New().String()[:8])
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = srv.wsRepo.Delete(ctx, ws.ID) }()

	flatRoutes := []struct {
		name        string
		method      string
		targetURL   string
		contentType string
		body        string
	}{
		{
			name:        "GET /api/v1/campaigns",
			method:      http.MethodGet,
			targetURL:   "/api/v1/campaigns",
			contentType: "",
		},
		{
			name:        "POST /api/v1/campaigns",
			method:      http.MethodPost,
			targetURL:   "/api/v1/campaigns",
			contentType: "application/json",
			body:        `{"name":"flat-camp"}`,
		},
		{
			name:        "GET /api/v1/tags",
			method:      http.MethodGet,
			targetURL:   "/api/v1/tags",
			contentType: "",
		},
		{
			name:        "POST /api/v1/tags",
			method:      http.MethodPost,
			targetURL:   "/api/v1/tags",
			contentType: "application/json",
			body:        `{"name":"flat-tag"}`,
		},
		{
			name:        "POST /api/v1/contacts/import",
			method:      http.MethodPost,
			targetURL:   "/api/v1/contacts/import",
			contentType: "multipart/form-data; boundary=---boundary",
			body:        "---boundary\r\nContent-Disposition: form-data; name=\"file\"; filename=\"c.csv\"\r\n\r\nphone\r\n5511999999999\r\n---boundary--\r\n",
		},
		{
			name:        "POST /api/v1/workspaces/webhook-secret",
			method:      http.MethodPost,
			targetURL:   "/api/v1/workspaces/webhook-secret",
			contentType: "application/json",
			body:        `{}`,
		},
	}

	for _, tc := range flatRoutes {
		t.Run(tc.name+" without X-Workspace-ID returns 400 WORKSPACE_REQUIRED", func(t *testing.T) {
			var bodyReader io.Reader
			if tc.body != "" {
				bodyReader = strings.NewReader(tc.body)
			}
			req := httptest.NewRequest(tc.method, tc.targetURL, bodyReader)
			req.Header.Set("Authorization", "Bearer "+srv.masterKey)
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}

			rec := httptest.NewRecorder()
			srv.e.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected HTTP 400 Bad Request, got %d: %s", rec.Code, rec.Body.String())
			}

			var resp map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode JSON error response: %v, raw: %s", err, rec.Body.String())
			}

			if resp["code"] != "WORKSPACE_REQUIRED" {
				t.Errorf("expected error code %q, got %q", "WORKSPACE_REQUIRED", resp["code"])
			}
			if resp["message"] != "workspace target must be specified via header or URL for operator scope" {
				t.Errorf("expected message %q, got %q", "workspace target must be specified via header or URL for operator scope", resp["message"])
			}
		})

		t.Run(tc.name+" with X-Workspace-ID header succeeds", func(t *testing.T) {
			var bodyReader io.Reader
			if tc.body != "" {
				bodyReader = strings.NewReader(tc.body)
			}
			req := httptest.NewRequest(tc.method, tc.targetURL, bodyReader)
			req.Header.Set("Authorization", "Bearer "+srv.masterKey)
			req.Header.Set("X-Workspace-ID", ws.ID.String())
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}

			rec := httptest.NewRecorder()
			srv.e.ServeHTTP(rec, req)

			// Should pass EnforceWorkspaceScope without 400 WORKSPACE_REQUIRED or 403 MISMATCH
			if rec.Code == http.StatusBadRequest {
				var errResp map[string]string
				_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
				if errResp["code"] == "WORKSPACE_REQUIRED" {
					t.Fatalf("unexpected WORKSPACE_REQUIRED with X-Workspace-ID provided: %s", rec.Body.String())
				}
			}
			if rec.Code == http.StatusForbidden {
				t.Fatalf("unexpected 403 Forbidden with valid master key and target: %s", rec.Body.String())
			}
		})
	}

	t.Run("Master Key accessing scoped route with URL param binds target", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%s/tags", ws.ID), nil)
		req.Header.Set("Authorization", "Bearer "+srv.masterKey)

		rec := httptest.NewRecorder()
		srv.e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for master key accessing scoped route, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}
