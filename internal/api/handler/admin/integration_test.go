package admin_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/handler/admin"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/pages"
)

func TestChatwootAdminHandler(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	wsRepo := repository.NewWorkspaceRepository(pool)

	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(i)
	}
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	integrationRepo := repository.NewIntegrationRepository(pool, enc)

	ws, err := wsRepo.Create(ctx, "chatwoot_admin_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	h := admin.NewChatwootAdminHandler(integrationRepo)

	t.Run("GetSettings_Empty", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/workspaces/%s/integrations/chatwoot", ws.ID), nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/admin/workspaces/:workspace_id/integrations/chatwoot")
		c.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: ws.ID.String()},
		})

		// Inject mock active workspace into request context to support sidebar template references
		reqCtx := context.WithValue(req.Context(), "active_workspace", ws)
		reqCtx = context.WithValue(reqCtx, "active_path", req.URL.Path)
		c.SetRequest(req.WithContext(reqCtx))

		err := h.GetSettings(c)
		if err != nil {
			t.Fatalf("GetSettings returned error: %v", err)
		}

		if rec.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d. Body: %s", http.StatusOK, rec.Code, rec.Body.String())
		}

		body := rec.Body.String()
		if !strings.Contains(body, `name="api_url"`) {
			t.Error("expected body to contain api_url field")
		}
		if !strings.Contains(body, `name="access_token"`) {
			t.Error("expected body to contain access_token field")
		}
	})

	t.Run("PostSettings_Success", func(t *testing.T) {
		e := echo.New()
		form := url.Values{}
		form.Set("api_url", "https://chatwoot.local")
		form.Set("access_token", "chatwoot-secret-access-token")
		form.Set("account_id", "12")
		form.Set("inbox_id", "34")
		form.Set("active", "true")

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/workspaces/%s/integrations/chatwoot", ws.ID), strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/admin/workspaces/:workspace_id/integrations/chatwoot")
		c.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: ws.ID.String()},
		})

		reqCtx := context.WithValue(req.Context(), "active_workspace", ws)
		reqCtx = context.WithValue(reqCtx, "active_path", req.URL.Path)
		c.SetRequest(req.WithContext(reqCtx))

		err := h.PostSettings(c)
		if err != nil {
			t.Fatalf("PostSettings returned error: %v", err)
		}

		if rec.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d. Body: %s", http.StatusOK, rec.Code, rec.Body.String())
		}

		// Retrieve from repo and check values
		saved, err := integrationRepo.GetByProvider(ctx, ws.ID, "chatwoot")
		if err != nil {
			t.Fatalf("failed to retrieve integration: %v", err)
		}

		if !saved.Active {
			t.Error("expected integration to be active")
		}

		var parsedConfig pages.ChatwootConfig
		err = json.Unmarshal(saved.Config, &parsedConfig)
		if err != nil {
			t.Fatalf("failed to unmarshal saved configuration: %v", err)
		}

		if parsedConfig.APIURL != "https://chatwoot.local" {
			t.Errorf("expected APIURL https://chatwoot.local, got %s", parsedConfig.APIURL)
		}
		if parsedConfig.AccessToken != "chatwoot-secret-access-token" {
			t.Errorf("expected AccessToken chatwoot-secret-access-token, got %s", parsedConfig.AccessToken)
		}
		if parsedConfig.AccountID != 12 {
			t.Errorf("expected AccountID 12, got %d", parsedConfig.AccountID)
		}
		if parsedConfig.InboxID != 34 {
			t.Errorf("expected InboxID 34, got %d", parsedConfig.InboxID)
		}
	})

	t.Run("GetSettings_WithConfig", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/workspaces/%s/integrations/chatwoot", ws.ID), nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/admin/workspaces/:workspace_id/integrations/chatwoot")
		c.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: ws.ID.String()},
		})

		reqCtx := context.WithValue(req.Context(), "active_workspace", ws)
		reqCtx = context.WithValue(reqCtx, "active_path", req.URL.Path)
		c.SetRequest(req.WithContext(reqCtx))

		err := h.GetSettings(c)
		if err != nil {
			t.Fatalf("GetSettings returned error: %v", err)
		}

		if rec.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d. Body: %s", http.StatusOK, rec.Code, rec.Body.String())
		}

		body := rec.Body.String()
		if !strings.Contains(body, `value="https://chatwoot.local"`) {
			t.Error("expected body to contain the configured api_url value")
		}
		if !strings.Contains(body, `value="12"`) {
			t.Error("expected body to contain the configured account_id value")
		}
	})

	t.Run("PostSettings_InvalidID", func(t *testing.T) {
		e := echo.New()
		form := url.Values{}
		form.Set("api_url", "https://chatwoot.local")
		form.Set("access_token", "chatwoot-secret-access-token")
		form.Set("account_id", "abc") // invalid
		form.Set("inbox_id", "34")
		form.Set("active", "true")

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/workspaces/%s/integrations/chatwoot", ws.ID), strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/admin/workspaces/:workspace_id/integrations/chatwoot")
		c.SetPathValues(echo.PathValues{
			{Name: "workspace_id", Value: ws.ID.String()},
		})

		reqCtx := context.WithValue(req.Context(), "active_workspace", ws)
		reqCtx = context.WithValue(reqCtx, "active_path", req.URL.Path)
		c.SetRequest(req.WithContext(reqCtx))

		err := h.PostSettings(c)
		if err != nil {
			t.Fatalf("PostSettings returned error: %v", err)
		}

		body := rec.Body.String()
		if !strings.Contains(body, "ID da Conta inválido") {
			t.Errorf("expected body to contain invalid account ID error message, got: %s", body)
		}
	})
}
