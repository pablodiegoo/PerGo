package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/config"
)

func TestMasterAuthMiddleware(t *testing.T) {
	const (
		masterKey = "secret-master-key-12345"
		adminPass = "fallback-admin-pass-67890"
		wrongKey  = "wrong-key-00000"
	)

	dummyHandler := func(c *echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	t.Run("valid key with Bearer Authorization header", func(t *testing.T) {
		e := echo.New()
		e.POST("/api/v1/workspaces", dummyHandler, MasterAuthMiddleware(masterKey, adminPass))

		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", nil)
		req.Header.Set("Authorization", "Bearer "+masterKey)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("valid key with raw Authorization header", func(t *testing.T) {
		e := echo.New()
		e.POST("/api/v1/workspaces", dummyHandler, MasterAuthMiddleware(masterKey, adminPass))

		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", nil)
		req.Header.Set("Authorization", masterKey)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("valid key with X-Master-Key header", func(t *testing.T) {
		e := echo.New()
		e.POST("/api/v1/workspaces", dummyHandler, MasterAuthMiddleware(masterKey, adminPass))

		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", nil)
		req.Header.Set("X-Master-Key", masterKey)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("valid key with master_key query param", func(t *testing.T) {
		e := echo.New()
		e.POST("/api/v1/workspaces", dummyHandler, MasterAuthMiddleware(masterKey, adminPass))

		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces?master_key="+masterKey, nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("valid key with token query param", func(t *testing.T) {
		e := echo.New()
		e.POST("/api/v1/workspaces", dummyHandler, MasterAuthMiddleware(masterKey, adminPass))

		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces?token="+masterKey, nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("valid key with api_key query param", func(t *testing.T) {
		e := echo.New()
		e.POST("/api/v1/workspaces", dummyHandler, MasterAuthMiddleware(masterKey, adminPass))

		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces?api_key="+masterKey, nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("invalid key returns 401 Unauthorized with standard payload", func(t *testing.T) {
		e := echo.New()
		e.POST("/api/v1/workspaces", dummyHandler, MasterAuthMiddleware(masterKey, adminPass))

		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", nil)
		req.Header.Set("Authorization", "Bearer "+wrongKey)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got %d", rec.Code)
		}

		var resp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode JSON response: %v", err)
		}

		if resp["code"] != "unauthorized" || resp["message"] != "invalid or missing master key" {
			t.Fatalf("unexpected response payload: %+v", resp)
		}
	})

	t.Run("missing credentials returns 401 Unauthorized with standard payload", func(t *testing.T) {
		e := echo.New()
		e.POST("/api/v1/workspaces", dummyHandler, MasterAuthMiddleware(masterKey, adminPass))

		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got %d", rec.Code)
		}

		var resp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode JSON response: %v", err)
		}

		if resp["code"] != "unauthorized" || resp["message"] != "invalid or missing master key" {
			t.Fatalf("unexpected response payload: %+v", resp)
		}
	})

	t.Run("fallback to admin password when masterKey is empty", func(t *testing.T) {
		e := echo.New()
		e.POST("/api/v1/workspaces", dummyHandler, MasterAuthMiddleware("", adminPass))

		// Valid admin password
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", nil)
		req.Header.Set("Authorization", "Bearer "+adminPass)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK with fallback admin password, got %d", rec.Code)
		}

		// Wrong password
		reqWrong := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", nil)
		reqWrong.Header.Set("Authorization", "Bearer "+wrongKey)
		recWrong := httptest.NewRecorder()
		e.ServeHTTP(recWrong, reqWrong)

		if recWrong.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized with wrong fallback password, got %d", recWrong.Code)
		}
	})

	t.Run("rejects when both masterKey and fallbackPassword are empty", func(t *testing.T) {
		e := echo.New()
		e.POST("/api/v1/workspaces", dummyHandler, MasterAuthMiddleware("", ""))

		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", nil)
		req.Header.Set("Authorization", "Bearer some-key")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized when config is empty, got %d", rec.Code)
		}
	})

	t.Run("MasterAuth helper with config and nil config", func(t *testing.T) {
		cfg := &config.Config{
			MasterKey:     masterKey,
			AdminPassword: adminPass,
		}

		e := echo.New()
		e.POST("/api/v1/workspaces", dummyHandler, MasterAuth(cfg))

		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", nil)
		req.Header.Set("Authorization", "Bearer "+masterKey)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK with MasterAuth(cfg), got %d", rec.Code)
		}

		// Nil config
		eNil := echo.New()
		eNil.POST("/api/v1/workspaces", dummyHandler, MasterAuth(nil))

		reqNil := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", nil)
		reqNil.Header.Set("Authorization", "Bearer "+masterKey)
		recNil := httptest.NewRecorder()
		eNil.ServeHTTP(recNil, reqNil)

		if recNil.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized with MasterAuth(nil), got %d", recNil.Code)
		}
	})
}
