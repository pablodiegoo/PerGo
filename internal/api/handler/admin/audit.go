package admin

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/pages"
)

// AuditHandler holds dependencies for audit log admin operations.
type AuditHandler struct {
	Repo       *repository.AuditRepository
	Workspaces *repository.WorkspaceRepository
}

// parseAuditFilters extracts filter parameters from the request query string.
func parseAuditFilters(c *echo.Context) repository.AuditFilters {
	filters := repository.AuditFilters{
		Page:     1,
		PageSize: 50,
	}

	if wsStr := c.QueryParam("workspace_id"); wsStr != "" {
		if id, err := uuid.Parse(wsStr); err == nil {
			filters.WorkspaceID = &id
		}
	}
	if traceID := c.QueryParam("trace_id"); traceID != "" {
		filters.TraceID = traceID
	}
	if eventType := c.QueryParam("event_type"); eventType != "" {
		filters.EventType = eventType
	}
	if startStr := c.QueryParam("start"); startStr != "" {
		if t, err := time.Parse("2006-01-02", startStr); err == nil {
			filters.Start = &t
		}
	}
	if endStr := c.QueryParam("end"); endStr != "" {
		if t, err := time.Parse("2006-01-02", endStr); err == nil {
			// Set end to end of day
			endOfDay := t.Add(24*time.Hour - time.Nanosecond)
			filters.End = &endOfDay
		}
	}
	if pageStr := c.QueryParam("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			filters.Page = p
		}
	}
	if sizeStr := c.QueryParam("pageSize"); sizeStr != "" {
		if s, err := strconv.Atoi(sizeStr); err == nil && s > 0 {
			filters.PageSize = s
		}
	}

	return filters
}

// List renders the audit log list page or HTMX fragment with filtering and pagination.
func (h *AuditHandler) List(c *echo.Context) error {
	ctx := c.Request().Context()
	filters := parseAuditFilters(c)

	// Get workspace list for filter dropdown
	var workspaces []repository.Workspace
	if h.Workspaces != nil {
		workspaces, _ = h.Workspaces.List(ctx, 100)
	}

	// Query audit entries
	entries, total, err := h.Repo.ListFiltered(ctx, filters)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to load audit logs")
	}

	if mw.IsHTMX(c) {
		return mw.Render(c, http.StatusOK, pages.AuditTableFragment(entries, total, filters))
	}
	return mw.Render(c, http.StatusOK, pages.AuditPage(entries, total, filters, workspaces))
}

// ExportCSV streams filtered audit logs as a CSV download.
func (h *AuditHandler) ExportCSV(c *echo.Context) error {
	ctx := c.Request().Context()
	filters := parseAuditFilters(c)

	// Query all matching entries (no pagination for export)
	entries, err := h.Repo.ListAll(ctx, filters)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to export audit logs")
	}

	// Set CSV headers
	c.Response().Header().Set("Content-Type", "text/csv; charset=UTF-8")
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=audit-logs-%s.csv", time.Now().Format("2006-01-02")))

	// Write CSV using stdlib encoding/csv
	c.Response().WriteHeader(http.StatusOK)
	w := csv.NewWriter(c.Response())
	defer w.Flush()

	// Header row
	w.Write([]string{"timestamp", "workspace_id", "trace_id", "event_type", "payload"})

	// Data rows
	for _, entry := range entries {
		payload := string(entry.Payload)
		// Strip JSON quotes from payload if present
		payload = strings.Trim(payload, "\"")
		w.Write([]string{
			entry.CreatedAt.Format(time.RFC3339),
			entry.WorkspaceID.String(),
			entry.TraceID,
			entry.EventType,
			payload,
		})
	}

	return nil
}
