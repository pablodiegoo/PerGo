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
	typebot "github.com/pablojhp.pergo/internal/integration/typebot"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/repository"
)

func TestTypebotSettingsHandler(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	wsRepo := repository.NewWorkspaceRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, nil)

	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(i)
	}
	enc, err := crypto.NewEncryptor(kek)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}
	integrationRepo := repository.NewIntegrationRepository(pool, enc)

	ws, err := wsRepo.Create(ctx, "typebot_admin_test_ws_"+uuid.New().String())
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	// Create a mock connection for select tests
	mockConn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    ws.ID,
		Name:           "Test Connection",
		Channel:        "telegram",
		SenderIdentity: "test-sender",
		Status:         "active",
	}
	// We insert the connection using simple SQL since we don't have connection save without encryptor/provider, or we can use connRepo
	// Wait, let's see how connRepo saves connection, or let's use SQL directly to avoid any crypto setup issues.
	// Let's insert via SQL.
	_, err = pool.Exec(ctx, `
		INSERT INTO connections (id, workspace_id, name, channel, sender_identity, status, is_default, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
	`, mockConn.ID, mockConn.WorkspaceID, mockConn.Name, mockConn.Channel, mockConn.SenderIdentity, mockConn.Status, mockConn.IsDefault)
	if err != nil {
		t.Fatalf("failed to insert mock connection: %v", err)
	}

	h := admin.NewTypebotSettingsHandler(integrationRepo, connRepo)

	t.Run("GetSettings_RendersEmptyFormWhenNoIntegration", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/workspaces/%s/integrations/typebot", ws.ID), nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/admin/workspaces/:workspace_id/integrations/typebot")
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
			t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
		}

		body := rec.Body.String()
		if !strings.Contains(body, `name="api_url"`) {
			t.Error("expected body to contain api_url field")
		}
		if !strings.Contains(body, `name="bot_id"`) {
			t.Error("expected body to contain bot_id field")
		}
	})

	t.Run("PostSettings_ValidatesRequiredFields", func(t *testing.T) {
		e := echo.New()
		form := url.Values{}
		form.Set("api_url", "https://typebot.local")
		form.Set("bot_id", "my-bot")
		form.Set("bot_public_token", "") // missing
		form.Set("connection_id", mockConn.ID.String())

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/workspaces/%s/integrations/typebot", ws.ID), strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/admin/workspaces/:workspace_id/integrations/typebot")
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
			t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
		}

		body := rec.Body.String()
		if !strings.Contains(body, "API URL, Bot ID, Bot Public Token, and Connection are required") {
			t.Errorf("expected missing fields validation error, got: %s", body)
		}
	})

	t.Run("PostSettings_RejectsInvalidConnectionID", func(t *testing.T) {
		e := echo.New()
		form := url.Values{}
		form.Set("api_url", "https://typebot.local")
		form.Set("bot_id", "my-bot")
		form.Set("bot_public_token", "token123")
		form.Set("connection_id", "not-a-uuid") // invalid

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/workspaces/%s/integrations/typebot", ws.ID), strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/admin/workspaces/:workspace_id/integrations/typebot")
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
			t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
		}

		body := rec.Body.String()
		if !strings.Contains(body, "Invalid Connection ID") {
			t.Errorf("expected invalid connection ID error, got: %s", body)
		}
	})

	t.Run("PostSettings_PersistsTypebotConfig_AndBuildsPerBotShape", func(t *testing.T) {
		e := echo.New()
		form := url.Values{}
		form.Set("api_url", "https://typebot.local")
		form.Set("bot_id", "my-bot")
		form.Set("bot_public_token", "token123")
		form.Set("connection_id", mockConn.ID.String())
		form.Set("trigger_keywords", "sales,help")
		form.Set("is_default", "true")
		form.Set("active", "true")

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/workspaces/%s/integrations/typebot", ws.ID), strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/admin/workspaces/:workspace_id/integrations/typebot")
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
			t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
		}

		// Retrieve from repo and check values
		saved, err := integrationRepo.GetByProvider(ctx, ws.ID, "typebot")
		if err != nil {
			t.Fatalf("failed to retrieve integration: %v", err)
		}

		if !saved.Active {
			t.Error("expected integration to be active")
		}

		var parsedConfig typebot.Config
		err = json.Unmarshal(saved.Config, &parsedConfig)
		if err != nil {
			t.Fatalf("failed to unmarshal saved configuration: %v", err)
		}

		if parsedConfig.APIURL != "https://typebot.local" {
			t.Errorf("expected APIURL https://typebot.local, got %s", parsedConfig.APIURL)
		}
		if len(parsedConfig.Bots) != 1 {
			t.Fatalf("expected 1 bot, got %d", len(parsedConfig.Bots))
		}

		bot := parsedConfig.Bots[0]
		if bot.BotID != "my-bot" {
			t.Errorf("expected BotID 'my-bot', got '%s'", bot.BotID)
		}
		if bot.PublicToken != "token123" {
			t.Errorf("expected PublicToken 'token123', got '%s'", bot.PublicToken)
		}
		if bot.ConnectionID != mockConn.ID.String() {
			t.Errorf("expected ConnectionID '%s', got '%s'", mockConn.ID.String(), bot.ConnectionID)
		}
		if len(bot.TriggerWords) != 2 || bot.TriggerWords[0] != "sales" || bot.TriggerWords[1] != "help" {
			t.Errorf("expected TriggerWords ['sales', 'help'], got %v", bot.TriggerWords)
		}
		if !bot.IsDefault {
			t.Error("expected IsDefault to be true")
		}
	})

	t.Run("GetSettings_LoadsStoredTypebotConfig", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/workspaces/%s/integrations/typebot", ws.ID), nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/admin/workspaces/:workspace_id/integrations/typebot")
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
			t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
		}

		body := rec.Body.String()
		if !strings.Contains(body, `value="https://typebot.local"`) {
			t.Error("expected body to contain the configured api_url value")
		}
		if !strings.Contains(body, `value="my-bot"`) {
			t.Error("expected body to contain the configured bot_id value")
		}
		if !strings.Contains(body, `value="token123"`) {
			t.Error("expected body to contain the configured bot_public_token value")
		}
		if !strings.Contains(body, `value="sales,help"`) {
			t.Error("expected body to contain trigger_keywords value")
		}
	})
}
