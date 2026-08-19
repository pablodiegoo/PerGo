// Package middleware provides Echo v5 middleware functions for the PerGo API.
package middleware

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
)

// AuthMiddleware returns an Echo middleware that validates API keys from the
// Authorization header and injects workspace_id into the request context.
func AuthMiddleware(repo *repository.APIKeyRepository) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			path := c.Request().URL.Path
			if path == "/" || path == "/healthz" || path == "/readyz" || strings.HasPrefix(path, "/admin") || strings.HasPrefix(path, "/webhooks") || strings.HasPrefix(path, "/static") || isMasterWorkspacePath(path) {
				return next(c)
			}

			authHeader := c.Request().Header.Get("Authorization")
			var key string
			if authHeader != "" {
				parts := strings.SplitN(authHeader, " ", 2)
				if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
					key = parts[1]
				}
			} else {
				key = c.QueryParam("api_key")
				if key == "" {
					key = c.QueryParam("token")
				}
			}

			if key == "" || len(key) < 8 {
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
			c.Set("api_key", apiKey)

			return next(c)
		}
	}
}

func isMasterWorkspacePath(path string) bool {
	if path == "/api/v1/workspaces" || path == "/api/v1/workspaces/" {
		return true
	}
	if strings.HasPrefix(path, "/api/v1/workspaces/") {
		trimmed := strings.TrimPrefix(path, "/api/v1/workspaces/")
		parts := strings.Split(trimmed, "/")
		if len(parts) == 1 {
			// /api/v1/workspaces/:id (only if valid UUID)
			if _, err := uuid.Parse(parts[0]); err == nil {
				return true
			}
			return false
		}
		if len(parts) >= 2 {
			if _, err := uuid.Parse(parts[0]); err == nil {
				if parts[1] == "api-keys" || parts[1] == "regenerate-key" {
					return true
				}
			}
			return false
		}
		return false
	}
	return false
}
