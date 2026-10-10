// Package middleware provides Echo v5 middleware functions for the PerGo API.
package middleware

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
)

// AuthMiddleware returns an Echo middleware that validates API keys from the
// Authorization header and injects workspace_id and WorkspaceScope into the request context.
// Optional masterKeys can be provided to allow System Operators using a Master Key
// to authenticate against protected API routes.
func AuthMiddleware(repo *repository.APIKeyRepository, masterKeys ...string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			path := c.Request().URL.Path
			if path == "/" || path == "/healthz" || path == "/readyz" ||
				strings.HasPrefix(path, "/admin") ||
				strings.HasPrefix(path, "/webhooks") ||
				strings.HasPrefix(path, "/static") ||
				strings.HasPrefix(path, "/docs") ||
				strings.HasPrefix(path, "/api/openapi") ||
				strings.HasPrefix(path, "/mcp") ||
				path == "/mcp" ||
				strings.HasPrefix(path, "/oauth") ||
				strings.HasPrefix(path, "/.well-known") ||
				strings.HasPrefix(path, "/api/mcp") ||
				path == "/openapi.yaml" ||
				path == "/openapi.json" ||
				path == "/llms.txt" ||
				path == "/llms-full.txt" ||
				isMasterWorkspacePath(path) {
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

			// Check if a Master Key was provided
			var expectedMasterKey string
			if len(masterKeys) > 0 && masterKeys[0] != "" {
				expectedMasterKey = masterKeys[0]
			} else {
				expectedMasterKey = os.Getenv("PERGO_MASTER_KEY")
			}

			masterCandidate := key
			if xKey := c.Request().Header.Get("X-Master-Key"); xKey != "" {
				masterCandidate = strings.TrimSpace(xKey)
			}

			if expectedMasterKey != "" && masterCandidate != "" && subtle.ConstantTimeCompare([]byte(masterCandidate), []byte(expectedMasterKey)) == 1 {
				ctx := domain.ContextWithScope(c.Request().Context(), domain.NewOperatorScope(uuid.Nil))
				c.SetRequest(c.Request().WithContext(ctx))
				return next(c)
			}

			if key == "" || len(key) < 8 {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"code":    "UNAUTHORIZED",
					"message": "invalid or missing API key",
				})
			}

			prefix := key[:8]
			apiKey, err := repo.GetByPrefix(c.Request().Context(), prefix)
			if err != nil {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"code":    "UNAUTHORIZED",
					"message": "invalid or missing API key",
				})
			}

			// Verify the full key by comparing hashes
			if !crypto.VerifyAPIKey(key, apiKey.KeyHash) {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"code":    "UNAUTHORIZED",
					"message": "invalid or missing API key",
				})
			}

			// Inject workspace_id and WorkspaceScope into request context
			ctx := tenant.WithWorkspaceID(c.Request().Context(), apiKey.WorkspaceID)
			ctx = domain.ContextWithScope(ctx, domain.NewWorkspaceScope(apiKey.WorkspaceID, domain.CapabilityWorkspaceScoped))
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
