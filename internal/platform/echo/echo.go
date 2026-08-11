// Package echosrv provides the Echo v5 HTTP server constructor with
// standard middleware (slog, recover, request ID).
package echosrv

import (
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	apimiddleware "github.com/pablojhp.pergo/internal/api/middleware"
)

// New creates and returns a configured Echo v5 instance with:
//   - Request recovery middleware
//   - Unique request ID per request
//   - Security headers middleware
func New() *echo.Echo {
	e := echo.New()
	e.Use(middleware.Recover())
	e.Use(middleware.RequestID())
	e.Use(apimiddleware.SecurityHeaders())
	return e
}
