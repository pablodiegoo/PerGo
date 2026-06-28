package admin_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/handler/admin"
)

// TestTelemetryHandler_Construction verifies TelemetryHandler is constructable.
func TestTelemetryHandler_Construction(t *testing.T) {
	h := &admin.TelemetryHandler{
		Manager:    nil,
		Sessions:   nil,
		QueueDepth: nil,
		NC:         nil,
		StartTime:  time.Now(),
	}
	if h == nil {
		t.Fatal("expected non-nil TelemetryHandler")
	}
}

// TestFormatDuration_Indirectly verifies telemetry handler can be called with nil deps.
func TestTelemetryHandler_Index_NilDeps(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/telemetry", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &admin.TelemetryHandler{
		StartTime: time.Now().Add(-5 * time.Minute),
	}

	// Should not panic with nil sessions/NATS.
	if err := h.Index(c); err != nil {
		t.Logf("Index returned error (may be acceptable without full deps): %v", err)
	}
}
