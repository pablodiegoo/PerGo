// Package admin provides HTTP handlers for the OmniGo admin panel.
package admin

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.omnigo/internal/api/middleware"
	"github.com/pablojhp.omnigo/internal/platform/audit"
	"github.com/pablojhp.omnigo/internal/repository"
	"github.com/pablojhp.omnigo/templates/layout"
	"github.com/pablojhp.omnigo/templates/pages"
)

// DashboardHandler holds dependencies for the admin dashboard.
type DashboardHandler struct {
	Pool       *pgxpool.Pool
	Workspaces *repository.WorkspaceRepository
	Audit      *audit.Querier
}

// Index renders the dashboard landing page.
// Returns a full page for direct navigation, or an HTML fragment for HTMX requests.
func (h *DashboardHandler) Index(c *echo.Context) error {
	ctx := c.Request().Context()

	workspaceCount := 0
	if h.Workspaces != nil {
		count, err := h.Workspaces.Count(ctx)
		if err == nil {
			workspaceCount = count
		}
	}

	recentCount := 0
	if h.Audit != nil {
		entries, err := h.Audit.ListRecent(ctx, 10)
		if err == nil {
			recentCount = len(entries)
		}
	}

	dashboard := pages.Dashboard(workspaceCount, recentCount)

	if mw.IsHTMX(c) {
		return mw.Render(c, http.StatusOK, dashboard)
	}

	return mw.Render(c, http.StatusOK, layout.Base("Dashboard", dashboard))
}
