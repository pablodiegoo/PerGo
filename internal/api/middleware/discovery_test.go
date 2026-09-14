package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/api/middleware"
)

func TestAgentDiscoveryMiddleware(t *testing.T) {
	e := echo.New()

	dummyHandler := func(c *echo.Context) error {
		return c.String(http.StatusOK, "ok")
	}

	e.GET("/with-discovery", dummyHandler, middleware.AgentDiscoveryMiddleware())
	e.GET("/without-discovery", dummyHandler)

	t.Run("Injects Link header on route with AgentDiscoveryMiddleware", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/with-discovery", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
		}
		if got := rec.Header().Get("Link"); got != middleware.DiscoveryLinkHeaderValue {
			t.Errorf("expected Link header %q, got %q", middleware.DiscoveryLinkHeaderValue, got)
		}
	})

	t.Run("Does not inject Link header on route without AgentDiscoveryMiddleware", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/without-discovery", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
		}
		if got := rec.Header().Get("Link"); got != "" {
			t.Errorf("expected empty Link header, got %q", got)
		}
	})
}
