// Package middleware provides Echo v5 middleware functions for the PerGo API and Admin panel.
package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
)

const (
	// ActiveWorkspaceCookieName is the cookie name storing the active workspace UUID.
	ActiveWorkspaceCookieName = "pergo-active-workspace"
)

// ActiveWorkspaceMiddleware returns an Echo v5 middleware that centrally resolves
// the Active Workspace for every incoming operator request.
//
// 1. It validates the pergo-active-workspace cookie against PostgreSQL.
// 2. If valid, it injects the resolved Workspace into request context (tenant.WithWorkspaceID).
// 3. If the cookie is absent, invalid, or points to a deleted workspace, it automatically falls back
//    to the earliest created workspace in PostgreSQL (ORDER BY created_at ASC LIMIT 1) and issues an
//    updated cookie to the browser.
// 4. If the database has zero workspaces, it redirects the operator to /admin/workspaces/new.
func ActiveWorkspaceMiddleware(wsRepo *repository.WorkspaceRepository) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			ctx := c.Request().Context()
			path := c.Request().URL.Path
			method := c.Request().Method

			// Exclude workspace creation and public admin routes from empty database redirection
			isCreateWorkspacePath := path == "/admin/workspaces/new" || path == "/admin/workspaces/new/" ||
				(strings.HasPrefix(path, "/admin/workspaces") && method == http.MethodPost) ||
				strings.HasPrefix(path, "/admin/login") ||
				strings.HasPrefix(path, "/admin/logout") ||
				strings.HasPrefix(path, "/admin/sso")

			var ws *repository.Workspace

			if wsRepo != nil {
				// 1. Check cookie
				cookie, err := c.Cookie(ActiveWorkspaceCookieName)
				if err == nil && cookie != nil && cookie.Value != "" {
					if parsedID, parseErr := uuid.Parse(cookie.Value); parseErr == nil && parsedID != uuid.Nil {
						ws, _ = wsRepo.GetByID(ctx, parsedID)
					}
				}

				// 2. If cookie is missing or invalid/deleted, fall back to earliest created workspace
				if ws == nil {
					earliest, err := wsRepo.GetEarliest(ctx)
					if err == nil && earliest != nil {
						ws = earliest
						// Issue updated cookie to the browser (auto-healing)
						SetActiveWorkspaceCookie(c, ws.ID)
					}
				}
			}

			// 3. Handle empty database condition
			if ws == nil {
				if isCreateWorkspacePath {
					ctx = context.WithValue(ctx, "active_path", path)
					ctx = context.WithValue(ctx, "workspaces_list", []repository.Workspace{})
					c.SetRequest(c.Request().WithContext(ctx))
					return next(c)
				}
				// Empty database condition -> redirect operator to /admin/workspaces/new
				if c.Request().Header.Get("HX-Request") == "true" {
					c.Response().Header().Set("HX-Redirect", "/admin/workspaces/new")
					return c.NoContent(http.StatusOK)
				}
				return c.Redirect(http.StatusFound, "/admin/workspaces/new")
			}

			// 4. Inject workspace into context
			ctx = tenant.WithWorkspaceID(ctx, ws.ID)
			ctx = context.WithValue(ctx, "active_workspace", ws)
			ctx = context.WithValue(ctx, "active_path", path)

			// Fetch workspaces list for sidebar selector
			if wsRepo != nil {
				workspaces, err := wsRepo.List(ctx, 50)
				if err == nil && len(workspaces) > 0 {
					ctx = context.WithValue(ctx, "workspaces_list", workspaces)
				}
			}

			c.SetRequest(c.Request().WithContext(ctx))
			c.Set("workspace", ws)

			return next(c)
		}
	}
}

// AdminWorkspaceMiddleware is an alias for ActiveWorkspaceMiddleware.
var AdminWorkspaceMiddleware = ActiveWorkspaceMiddleware

// SetActiveWorkspaceCookie sets the active workspace cookie on the response.
func SetActiveWorkspaceCookie(c *echo.Context, wsID uuid.UUID) {
	cookie := &http.Cookie{
		Name:     ActiveWorkspaceCookieName,
		Value:    wsID.String(),
		Path:     "/",
		Expires:  time.Now().Add(365 * 24 * time.Hour),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	c.SetCookie(cookie)
}

// ClearActiveWorkspaceCookie clears the active workspace cookie on the response.
func ClearActiveWorkspaceCookie(c *echo.Context) {
	cookie := &http.Cookie{
		Name:     ActiveWorkspaceCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	c.SetCookie(cookie)
}
