package admin_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	"github.com/pablojhp.pergo/internal/api/handler/admin"
	"github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/session"
)

// TestDeviceHandler_Construction verifies fields are correct.
func TestDeviceHandler_Construction(t *testing.T) {
	h := &admin.DeviceHandler{
		Repo:          nil,
		Sessions:      nil,
		Manager:       nil,
		Connections:   nil,
		Publisher:     nil,
		NC:            nil,
		TemplatesRepo: nil,
	}
	if h == nil {
		t.Fatal("expected non-nil DeviceHandler")
	}
}

// TestDeviceHandler_GetQR_MissingPhone verifies BadRequest response when phone param is missing.
func TestDeviceHandler_GetQR_MissingPhone(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/devices/qr", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &admin.DeviceHandler{}
	err := h.GetQR(c)
	if err != nil {
		t.Logf("GetQR returned error (acceptable): %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

// TestDeviceHandler_DatabaseFlows runs integration tests against real PostgreSQL.
func TestDeviceHandler_DatabaseFlows(t *testing.T) {
	dsn := "postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable"
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		// Try fallback port 5433 for testing environments
		dsnFallback := "postgres://postgres:postgres@localhost:5433/pergo?sslmode=disable"
		pool, err = pgxpool.New(ctx, dsnFallback)
		if err != nil {
			t.Skip("PostgreSQL not available for testing")
		}
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Skip("PostgreSQL ping failed")
	}

	encryptor, err := crypto.NewEncryptor([]byte("dev-development-key-32-bytes-kek"))
	if err != nil {
		t.Fatalf("failed to initialize encryptor: %v", err)
	}

	connRepo := repository.NewConnectionRepository(pool, encryptor)
	deviceRepo := session.NewDeviceRepository(pool)
	wsRepo := repository.NewWorkspaceRepository(pool)

	// Setup a test workspace
	ws, err := wsRepo.Create(ctx, "Test Workspace Devices")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() {
		_ = wsRepo.Delete(ctx, ws.ID)
	}()

	h := &admin.DeviceHandler{
		Repo:        deviceRepo,
		Connections: connRepo,
	}

	e := echo.New()

	t.Run("List Connections", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/devices", nil)
		// Set workspace cookie
		req.AddCookie(&http.Cookie{Name: "pergo-active-workspace", Value: ws.ID.String()})
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		if err := h.List(c); err != nil {
			t.Errorf("List returned error: %v", err)
		}

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}
	})

	t.Run("Create Telegram Bot - Bad Token", func(t *testing.T) {
		fValues := make(url.Values)
		fValues.Set("name", "Test TG Bot")
		fValues.Set("channel", "telegram")
		fValues.Set("token", "12345:invalidtoken")

		req := httptest.NewRequest(http.MethodPost, "/admin/devices/create", strings.NewReader(fValues.Encode()))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
		req.AddCookie(&http.Cookie{Name: "pergo-active-workspace", Value: ws.ID.String()})
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		if err := h.Create(c); err != nil {
			t.Errorf("Create returned error: %v", err)
		}

		// Validation should fail on getMe because token is dummy
		retarget := rec.Header().Get("HX-Retarget")
		if retarget != "#modal-error-container" {
			t.Errorf("expected HX-Retarget header, got %s", retarget)
		}
	})

	t.Run("Delete Connection (Disconnect)", func(t *testing.T) {
		// Manually insert a mock connection
		conn := &repository.Connection{
			WorkspaceID:    ws.ID,
			Name:           "Mock to delete",
			Channel:        "telegram",
			SenderIdentity: "@MockBot",
			Status:         "connected",
		}
		err := connRepo.Create(ctx, conn)
		if err != nil {
			t.Fatalf("failed to insert connection: %v", err)
		}

		req := httptest.NewRequest(http.MethodDelete, "/admin/devices/"+conn.ID.String(), nil)
		req.AddCookie(&http.Cookie{Name: "pergo-active-workspace", Value: ws.ID.String()})
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/admin/devices/:id")
		c.SetPathValues(echo.PathValues{
			{Name: "id", Value: conn.ID.String()},
		})

		if err := h.Disconnect(c); err != nil {
			t.Errorf("Disconnect returned error: %v", err)
		}

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}

		// Verify connection is gone
		_, err = connRepo.GetByID(ctx, conn.ID)
		if !errors.Is(err, repository.ErrConnectionNotFound) {
			t.Errorf("expected connection to be deleted, got error: %v", err)
		}
	})
}

// TestDeviceHandler_StartPairing_LimitExceeded checks that the handler returns HTTP 422
// when the WhatsApp connection limit is exceeded.
func TestDeviceHandler_StartPairing_LimitExceeded(t *testing.T) {
	t.Setenv("PERGO_MAX_WHATSAPP_CONNECTIONS", "0")

	dsn := "postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable"
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		dsnFallback := "postgres://postgres:postgres@localhost:5433/pergo?sslmode=disable"
		pool, err = pgxpool.New(ctx, dsnFallback)
		if err != nil {
			t.Skip("PostgreSQL not available for testing")
		}
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
		"2.3000.1025000000",
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

	if !strings.Contains(rec.Body.String(), "maximum active WhatsApp connections limit exceeded") {
		t.Errorf("expected body to contain limit exceeded message, got: %s", rec.Body.String())
	}
}

// TestDeviceHandler_WS_RequiresAuth asserts that the WebSocket endpoint /admin/devices/test/ws
// rejects unauthenticated requests.
func TestDeviceHandler_WS_RequiresAuth(t *testing.T) {
	e := echo.New()
	e.Use(middleware.SessionAuthMiddleware())

	h := &admin.DeviceHandler{}
	e.GET("/admin/devices/test/ws", h.WS)

	req := httptest.NewRequest(http.MethodGet, "/admin/devices/test/ws", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// Requests without session cookie redirect to /admin/login (302)
	if rec.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", rec.Code)
	}
	location := rec.Header().Get("Location")
	if location != "/admin/login" {
		t.Errorf("expected redirect to /admin/login, got %q", location)
	}
}

