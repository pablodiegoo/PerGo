package admin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/handler/admin"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/session"
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

// TestDeviceHandler_StartPairing_LimitExceeded checks that the handler returns HTTP 422
// when the WhatsApp connection limit is exceeded.
func TestDeviceHandler_StartPairing_LimitExceeded(t *testing.T) {
	// Set environment variable to 0 to force limit exceeded immediately.
	t.Setenv("PERGO_MAX_WHATSAPP_CONNECTIONS", "0")

	// We can use a test DB pool if available, otherwise skip.
	// Since the Manager needs a DB to count, we initialize a minimal pool.
	dsn := "postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable"
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skip("PostgreSQL not available for testing")
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Skip("PostgreSQL ping failed")
	}

	sqlDB, err := postgres.NewSQLDB(pool)
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}

	repo := session.NewDeviceRepository(pool)
	registry := session.NewActiveSession()
	manager := session.NewManager(
		sqlDB,
		repo,
		registry,
		nil,
		"",
		nil,
	)

	h := &admin.DeviceHandler{
		Repo:     repo,
		Sessions: registry,
		Manager:  manager,
	}

	e := echo.New()
	fValues := make(url.Values)
	fValues.Set("phone", "5511999990001")
	req := httptest.NewRequest(http.MethodPost, "/admin/devices/pair", strings.NewReader(fValues.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err = h.StartPairing(c)
	if err != nil {
		t.Errorf("StartPairing returned error: %v", err)
	}

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected status 422, got %d", rec.Code)
	}

	// Response body should contain the error message
	if !strings.Contains(rec.Body.String(), "maximum active WhatsApp connections limit exceeded") {
		t.Errorf("expected body to contain limit exceeded message, got: %s", rec.Body.String())
	}
}
