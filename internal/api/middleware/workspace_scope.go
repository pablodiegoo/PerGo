// Package middleware provides Echo v5 middleware functions for the PerGo API and Admin panel.
package middleware

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
)

// EnforceWorkspaceScope returns an Echo middleware that validates tenant scope consistency
// across all protected API and admin requests.
//
// Invariants enforced:
//  1. Excluded public endpoints pass through without enforcement.
//  2. If context lacks domain.WorkspaceScope, fallback to tenant.WorkspaceIDFrom(ctx)
//     with CapabilityWorkspaceScoped. If still missing, returns 401 Unauthorized.
//  3. If :workspace_id parameter is present:
//     - WorkspaceScoped: param must equal scope.WorkspaceID. Mismatch logs
//       "security.workspace_mismatch_detected" and returns 403 Forbidden with WORKSPACE_ID_MISMATCH.
//     - OperatorScoped: param binds as active target scope if valid UUID.
//  4. If :workspace_id parameter is absent (flat route):
//     - OperatorScoped with uuid.Nil: requires X-Workspace-ID header or workspace_id query param.
//       Missing/invalid target returns 400 Bad Request with WORKSPACE_REQUIRED.
//  5. Injects verified scope and calls tenant.WithWorkspaceID(ctx, scope.WorkspaceID()) on request context.
func EnforceWorkspaceScope() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			path := c.Request().URL.Path
			if isExcludedPath(path) {
				return next(c)
			}

			ctx := c.Request().Context()
			scope, ok := domain.FromContext(ctx)
			if !ok {
				if wsID, hasTenant := tenant.WorkspaceIDFrom(ctx); hasTenant && wsID != uuid.Nil {
					scope = domain.NewWorkspaceScope(wsID, domain.CapabilityWorkspaceScoped)
				} else {
					return c.JSON(http.StatusUnauthorized, map[string]string{
						"code":    "UNAUTHORIZED",
						"message": "authentication and workspace scope required",
					})
				}
			}

			paramStr, err := echo.PathParam[string](c, "workspace_id")
			hasWorkspaceParam := err == nil && paramStr != ""

			if hasWorkspaceParam {
				parsedParam, parseErr := uuid.Parse(paramStr)
				if scope.Capability() == domain.CapabilityWorkspaceScoped {
					if parseErr != nil || parsedParam == uuid.Nil || parsedParam != scope.WorkspaceID() {
						slog.WarnContext(ctx, "security.workspace_mismatch_detected",
							"authenticated_workspace_id", scope.WorkspaceID(),
							"attempted_workspace_id", paramStr,
							"path", c.Request().URL.Path,
							"remote_addr", c.RealIP(),
						)
						return c.JSON(http.StatusForbidden, map[string]string{
							"code":    "WORKSPACE_ID_MISMATCH",
							"message": "authenticated workspace does not match URL parameter",
						})
					}
				} else if scope.Capability() == domain.CapabilityOperatorScoped {
					if parseErr != nil || parsedParam == uuid.Nil {
						return c.JSON(http.StatusBadRequest, map[string]string{
							"code":    "INVALID_WORKSPACE_ID",
							"message": "invalid workspace ID parameter",
						})
					}
					scope = scope.WithTarget(parsedParam)
				}
			} else {
				// Flat route
				if scope.Capability() == domain.CapabilityOperatorScoped {
					targetHeader := c.Request().Header.Get("X-Workspace-ID")
					if targetHeader == "" {
						targetHeader = c.QueryParam("workspace_id")
					}
					if targetHeader != "" {
						if parsedHeader, parseErr := uuid.Parse(targetHeader); parseErr == nil && parsedHeader != uuid.Nil {
							scope = scope.WithTarget(parsedHeader)
						}
					}
					if scope.WorkspaceID() == uuid.Nil {
						return c.JSON(http.StatusBadRequest, map[string]string{
							"code":    "WORKSPACE_REQUIRED",
							"message": "workspace target must be specified via header or URL for operator scope",
						})
					}
				}
			}

			// Update request context with authoritative scope and database tenant context bridge
			ctx = domain.ContextWithScope(ctx, scope)
			ctx = tenant.WithWorkspaceID(ctx, scope.WorkspaceID())
			c.SetRequest(c.Request().WithContext(ctx))

			return next(c)
		}
	}
}

func isExcludedPath(path string) bool {
	if path == "/" || path == "/healthz" || path == "/readyz" ||
		strings.HasPrefix(path, "/docs") ||
		strings.HasPrefix(path, "/static") ||
		strings.HasPrefix(path, "/openapi") ||
		strings.HasPrefix(path, "/api/openapi") ||
		path == "/openapi.yaml" ||
		path == "/openapi.json" ||
		path == "/llms.txt" ||
		path == "/llms-full.txt" ||
		strings.HasPrefix(path, "/webhooks") ||
		strings.HasPrefix(path, "/v1/webhooks") ||
		strings.HasPrefix(path, "/admin/login") ||
		strings.HasPrefix(path, "/admin/logout") ||
		strings.HasPrefix(path, "/admin/sso") ||
		strings.HasPrefix(path, "/admin/locale") {
		return true
	}
	return false
}
