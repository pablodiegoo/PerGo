package admin

import (
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	mw "github.com/pablojhp.omnigo/internal/api/middleware"
	"github.com/pablojhp.omnigo/internal/session"
	"github.com/pablojhp.omnigo/templates/pages"
)

// DeviceHandler handles admin operations for WhatsApp Web device management.
type DeviceHandler struct {
	Repo     *session.DeviceRepository
	Sessions *session.ActiveSession
	Manager  *session.Manager
}

// pairingState holds the current QR pairing state for a phone number.
type pairingState struct {
	code    string        // raw QR code string (empty if not yet received)
	status  string        // "pending", "paired", "error"
	message string        // human-readable message
	expires time.Time     // when the current QR code expires
	mu      sync.RWMutex  // protects fields
}

// pairingSessions holds in-memory pairing state keyed by phone number.
// MVP: single-instance only (no distributed state).
var (
	pairingSessions   = make(map[string]*pairingState)
	pairingSessionsMu sync.Mutex
)

// List renders the device management page or HTMX fragment.
func (h *DeviceHandler) List(c *echo.Context) error {
	devices, err := h.Repo.ListAll(c.Request().Context())
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to load devices")
	}

	if mw.IsHTMX(c) {
		return mw.Render(c, http.StatusOK, pages.DeviceListContent(devices))
	}
	return mw.Render(c, http.StatusOK, pages.DeviceListPage(devices))
}

// PairForm renders the QR pairing initiation form fragment.
// GET /admin/devices/pair-form — HTMX-triggered by "Link Device" button.
func (h *DeviceHandler) PairForm(c *echo.Context) error {
	return mw.Render(c, http.StatusOK, pages.PairForm())
}

// StartPairing begins the QR pairing flow for a new device.
// POST /admin/devices/pair — expects form field "phone"
func (h *DeviceHandler) StartPairing(c *echo.Context) error {
	phone := c.FormValue("phone")
	if phone == "" {
		return c.String(http.StatusBadRequest, "phone number is required")
	}

	// Use a zero UUID as the workspace ID for single-tenant MVP.
	// Multi-tenant deployments should extract this from the operator session.
	wsID := uuid.Nil

	// Initialize pairing state.
	ps := &pairingState{status: "pending", message: "Waiting for QR code..."}
	pairingSessionsMu.Lock()
	pairingSessions[phone] = ps
	pairingSessionsMu.Unlock()

	// Start pairing in background.
	ch, err := h.Manager.StartPairing(c.Request().Context(), wsID, phone)
	if err != nil {
		ps.mu.Lock()
		ps.status = "error"
		ps.message = err.Error()
		ps.mu.Unlock()
		return mw.Render(c, http.StatusInternalServerError, pages.QRFragment("", phone, "error", err.Error()))
	}

	// Process QR events in background goroutine.
	go func() {
		for evt := range ch {
			ps.mu.Lock()
			switch evt.Type {
			case session.QREventCode:
				ps.code = string(evt.Data)
				ps.status = "pending"
				ps.message = evt.Message
				ps.expires = time.Now().Add(25 * time.Second)
			case session.QREventPaired:
				ps.code = ""
				ps.status = "paired"
				ps.message = evt.Message
			case session.QREventError:
				ps.code = ""
				ps.status = "error"
				ps.message = evt.Message
			}
			ps.mu.Unlock()
		}
		// Channel closed — cleanup after a delay.
		time.AfterFunc(30*time.Second, func() {
			pairingSessionsMu.Lock()
			delete(pairingSessions, phone)
			pairingSessionsMu.Unlock()
		})
	}()

	return mw.Render(c, http.StatusOK, pages.QRFragment("", phone, "pending", "Scan the QR code below to pair your device"))
}

// GetQR returns the current QR code state as an HTMX fragment.
// GET /admin/devices/qr?phone=...
func (h *DeviceHandler) GetQR(c *echo.Context) error {
	phone := c.QueryParam("phone")
	if phone == "" {
		return c.String(http.StatusBadRequest, "phone is required")
	}

	pairingSessionsMu.Lock()
	ps, ok := pairingSessions[phone]
	pairingSessionsMu.Unlock()

	if !ok {
		return mw.Render(c, http.StatusOK, pages.QRFragment("", phone, "error", "No active pairing session for this phone"))
	}

	ps.mu.RLock()
	code, status, message := ps.code, ps.status, ps.message
	ps.mu.RUnlock()

	return mw.Render(c, http.StatusOK, pages.QRFragment(code, phone, status, message))
}

// Disconnect stops an active device session.
// DELETE /admin/devices/:jid
func (h *DeviceHandler) Disconnect(c *echo.Context) error {
	jidStr, err := echo.PathParam[string](c, "jid")
	if err != nil || jidStr == "" {
		return c.String(http.StatusBadRequest, "invalid JID")
	}

	h.Sessions.DisconnectByJID(jidStr)

	devices, err := h.Repo.ListAll(c.Request().Context())
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to reload devices")
	}
	return mw.Render(c, http.StatusOK, pages.DeviceListContent(devices))
}
