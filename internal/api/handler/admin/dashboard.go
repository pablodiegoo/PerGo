// Package admin provides HTTP handlers for the PerGo admin panel.
package admin

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/platform/audit"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/platform/queue"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/templates/layout"
	"github.com/pablojhp.pergo/templates/pages"
)

// DashboardHandler holds dependencies for the admin dashboard.
type DashboardHandler struct {
	Pool        *pgxpool.Pool
	Workspaces  *repository.WorkspaceRepository
	Audit       *audit.Querier
	APIKeys     *repository.APIKeyRepository
	Connections *repository.ConnectionRepository
	Publisher   *queue.JetStreamPublisher
}

// Index renders the dashboard landing page.
// Returns a full page for direct navigation, or an HTML fragment for HTMX requests.
func (h *DashboardHandler) Index(c *echo.Context) error {
	ctx := c.Request().Context()

	// 1. Resolve workspace from context (injected by ActiveWorkspaceMiddleware) or fallback
	var ws *repository.Workspace
	if wsVal, ok := ctx.Value("active_workspace").(*repository.Workspace); ok && wsVal != nil {
		ws = wsVal
	} else if wsID, ok := tenant.WorkspaceIDFrom(ctx); ok && h.Workspaces != nil {
		ws, _ = h.Workspaces.GetByID(ctx, wsID)
	}
	if ws == nil && h.Workspaces != nil {
		ws, _ = h.Workspaces.GetEarliest(ctx)
	}
	if ws == nil {
		if h.Workspaces == nil {
			ws = &repository.Workspace{
				ID:   uuid.New(),
				Name: "Workspace",
			}
		} else {
			return c.Redirect(http.StatusFound, "/admin/workspaces/new?onboarding=true")
		}
	}

	// 2. Query workspace count, API keys count and connections count for this workspace
	wsCount := 0
	if h.Workspaces != nil {
		if count, err := h.Workspaces.Count(ctx); err == nil {
			wsCount = count
		}
	}

	activeKeysCount := 0
	if h.APIKeys != nil {
		if count, err := h.APIKeys.CountActive(ctx, ws.ID); err == nil {
			activeKeysCount = count
		}
	}

	activeConnectionsCount := 0
	if h.Connections != nil {
		if count, err := h.Connections.CountActiveByWorkspace(ctx, ws.ID); err == nil {
			activeConnectionsCount = count
		}
	}

	var connections []*repository.Connection
	if h.Connections != nil {
		list, err := h.Connections.ListByWorkspace(ctx, ws.ID)
		if err == nil {
			connections = list
		}
	}

	recentAuditCount := 0
	var recentLogs []audit.RecentEntry
	if h.Audit != nil {
		entries, err := h.Audit.ListRecent(ctx, 10)
		if err == nil {
			recentLogs = entries
			recentAuditCount = len(entries)
		}
	}

	// Fetch all workspaces for selector
	var allWorkspaces []repository.Workspace
	if h.Workspaces != nil {
		allWorkspaces, _ = h.Workspaces.List(ctx, 50)
	} else {
		allWorkspaces = []repository.Workspace{*ws}
	}

	// Determine if workspace is fully onboarded (requires at least 1 active connection and 1 active API key)
	isOnboarded := activeKeysCount > 0 && activeConnectionsCount > 0

	dashboard := pages.Dashboard(ws, allWorkspaces, wsCount, recentAuditCount, isOnboarded, activeKeysCount, activeConnectionsCount, connections, recentLogs)

	if mw.IsHTMX(c) {
		return mw.Render(c, http.StatusOK, dashboard)
	}

	return mw.Render(c, http.StatusOK, layout.Base("Dashboard", dashboard))
}

// SimulateWebhook publishes a mock event to the webhooks NATS channel and records it in audit logs.
// POST /admin/webhook/simulate
func (h *DashboardHandler) SimulateWebhook(c *echo.Context) error {
	ctx := c.Request().Context()
	wsIDStr := c.FormValue("workspace_id")
	if wsIDStr == "" {
		return c.String(http.StatusBadRequest, "workspace_id is required")
	}
	wsID, err := uuid.Parse(wsIDStr)
	if err != nil {
		return c.String(http.StatusBadRequest, "invalid workspace_id")
	}

	eventType := c.FormValue("event_type")
	if eventType == "" {
		eventType = "message.delivered"
	}

	traceID := uuid.New().String()
	messageID := uuid.New().String()

	evt := struct {
		Event       string `json:"event"`
		TraceID     string `json:"trace_id"`
		MessageID   string `json:"message_id"`
		Channel     string `json:"channel"`
		Timestamp   string `json:"timestamp"`
		WorkspaceID string `json:"workspace_id"`
	}{
		Event:       eventType,
		TraceID:     traceID,
		MessageID:   messageID,
		Channel:     "whatsapp",
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		WorkspaceID: wsID.String(),
	}

	payload, err := json.Marshal(evt)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to marshal event")
	}

	if h.Publisher != nil {
		err = h.Publisher.Publish(ctx, "webhooks.events", payload, traceID)
		if err != nil {
			return c.HTML(http.StatusOK, `<div class="alert alert-error">Failed to publish simulation event: `+err.Error()+`</div>`)
		}
	}

	// Record in audit log so it shows up on the dashboard
	if h.Pool != nil {
		_, _ = h.Pool.Exec(ctx, `
			INSERT INTO audit_logs (id, workspace_id, trace_id, event_type, payload, created_at)
			VALUES ($1, $2, $3, $4, $5, NOW())
		`, uuid.New(), wsID, traceID, eventType, payload)
	}

	return c.HTML(http.StatusOK, `
		<div class="alert alert-success mt-2 p-3 bg-green-50 text-green-800 border border-green-200 rounded">
			<strong>Simulation Sent!</strong> Event <code>`+eventType+`</code> enqueued.<br/>
			<span class="text-xs font-mono">Trace ID: `+traceID+`</span>
		</div>
	`)
}

