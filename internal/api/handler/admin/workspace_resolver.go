// Package admin provides HTTP handlers for the PerGo admin panel.
package admin

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
)

// ResolveWorkspaceID extracts the authoritative workspace ID strictly from context.
// It first tries domain.Require, then falls back to tenant.RequireWorkspaceID.
func ResolveWorkspaceID(c *echo.Context) (uuid.UUID, error) {
	ctx := c.Request().Context()
	if scope, err := domain.Require(ctx); err == nil && scope.WorkspaceID() != uuid.Nil {
		return scope.WorkspaceID(), nil
	}
	if wsID, err := tenant.RequireWorkspaceID(ctx); err == nil && wsID != uuid.Nil {
		return wsID, nil
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

