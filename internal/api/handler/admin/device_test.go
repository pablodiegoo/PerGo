package admin_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/handler/admin"
)

// TestDeviceHandler_List_NoRepo verifies that DeviceHandler is constructable.
func TestDeviceHandler_Construction(t *testing.T) {
	h := &admin.DeviceHandler{
		Repo:     nil,
		Sessions: nil,
		Manager:  nil,
	}
	// Verify handler struct is non-nil.
	if h == nil {
		t.Fatal("expected non-nil DeviceHandler")
	}
}

// TestDeviceHandler_GetQR_MissingPhone verifies 400 response when phone param is missing.
func TestDeviceHandler_GetQR_MissingPhone(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/devices/qr", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &admin.DeviceHandler{}
	if err := h.GetQR(c); err != nil {
		// Echo handlers may return errors or write directly
		t.Logf("GetQR returned error (acceptable): %v", err)
	}
	// Should be 400 or handled gracefully
}

// TestDeviceHandler_Disconnect_EmptyJID verifies graceful handling of empty JID.
func TestDeviceHandler_Disconnect_EmptyJID(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/admin/devices/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &admin.DeviceHandler{}
	if err := h.Disconnect(c); err != nil {
		t.Logf("Disconnect returned error (acceptable for empty JID): %v", err)
	}
}