// ResolveWorkspaceSwitchTarget determines if a URL is a detail view
// that should redirect to its parent section root, or a list view that should reload in place.
func ResolveWorkspaceSwitchTarget(rawURL string) (isDetail bool, targetPath string) {
	if rawURL == "" {
		return false, "/admin/"
	}

	parsed, err := url.Parse(rawURL)
	var path string
	if err == nil && parsed.Path != "" {
		path = parsed.Path
	} else {
		path = rawURL
	}

	trimmed := strings.TrimRight(path, "/")
	if trimmed == "" {
		trimmed = "/admin"
	}

	segments := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(segments) == 0 || segments[0] != "admin" {
		return false, path
	}

	if len(segments) == 1 { // /admin
		return false, path
	}

	section := segments[1]

	switch section {
	case "workspaces", "workspace":
		if len(segments) == 2 {
			return false, path
		}
		sub := segments[2]
		if sub == "new" || sub == "selector" || sub == "active" {
			return false, path
		}
		return true, "/admin/workspaces"

	case "campaigns":
		if len(segments) == 2 || (len(segments) == 3 && segments[2] == "new") {
			return false, path
		}
		return true, "/admin/campaigns"

	case "webhooks":
		if len(segments) == 2 {
			return false, path
		}
		if len(segments) >= 3 {
			sub := segments[2]
			if sub == "subscriptions" {
				if len(segments) == 3 || (len(segments) == 4 && segments[3] == "new") {
					return false, path
				}
				return true, "/admin/webhooks"
			}
			if sub == "dlq" {
				if len(segments) == 3 || (len(segments) == 4 && segments[3] == "badge") {
					return false, path
				}
				return true, "/admin/webhooks"
			}
		}
		return false, path

	case "logs", "audit":
		if len(segments) <= 3 {
			return false, path
		}
		if len(segments) >= 4 && segments[2] == "actions" {
			return true, "/admin/logs/actions"
		}
		return false, path

	case "connections", "devices":
		if len(segments) == 2 || (len(segments) == 3 && (segments[2] == "pair-form" || segments[2] == "test" || segments[2] == "qr")) {
			return false, path
		}
		return true, "/admin/connections"

	case "tags", "templates", "telemetry", "inbox", "integrations":
		return false, path

	default:
		return false, path
	}
}

// SelectWorkspace updates the active workspace cookie and triggers smart navigation.
// POST /admin/workspaces/active
func (h *DashboardHandler) SelectWorkspace(c *echo.Context) error {
	wsIDStr := c.FormValue("workspace_id")
	if wsIDStr == "" {
		return c.String(http.StatusBadRequest, "workspace_id is required")
	}
	wsID, err := uuid.Parse(wsIDStr)
	if err != nil || wsID == uuid.Nil {
		return c.String(http.StatusBadRequest, "invalid workspace_id")
	}

	mw.SetActiveWorkspaceCookie(c, wsID)

	currentURL := c.Request().Header.Get("HX-Current-URL")
	if currentURL == "" {
		currentURL = c.FormValue("current_path")
	}
	if currentURL == "" {
		currentURL = c.Request().Referer()
	}

	isDetail, targetPath := ResolveWorkspaceSwitchTarget(currentURL)

	if mw.IsHTMX(c) {
		if isDetail {
			c.Response().Header().Set("HX-Redirect", targetPath)
		} else {
			c.Response().Header().Set("HX-Refresh", "true")
		}
		return c.NoContent(http.StatusOK)
	}

	if isDetail {
		return c.Redirect(http.StatusSeeOther, targetPath)
	}
	return c.NoContent(http.StatusOK)
}

// WorkspaceSelector renders the workspace dropdown selector for the sidebar.
// GET /admin/workspaces/selector
func (h *DashboardHandler) WorkspaceSelector(c *echo.Context) error {
	ctx := c.Request().Context()

	var ws *repository.Workspace
	if wsVal, ok := ctx.Value("active_workspace").(*repository.Workspace); ok && wsVal != nil {
		ws = wsVal
	} else if wsID, ok := tenant.WorkspaceIDFrom(ctx); ok && h.Workspaces != nil {
		ws, _ = h.Workspaces.GetByID(ctx, wsID)
	}
	if ws == nil && h.Workspaces != nil {
		ws, _ = h.Workspaces.GetEarliest(ctx)
	}

	var workspaces []repository.Workspace
	if h.Workspaces != nil {
		workspaces, _ = h.Workspaces.List(ctx, 50)
	}

	return mw.Render(c, http.StatusOK, layout.WorkspaceSelector(ws, workspaces))
}
