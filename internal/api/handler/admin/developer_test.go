package admin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/api/handler/admin"
	"github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupDeveloperTest(t *testing.T) (context.Context, *repository.WorkspaceRepository, *repository.APIKeyRepository, *repository.Workspace, func()) {
	t.Helper()
	dbURL := testDBURL
	if dbURL == "" {
		dbURL = os.Getenv("PERGO_DATABASE_URL")
	}
	if dbURL == "" {
		t.Skip("testcontainers postgres not available")
	}

	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, dbURL)
	require.NoError(t, err)

	wsRepo := repository.NewWorkspaceRepository(pool)
	apiKeyRepo := repository.NewAPIKeyRepository(pool)

	ws, err := wsRepo.Create(ctx, "dev_portal_ws_"+uuid.New().String()[:8])
	require.NoError(t, err)

	cleanup := func() {
		_ = wsRepo.Delete(ctx, ws.ID)
		pool.Close()
	}

	return ctx, wsRepo, apiKeyRepo, ws, cleanup
}

func TestDeveloperHandler_GetPortal(t *testing.T) {
	ctx, wsRepo, apiKeyRepo, ws, cleanup := setupDeveloperTest(t)
	defer cleanup()

	handler := admin.NewDeveloperHandler(wsRepo, apiKeyRepo, []byte("dev-secret-32-bytes-test-key123"), "http://localhost:8080")

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/developers", nil)
	req = req.WithContext(tenant.WithWorkspaceID(ctx, ws.ID))
	req = req.WithContext(middleware.WithActiveWorkspace(req.Context(), ws))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.GetPortal(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, ws.Name)
	assert.Contains(t, body, ws.ID.String())
	assert.Contains(t, body, "Sandbox")
	assert.Contains(t, body, "Scalar Docs")
}

func TestDeveloperHandler_RotateWebhookSecret(t *testing.T) {
	ctx, wsRepo, apiKeyRepo, ws, cleanup := setupDeveloperTest(t)
	defer cleanup()

	handler := admin.NewDeveloperHandler(wsRepo, apiKeyRepo, []byte("dev-secret-32-bytes-test-key123"), "http://localhost:8080")

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/admin/developers/webhook-secret/rotate", nil)
	req = req.WithContext(tenant.WithWorkspaceID(ctx, ws.ID))
	req = req.WithContext(middleware.WithActiveWorkspace(req.Context(), ws))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.RotateWebhookSecret(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Verify in database that secret was updated
	updatedWS, err := wsRepo.GetByID(ctx, ws.ID)
	require.NoError(t, err)
	require.NotNil(t, updatedWS.WebhookSecret)
	assert.NotEmpty(t, *updatedWS.WebhookSecret)
}

func TestDeveloperHandler_APIKeyLifecycle(t *testing.T) {
	ctx, wsRepo, apiKeyRepo, ws, cleanup := setupDeveloperTest(t)
	defer cleanup()

	handler := admin.NewDeveloperHandler(wsRepo, apiKeyRepo, []byte("dev-secret-32-bytes-test-key123"), "http://localhost:8080")
	e := echo.New()

	// 1. Create API Key
	form := url.Values{}
	form.Set("name", "Backend CRM Test Key")
	req := httptest.NewRequest(http.MethodPost, "/admin/developers/keys", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(tenant.WithWorkspaceID(ctx, ws.ID))
	req = req.WithContext(middleware.WithActiveWorkspace(req.Context(), ws))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.CreateAPIKey(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Backend CRM Test Key")

	// Verify key in db
	keys, err := apiKeyRepo.ListByWorkspace(ctx, ws.ID)
	require.NoError(t, err)
	require.Len(t, keys, 1)
	keyID := keys[0].ID

	// 2. Revoke API Key
	reqRevoke := httptest.NewRequest(http.MethodDelete, "/admin/developers/keys/"+keyID.String(), nil)
	reqRevoke = reqRevoke.WithContext(tenant.WithWorkspaceID(ctx, ws.ID))
	reqRevoke = reqRevoke.WithContext(middleware.WithActiveWorkspace(reqRevoke.Context(), ws))
	recRevoke := httptest.NewRecorder()
	cRevoke := e.NewContext(reqRevoke, recRevoke)
	cRevoke.SetPath("/admin/developers/keys/:key_id")
	cRevoke.SetPathValues(echo.PathValues{{Name: "key_id", Value: keyID.String()}})

	err = handler.RevokeAPIKey(cRevoke)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, recRevoke.Code)

	keysAfter, err := apiKeyRepo.ListByWorkspace(ctx, ws.ID)
	require.NoError(t, err)
	require.Len(t, keysAfter, 1)
	assert.NotNil(t, keysAfter[0].RevokedAt, "API key must be marked as revoked")
}

func TestDeveloperHandler_SandboxTest_ValidInteractiveButton(t *testing.T) {
	ctx, wsRepo, apiKeyRepo, ws, cleanup := setupDeveloperTest(t)
	defer cleanup()

	handler := admin.NewDeveloperHandler(wsRepo, apiKeyRepo, []byte("dev-secret-32-bytes-test-key123"), "http://localhost:8080")
	e := echo.New()

	payloadJSON := `{
		"to": "+5511999999999",
		"channel": "whatsapp_cloud",
		"type": "interactive",
		"fallback_behavior": "degrade",
		"interactive": {
			"type": "button",
			"body": {
				"text": "Selecione uma opção:"
			},
			"action": {
				"buttons": [
					{
						"type": "reply",
						"reply": {
							"id": "btn_1",
							"title": "Opção 1"
						}
					}
				]
			}
		}
	}`

	form := url.Values{}
	form.Set("payload", payloadJSON)
	req := httptest.NewRequest(http.MethodPost, "/admin/developers/sandbox/test", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(tenant.WithWorkspaceID(ctx, ws.ID))
	req = req.WithContext(middleware.WithActiveWorkspace(req.Context(), ws))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.SandboxTest(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Válido")
	assert.Contains(t, body, "Opção 1")
	assert.Contains(t, body, "whatsapp_cloud")
}

func TestDeveloperHandler_SandboxTest_InvalidPayload(t *testing.T) {
	ctx, wsRepo, apiKeyRepo, ws, cleanup := setupDeveloperTest(t)
	defer cleanup()

	handler := admin.NewDeveloperHandler(wsRepo, apiKeyRepo, []byte("dev-secret-32-bytes-test-key123"), "http://localhost:8080")
	e := echo.New()

	// Missing 'to' and missing 'channel'
	payloadJSON := `{
		"body": "Teste sem destinatário nem canal"
	}`

	form := url.Values{}
	form.Set("payload", payloadJSON)
	req := httptest.NewRequest(http.MethodPost, "/admin/developers/sandbox/test", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(tenant.WithWorkspaceID(ctx, ws.ID))
	req = req.WithContext(middleware.WithActiveWorkspace(req.Context(), ws))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.SandboxTest(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Inválido")
	assert.Contains(t, body, "to")
	assert.Contains(t, body, "channel")
}
