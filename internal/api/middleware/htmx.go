package middleware

import (
	"github.com/labstack/echo/v5"
)

// HTMXMiddleware returns an Echo middleware that detects HTMX requests
// via the HX-Request header.
// STUB — full implementation in Task 2.
func HTMXMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			// Stub: pass through — tests will fail
			return next(c)
		}
	}
}

// IsHTMX checks if the request is from HTMX (fragment request).
func IsHTMX(c *echo.Context) bool {
	return c.Request().Header.Get("HX-Request") == "true"
}
