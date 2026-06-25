// Package middleware provides Echo v5 middleware functions for the OmniGo API.
package middleware

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/pablojhp.omnigo/internal/platform/crypto"
	"github.com/pablojhp.omnigo/internal/platform/postgres/tenant"
	"github.com/pablojhp.omnigo/internal/repository"
)

// AuthMiddleware returns an Echo middleware that validates API keys from the
// Authorization header and injects workspace_id into the request context.
func AuthMiddleware(repo *repository.APIKeyRepository) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			authHeader := c.Request().Header.Get("Authorization")
			if authHeader == "" {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"code":    "unauthorized",
					"message": "invalid or missing API key",
				})
			}

			// Parse "Bearer <key>"
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"code":    "unauthorized",
					"message": "invalid or missing API key",
				})
			}
			key := parts[1]
			if len(key) < 8 {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"code":    "unauthorized",
					"message": "invalid or missing API key",
				})
			}

			prefix := key[:8]
			apiKey, err := repo.GetByPrefix(c.Request().Context(), prefix)
			if err != nil {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"code":    "unauthorized",
					"message": "invalid or missing API key",
				})
			}

			// Verify the full key by comparing hashes
			if !crypto.VerifyAPIKey(key, apiKey.KeyHash) {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"code":    "unauthorized",
					"message": "invalid or missing API key",
				})
			}

			// Inject workspace_id into request context
			ctx := tenant.WithWorkspaceID(c.Request().Context(), apiKey.WorkspaceID)
			c.SetRequest(c.Request().WithContext(ctx))

			return next(c)
		}
	}
}
