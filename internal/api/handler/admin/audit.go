package admin

import (
	"net/http"

	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.omnigo/internal/api/middleware"
	"github.com/pablojhp.omnigo/internal/repository"
	"github.com/pablojhp.omnigo/templates/pages"
)

// AuditHandler holds dependencies for audit log admin operations.
type AuditHandler struct {
	Repo       *repository.AuditRepository
	Workspaces *repository.WorkspaceRepository
}

// List renders the audit log list page or HTMX fragment with filtering and pagination.
func (h *AuditHandler) List(c *echo.Context) error {
	// Stub — to be implemented in GREEN phase
	if mw.IsHTMX(c) {
		return mw.Render(c, http.StatusOK, pages.AuditTableFragment(nil, 0, repository.AuditFilters{}))
	}
	return mw.Render(c, http.StatusOK, pages.AuditPage(nil, 0, repository.AuditFilters{}, nil))
}

// ExportCSV streams filtered audit logs as a CSV download.
func (h *AuditHandler) ExportCSV(c *echo.Context) error {
	// Stub — to be implemented in GREEN phase
	return c.String(http.StatusOK, "")
}
