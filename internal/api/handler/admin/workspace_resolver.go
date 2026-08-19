// Package admin provides HTTP handlers for the PerGo admin panel.
package admin

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
)

// ResolveWorkspaceID extracts the target or active workspace ID from URL path parameters
// ("workspace_id" or "id"), tenant request context, or the active workspace session cookie.
func ResolveWorkspaceID(c *echo.Context) (uuid.UUID, error) {
	if idStr, err := echo.PathParam[string](c, "workspace_id"); err == nil && idStr != "" {
		if id, parseErr := uuid.Parse(idStr); parseErr == nil && id != uuid.Nil {
			return id, nil
		}
	}
	if wsID, ok := tenant.WorkspaceIDFrom(c.Request().Context()); ok && wsID != uuid.Nil {
		return wsID, nil
	}
	if cookie, err := c.Cookie(mw.ActiveWorkspaceCookieName); err == nil && cookie != nil && cookie.Value != "" {
		if id, parseErr := uuid.Parse(cookie.Value); parseErr == nil && id != uuid.Nil {
			return id, nil
		}
	}
	return uuid.Nil, fmt.Errorf("invalid or missing workspace ID")
}

// resolveWorkspaceID is an internal alias for ResolveWorkspaceID.
func resolveWorkspaceID(c *echo.Context) (uuid.UUID, error) {
	return ResolveWorkspaceID(c)
}

// ResolveWorkspaceIDForTest exposes ResolveWorkspaceID for test packages.
func ResolveWorkspaceIDForTest(c *echo.Context) (uuid.UUID, error) {
	return ResolveWorkspaceID(c)
}

// resolveWorkspaceIDOrNil returns the resolved workspace ID or uuid.Nil if absent.
func resolveWorkspaceIDOrNil(c *echo.Context) uuid.UUID {
	id, err := ResolveWorkspaceID(c)
	if err != nil {
		return uuid.Nil
	}
	return id
}
