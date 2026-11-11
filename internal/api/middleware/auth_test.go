package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthMiddleware_MCPProtection(t *testing.T) {
	const masterKey = "test-master-secret-key-123456"

	newServer := func() *echo.Echo {
		e := echo.New()
		e.Use(middleware.AuthMiddleware(nil, masterKey))
		e.GET("/api/mcp/sse", func(c *echo.Context) error {
			return c.String(http.StatusOK, "sse-connected")
		})
		e.GET("/healthz", func(c *echo.Context) error {
			return c.String(http.StatusOK, "ok")
		})
		return e
	}

	t.Run("unauthenticated /api/mcp/sse returns 401", func(t *testing.T) {
		e := newServer()
		req := httptest.NewRequest(http.MethodGet, "/api/mcp/sse", nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.Contains(t, rec.Body.String(), "UNAUTHORIZED")
	})

	t.Run("authenticated /api/mcp/sse with master key returns 200", func(t *testing.T) {
		e := newServer()
		req := httptest.NewRequest(http.MethodGet, "/api/mcp/sse", nil)
		req.Header.Set("Authorization", "Bearer "+masterKey)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "sse-connected", rec.Body.String())
	})

	t.Run("/healthz remains accessible without API key", func(t *testing.T) {
		e := newServer()
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})
}
