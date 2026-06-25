// Package middleware provides Echo v5 middleware functions for the OmniGo API.
package middleware

import (
	"github.com/labstack/echo/v5"
)

// SessionAuthMiddleware returns an Echo middleware that checks for an
// authenticated session and redirects to /admin/login if not authenticated.
// STUB — full implementation in Task 2.
func SessionAuthMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			// Stub: pass through without session check — tests will fail
			return next(c)
		}
	}
}
