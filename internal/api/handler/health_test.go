package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/api/handler"
	"github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/stretchr/testify/assert"
)

type mockNATSConn struct {
	err error
}

func (m *mockNATSConn) Ping() error {
	return m.err
}

func TestHealthHandler_DiscoveryLinks(t *testing.T) {
	e := echo.New()
	h := &handler.HealthHandler{
		Pool: nil,
		NATS: nil,
	}
	h.RegisterRoutes(e)

	t.Run("GET /healthz includes RFC 8288 Link header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, middleware.DiscoveryLinkHeaderValue, rec.Header().Get("Link"))
	})

	t.Run("GET /readyz includes RFC 8288 Link header even when unready", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Equal(t, middleware.DiscoveryLinkHeaderValue, rec.Header().Get("Link"))
	})
}
