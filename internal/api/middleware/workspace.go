// Package middleware provides Echo v5 middleware functions for the PerGo API and Admin panel.
package middleware

import (
	"context"
	"errors"
	"log/slog"
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

type (
	workspaceContextKey struct{}
	pathContextKey      struct{}
)

// WithActiveWorkspace injects the active workspace into the context.
func WithActiveWorkspace(ctx context.Context, ws *repository.Workspace) context.Context {
	return context.WithValue(ctx, workspaceContextKey{}, ws)
}

// ActiveWorkspaceFrom retrieves the active workspace from context.
func ActiveWorkspaceFrom(ctx context.Context) *repository.Workspace {
	if ws, ok := ctx.Value(workspaceContextKey{}).(*repository.Workspace); ok {
		return ws
	}
	if ws, ok := ctx.Value("active_workspace").(*repository.Workspace); ok {
		return ws
	}
	return nil
}

// WithActivePath injects the active URL path into the context.
func WithActivePath(ctx context.Context, path string) context.Context {
	return context.WithValue(ctx, pathContextKey{}, path)
}

// ActivePathFrom retrieves the active URL path from context.
func ActivePathFrom(ctx context.Context) string {
	if p, ok := ctx.Value(pathContextKey{}).(string); ok {
		return p
	}
	if p, ok := ctx.Value("active_path").(string); ok {
		return p
	}
	return ""
}

// ActiveWorkspaceMiddleware returns an Echo v5 middleware that centrally resolves
// the Active Workspace for every incoming operator request.
//
//  1. It validates the pergo-active-workspace cookie against PostgreSQL.
//  2. If valid, it injects the resolved Workspace into request context (tenant.WithWorkspaceID).
//  3. If the cookie is absent, invalid, or points to a deleted workspace, it automatically falls back
//     to the earliest created workspace in PostgreSQL (ORDER BY created_at ASC LIMIT 1) and issues an
//     updated cookie to the browser.
//  4. If the database has zero workspaces, it redirects the operator to /admin/workspaces/new with an onboarding message.
func ActiveWorkspaceMiddleware(wsRepo *repository.WorkspaceRepository) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			ctx := c.Request().Context()
			path := c.Request().URL.Path
			method := c.Request().Method

			// Exclude workspace creation and public admin routes from empty database redirection
			isCreateWorkspacePath := strings.HasPrefix(path, "/admin/workspaces/new") ||
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
						var getErr error
						ws, getErr = wsRepo.GetByID(ctx, parsedID)
						if getErr != nil && !errors.Is(getErr, repository.ErrWorkspaceNotFound) {
							slog.WarnContext(ctx, "failed to get workspace by cookie ID", "workspace_id", parsedID, "error", getErr)
						}
					}
				}

				// 2. If cookie is missing or invalid/deleted, fall back to earliest created workspace
				if ws == nil {
					earliest, err := wsRepo.GetEarliest(ctx)
					if err != nil && !errors.Is(err, repository.ErrWorkspaceNotFound) {
						slog.WarnContext(ctx, "failed to get earliest workspace", "error", err)
					}
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
					ctx = WithActivePath(ctx, path)
					c.SetRequest(c.Request().WithContext(ctx))
					return next(c)
				}
				// Empty database condition -> redirect operator to /admin/workspaces/new?onboarding=true
				onboardingURL := "/admin/workspaces/new?onboarding=true"
				if c.Request().Header.Get("HX-Request") == "true" {
					c.Response().Header().Set("HX-Redirect", onboardingURL)
					return c.NoContent(http.StatusOK)
				}
				return c.Redirect(http.StatusFound, onboardingURL)
			}

			// 4. Inject workspace into context
			ctx = tenant.WithWorkspaceID(ctx, ws.ID)
			ctx = WithActiveWorkspace(ctx, ws)
			ctx = WithActivePath(ctx, path)

			c.SetRequest(c.Request().WithContext(ctx))
			c.Set("workspace", ws)

			return next(c)
		}
	}
}

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
