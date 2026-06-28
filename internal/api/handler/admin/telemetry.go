package admin

import (
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/session"
	"github.com/pablojhp.pergo/templates/pages"
)

// NATSStatus is implemented by *nats.Conn (or a wrapper) to report connection health.
type NATSStatus interface {
	IsConnected() bool
}

// SessionInfo holds per-session telemetry data for the telemetry page.
type SessionInfo struct {
	JID            string
	Status         string
	ConnectedSince *time.Time
	MessagesSent   int64
}

// TelemetryData aggregates system health metrics for the telemetry page.
type TelemetryData struct {
	Sessions        []SessionInfo
	TotalQueueDepth int64
	NATSConnected   bool
	Uptime          string
	ActiveSessions  int
}

// TelemetryHandler serves the /admin/telemetry page.
type TelemetryHandler struct {
	Manager    *session.Manager
	Sessions   *session.ActiveSession
	QueueDepth *mw.QueueDepthTracker
	NC         NATSStatus
	StartTime  time.Time
}

// Index renders the telemetry page or HTMX fragment.
func (h *TelemetryHandler) Index(c *echo.Context) error {
	data := h.collectTelemetry()

	if mw.IsHTMX(c) {
		return mw.Render(c, http.StatusOK, pages.TelemetryContent(data))
	}
	return mw.Render(c, http.StatusOK, pages.TelemetryPage(data))
}

// collectTelemetry aggregates all telemetry data from live state.
func (h *TelemetryHandler) collectTelemetry() pages.TelemetryData {
	// Build session info from active in-memory sessions.
	var sessionInfos []pages.SessionInfo
	if h.Sessions != nil {
		activeSessions := h.Sessions.All()
		for _, s := range activeSessions {
			sessionInfos = append(sessionInfos, pages.SessionInfo{
				JID:          s.JID.String(),
				Status:       "connected",
				MessagesSent: s.MessagesSent.Load(),
			})
		}
	}

	// NATS connection status.
	natsConnected := false
	if h.NC != nil {
		natsConnected = h.NC.IsConnected()
	}

	// Uptime.
	uptime := formatDuration(time.Since(h.StartTime))

	return pages.TelemetryData{
		Sessions:       sessionInfos,
		NATSConnected:  natsConnected,
		Uptime:         uptime,
		ActiveSessions: len(sessionInfos),
	}
}

// formatDuration formats a duration as a human-readable string (e.g. "2h 15m").
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60

	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
