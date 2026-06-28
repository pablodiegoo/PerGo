// Package handler provides HTTP handler constructors for the PerGo API.
package handler

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"
)

// NATSConn is the interface the health handler uses to ping NATS.
type NATSConn interface {
	Ping() error
}

// HealthHandler holds dependencies for health and readiness endpoints.
type HealthHandler struct {
	Pool *pgxpool.Pool
	NATS NATSConn
}

// RegisterRoutes wires the health endpoints onto the Echo router.
func (h *HealthHandler) RegisterRoutes(e *echo.Echo) {
	e.GET("/healthz", h.Healthz)
	e.GET("/readyz", h.Readyz)
}

// Healthz returns 200 always (liveness probe).
func (h *HealthHandler) Healthz(c *echo.Context) error {
	return c.String(http.StatusOK, "ok")
}

// Readyz returns 200 only when pgx and NATS are both reachable.
func (h *HealthHandler) Readyz(c *echo.Context) error {
	if h.Pool == nil || h.NATS == nil {
		return c.String(http.StatusServiceUnavailable, "not initialized")
	}
	if err := h.Pool.Ping(c.Request().Context()); err != nil {
		return c.String(http.StatusServiceUnavailable, "pgx not ready")
	}
	if err := h.NATS.Ping(); err != nil {
		return c.String(http.StatusServiceUnavailable, "nats not ready")
	}
	return c.String(http.StatusOK, "ok")
}
