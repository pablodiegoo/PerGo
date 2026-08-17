package admin_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	"encoding/json"
	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/outbound"
	"github.com/pablojhp.pergo/internal/api/handler/admin"
	"github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/session"
	"github.com/pablojhp.pergo/templates/pages"
)

func TestDeviceHandler_Construction(t *testing.T) {
	h := &admin.DeviceHandler{
		Sessions:      nil,
		Manager:       nil,
		Connections:   nil,
		Publisher:     nil,
		NC:            nil,
		TemplatesRepo: nil,
	}
	if h.Sessions != nil || h.Manager != nil {
		t.Fatal("expected nil initial fields")
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
	dsn := os.Getenv("PERGO_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable"
	}
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

	dsn := os.Getenv("PERGO_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable"
	}
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

	enc, _ := crypto.NewEncryptor(make([]byte, 32))
	repo := repository.NewConnectionRepository(pool, enc)
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
		Connections: repo,
		Sessions:    registry,
		Manager:     manager,
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

// TestDeviceHandler_QRFragment_Rendering verifies that the templ QRFragment renders properly.
func TestDeviceHandler_QRFragment_Rendering(t *testing.T) {
	// 1. Pending state with QR PNG
	var buf strings.Builder
	comp := pages.QRFragment("raw-qr-data", "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==", "5511999999999", "pending", "Aponte a camera")
	if err := comp.Render(context.Background(), &buf); err != nil {
		t.Fatalf("failed to render QRFragment pending: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "data:image/png;base64,") {
		t.Errorf("expected rendered HTML to contain base64 image data URL, got: %s", out)
	}
	if !strings.Contains(out, "hx-get=\"/admin/devices/qr?phone=5511999999999\"") {
		t.Errorf("expected rendered HTML to contain hx-get trigger for phone, got: %s", out)
	}

	// 2. Paired state
	buf.Reset()
	compPaired := pages.QRFragment("", "", "5511999999999", "paired", "")
	if err := compPaired.Render(context.Background(), &buf); err != nil {
		t.Fatalf("failed to render QRFragment paired: %v", err)
	}
	outPaired := buf.String()
	if !strings.Contains(outPaired, "Dispositivo Pareado com Sucesso!") {
		t.Errorf("expected rendered HTML to contain paired header, got: %s", outPaired)
	}
	if !strings.Contains(outPaired, "5511999999999") {
		t.Errorf("expected rendered HTML to contain phone number, got: %s", outPaired)
	}

	// 3. Error state
	buf.Reset()
	compError := pages.QRFragment("", "", "5511999999999", "error", "Limite de conexões excedido")
	if err := compError.Render(context.Background(), &buf); err != nil {
		t.Fatalf("failed to render QRFragment error: %v", err)
	}
	outError := buf.String()
	if !strings.Contains(outError, "Falha no Pareamento") {
		t.Errorf("expected rendered HTML to contain error header, got: %s", outError)
	}
	if !strings.Contains(outError, "Limite de conexões excedido") {
		t.Errorf("expected rendered HTML to contain error message, got: %s", outError)
	}
}

// TestDeviceHandler_RunTest_TemplateDynamicParamsAndLanguage verifies that RunTest
// serializes the template's specified language and all dynamic parameters.
func TestDeviceHandler_RunTest_TemplateDynamicParamsAndLanguage(t *testing.T) {
	dsn := os.Getenv("PERGO_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable"
	}
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

	enc, err := crypto.NewEncryptor([]byte("dev-development-key-32-bytes-kek"))
	if err != nil {
		t.Fatalf("failed to init encryptor: %v", err)
	}
	connRepo := repository.NewConnectionRepository(pool, enc)
	wsRepo := repository.NewWorkspaceRepository(pool)

	ws, err := wsRepo.Create(ctx, "Test Workspace RunTest Params")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() { _ = wsRepo.Delete(ctx, ws.ID) }()

	conn := &repository.Connection{
		WorkspaceID:    ws.ID,
		Name:           "WABA Cloud Test",
		Channel:        "whatsapp_cloud",
		SenderIdentity: "+15551234567",
		Status:         "connected",
	}
	if err := connRepo.Create(ctx, conn); err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}

	pub := &testMessagePublisher{}
	h := &admin.DeviceHandler{
		Connections: connRepo,
		Publisher:   pub,
	}

	e := echo.New()

	t.Run("Dispatches with language and dynamic parameters", func(t *testing.T) {
		fv := url.Values{}
		fv.Set("connection_id", conn.ID.String())
		fv.Set("to", "+5511999990001")
		fv.Set("is_template", "true")
		fv.Set("template_name", "shipping_update")
		fv.Set("language", "en_US")
		fv.Set("param_1", "Alice")
		fv.Set("param_2", "TRACK-999")
		fv.Set("param_3", "Arriving Monday")
		fv.Set("param_4", "Express")

		req := httptest.NewRequest(http.MethodPost, "/admin/devices/test", strings.NewReader(fv.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: "pergo-active-workspace", Value: ws.ID.String()})
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := h.RunTest(c)
		if err != nil {
			t.Fatalf("RunTest returned error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		qMsg := pub.lastMessage
		if qMsg.TemplateName != "shipping_update" {
			t.Errorf("expected TemplateName 'shipping_update', got %q", qMsg.TemplateName)
		}
		if qMsg.Language != "en_US" {
			t.Errorf("expected Language 'en_US', got %q", qMsg.Language)
		}
		if len(qMsg.Components) != 1 {
			t.Fatalf("expected 1 component, got %d", len(qMsg.Components))
		}
		params, err := outbound.NormalizeTemplateParams(qMsg.Components[0].Parameters)
		if err != nil {
			t.Fatalf("failed to normalize params: %v", err)
		}
		if len(params) != 4 {
			t.Fatalf("expected 4 parameters, got %d", len(params))
		}
		if params[0].Text != "Alice" || params[1].Text != "TRACK-999" || params[2].Text != "Arriving Monday" || params[3].Text != "Express" {
			t.Errorf("unexpected parameters: %+v", params)
		}
	})

	t.Run("Dispatches static template with zero parameters", func(t *testing.T) {
		fv := url.Values{}
		fv.Set("connection_id", conn.ID.String())
		fv.Set("to", "+5511999990001")
		fv.Set("is_template", "true")
		fv.Set("template_name", "static_welcome")
		fv.Set("language", "pt_BR")

		req := httptest.NewRequest(http.MethodPost, "/admin/devices/test", strings.NewReader(fv.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: "pergo-active-workspace", Value: ws.ID.String()})
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := h.RunTest(c)
		if err != nil {
			t.Fatalf("RunTest returned error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		qMsg := pub.lastMessage
		if qMsg.TemplateName != "static_welcome" {
			t.Errorf("expected TemplateName 'static_welcome', got %q", qMsg.TemplateName)
		}
		if qMsg.Language != "pt_BR" {
			t.Errorf("expected Language 'pt_BR', got %q", qMsg.Language)
		}
		if len(qMsg.Components) != 0 {
			t.Errorf("expected 0 components for static template, got %d", len(qMsg.Components))
		}
		if qMsg.Body != "[Template: static_welcome]" {
			t.Errorf("expected body '[Template: static_welcome]', got %q", qMsg.Body)
		}
	})

	t.Run("Returns 503 when publisher is nil", func(t *testing.T) {
		hNoPub := &admin.DeviceHandler{
			Connections: connRepo,
			Publisher:   nil,
		}

		fv := url.Values{}
		fv.Set("connection_id", conn.ID.String())
		fv.Set("to", "+5511999990001")
		fv.Set("is_template", "true")
		fv.Set("template_name", "static_welcome")

		req := httptest.NewRequest(http.MethodPost, "/admin/devices/test", strings.NewReader(fv.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := hNoPub.RunTest(c)
		if err != nil {
			t.Fatalf("RunTest returned error: %v", err)
		}
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("expected status 503, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Resolves template language fallback from repository when empty in form", func(t *testing.T) {
		tmplRepo := repository.NewWABATemplateRepository(pool)
		hWithTmpl := &admin.DeviceHandler{
			Connections:   connRepo,
			Publisher:     pub,
			TemplatesRepo: tmplRepo,
		}

		tmpl := &repository.WABATemplate{
			WorkspaceID:    ws.ID,
			ConnectionID:   conn.ID,
			MetaTemplateID: "meta-test-tmpl-id-1",
			Name:           "spanish_promo",
			Language:       "es_ES",
			Category:       "MARKETING",
			Status:         "APPROVED",
			Components:     json.RawMessage(`[{"type":"BODY","text":"Hola {{1}}"}]`),
		}
		if _, err := tmplRepo.Create(ctx, tmpl); err != nil {
			t.Fatalf("failed to create template: %v", err)
		}

		fv := url.Values{}
		fv.Set("connection_id", conn.ID.String())
		fv.Set("to", "+5511999990001")
		fv.Set("is_template", "true")
		fv.Set("template_name", "spanish_promo")
		fv.Set("param_1", "Carlos")
		// note: language field is omitted

		req := httptest.NewRequest(http.MethodPost, "/admin/devices/test", strings.NewReader(fv.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: "pergo-active-workspace", Value: ws.ID.String()})
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := hWithTmpl.RunTest(c)
		if err != nil {
			t.Fatalf("RunTest returned error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		qMsg := pub.lastMessage
		if qMsg.TemplateName != "spanish_promo" {
			t.Errorf("expected TemplateName 'spanish_promo', got %q", qMsg.TemplateName)
		}
		if qMsg.Language != "es_ES" {
			t.Errorf("expected Language resolved from repo 'es_ES', got %q", qMsg.Language)
		}
	})
}

// TestDeviceHandler_TestConnectionModal_DynamicTemplateAttributes verifies that
// TestConnectionModal renders data-language, data-components, and preview elements.
func TestDeviceHandler_TestConnectionModal_DynamicTemplateAttributes(t *testing.T) {
	conn := &repository.Connection{
		ID:             uuid.New(),
		WorkspaceID:    uuid.New(),
		Name:           "WABA Cloud Device",
		Channel:        "whatsapp_cloud",
		SenderIdentity: "+15551234567",
		Status:         "connected",
	}

	templates := []repository.WABATemplate{
		{
			Name:       "order_alert",
			Language:   "en_US",
			Components: json.RawMessage(`[{"type":"BODY","text":"Hello {{1}}, your order {{2}} is ready"}]`),
		},
		{
			Name:       "static_announcement",
			Language:   "pt_BR",
			Components: json.RawMessage(`[{"type":"BODY","text":"Bem-vindo à nossa plataforma!"}]`),
		},
	}

	var buf strings.Builder
	comp := pages.TestConnectionModal(conn, templates)
	if err := comp.Render(context.Background(), &buf); err != nil {
		t.Fatalf("failed to render TestConnectionModal: %v", err)
	}

	html := buf.String()

	// 1. Must contain hidden language input
	if !strings.Contains(html, `name="language"`) || !strings.Contains(html, `id="test-template-language"`) {
		t.Errorf("expected form to contain hidden input for language with id test-template-language")
	}

	// 2. Options must contain data-language and data-components
	if !strings.Contains(html, `data-language="en_US"`) {
		t.Errorf("expected template option to contain data-language=\"en_US\"")
	}
	if !strings.Contains(html, `data-components="[{&#34;type&#34;:&#34;BODY&#34;,&#34;text&#34;:&#34;Hello {{1}}, your order {{2}} is ready&#34;}]"`) && !strings.Contains(html, `data-components=`) {
		t.Errorf("expected template option to contain data-components attribute")
	}
	if !strings.Contains(html, `data-language="pt_BR"`) {
		t.Errorf("expected template option to contain data-language=\"pt_BR\"")
	}

	// 3. Must contain container for dynamic variables
	if !strings.Contains(html, `id="test-template-vars"`) {
		t.Errorf("expected modal to contain #test-template-vars container")
	}
	if !strings.Contains(html, `id="test-template-vars-content"`) {
		t.Errorf("expected modal to contain #test-template-vars-content container")
	}

	// 4. Must call showTestTemplatePreview
	if !strings.Contains(html, `showTestTemplatePreview(this)`) && !strings.Contains(html, `showTestTemplatePreview(this.value)`) {
		t.Errorf("expected select to have onchange handler showTestTemplatePreview")
	}
}

