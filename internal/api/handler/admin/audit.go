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

// Redirect redirects /admin/audit to /admin/audit/inbound
func (h *AuditHandler) Redirect(c *echo.Context) error {
	return c.Redirect(http.StatusMovedPermanently, "/admin/audit/inbound")
}

// ListInbound renders the inbound audit log page.
func (h *AuditHandler) ListInbound(c *echo.Context) error {
	ctx := c.Request().Context()
	filters := parseAuditFilters(c)
	filters.EventType = "inbound_message"

	var workspaces []repository.Workspace
	if h.Workspaces != nil {
		workspaces, _ = h.Workspaces.List(ctx, 100)
	}

	entries, total, err := h.Repo.ListFiltered(ctx, filters)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to load inbound audit logs")
	}

	if mw.IsHTMX(c) {
		return mw.Render(c, http.StatusOK, pages.AuditTableFragment(entries, total, filters, "/admin/audit/inbound"))
	}
	return mw.Render(c, http.StatusOK, pages.AuditPage("Inbound Audit Logs", entries, total, filters, workspaces, "/admin/audit/inbound"))
}

// ListOutbound renders the outbound audit log page.
func (h *AuditHandler) ListOutbound(c *echo.Context) error {
	ctx := c.Request().Context()
	filters := parseAuditFilters(c)
	filters.EventType = "outbound_message"

	var workspaces []repository.Workspace
	if h.Workspaces != nil {
		workspaces, _ = h.Workspaces.List(ctx, 100)
	}

	entries, total, err := h.Repo.ListFiltered(ctx, filters)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to load outbound audit logs")
	}

	if mw.IsHTMX(c) {
		return mw.Render(c, http.StatusOK, pages.AuditTableFragment(entries, total, filters, "/admin/audit/outbound"))
	}
	return mw.Render(c, http.StatusOK, pages.AuditPage("Outbound Audit Logs", entries, total, filters, workspaces, "/admin/audit/outbound"))
}

// ExportInboundCSV streams filtered inbound audit logs as a CSV download.
func (h *AuditHandler) ExportInboundCSV(c *echo.Context) error {
	filters := parseAuditFilters(c)
	filters.EventType = "inbound_message"
	return h.exportCSV(c, filters, "inbound")
}

// ExportOutboundCSV streams filtered outbound audit logs as a CSV download.
func (h *AuditHandler) ExportOutboundCSV(c *echo.Context) error {
	filters := parseAuditFilters(c)
	filters.EventType = "outbound_message"
	return h.exportCSV(c, filters, "outbound")
}

func (h *AuditHandler) exportCSV(c *echo.Context, filters repository.AuditFilters, prefix string) error {
	ctx := c.Request().Context()

	entries, err := h.Repo.ListAll(ctx, filters)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to export audit logs")
	}

	c.Response().Header().Set("Content-Type", "text/csv; charset=UTF-8")
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-audit-logs-%s.csv", prefix, time.Now().Format("2006-01-02")))

	c.Response().WriteHeader(http.StatusOK)
	w := csv.NewWriter(c.Response())
	defer w.Flush()

	w.Write([]string{"timestamp", "workspace_id", "trace_id", "event_type", "payload"})

	for _, entry := range entries {
		payload := string(entry.Payload)
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
