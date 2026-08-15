// Package middleware provides Echo v5 middleware functions for the PerGo API.
package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/config"
)

// MasterAuthMiddleware returns an Echo middleware that enforces master authentication.
// It verifies the master key provided in the Authorization header (Bearer or raw),
// the X-Master-Key header, or the query parameters against the configured masterKey.
// If masterKey is empty, it falls back securely to fallbackPassword.
// All comparisons are performed in constant time using crypto/subtle.ConstantTimeCompare.
func MasterAuthMiddleware(masterKey, fallbackPassword string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			expected := masterKey
			if expected == "" {
				expected = fallbackPassword
			}

			key := extractMasterKey(c)

			if expected == "" || key == "" || subtle.ConstantTimeCompare([]byte(key), []byte(expected)) != 1 {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"code":    "unauthorized",
					"message": "invalid or missing master key",
				})
			}

			return next(c)
		}
	}
}

// MasterAuth returns MasterAuthMiddleware configured from a *config.Config.
func MasterAuth(cfg *config.Config) echo.MiddlewareFunc {
	if cfg == nil {
		return MasterAuthMiddleware("", "")
	}
	return MasterAuthMiddleware(cfg.MasterKey, cfg.AdminPassword)
}

func extractMasterKey(c *echo.Context) string {
	authHeader := c.Request().Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return strings.TrimSpace(parts[1])
		}
		return strings.TrimSpace(authHeader)
	}

	if xKey := c.Request().Header.Get("X-Master-Key"); xKey != "" {
		return strings.TrimSpace(xKey)
	}

	if qKey := c.QueryParam("master_key"); qKey != "" {
		return strings.TrimSpace(qKey)
	}
	if qKey := c.QueryParam("token"); qKey != "" {
		return strings.TrimSpace(qKey)
	}
	if qKey := c.QueryParam("api_key"); qKey != "" {
		return strings.TrimSpace(qKey)
	}

	return ""
}
