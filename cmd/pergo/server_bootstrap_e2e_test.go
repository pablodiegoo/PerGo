package main_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"
	"github.com/pablojhp.pergo/internal/api/handler"
	"github.com/pablojhp.pergo/internal/api/handler/admin"
	mw "github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/platform/audit"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	echosrv "github.com/pablojhp.pergo/internal/platform/echo"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type bootstrapNoopNATS struct{}

func (n *bootstrapNoopNATS) Ping() error { return nil }

// setupBootstrapE2EServer wires the core server routes identically to main.go
// for end-to-end integration verification against a seeded database.
func setupBootstrapE2EServer(t *testing.T, pool *pgxpool.Pool, adminPassword string) (*echo.Echo, *repository.WorkspaceRepository) {
	t.Helper()

	kek := make([]byte, 32)
	copy(kek, []byte("dev-development-key-32-bytes-kek"))
	encryptor, err := crypto.NewEncryptor(kek)
	require.NoError(t, err)

	wsRepo := repository.NewWorkspaceRepository(pool)
	apiKeyRepo := repository.NewAPIKeyRepository(pool)
	connRepo := repository.NewConnectionRepository(pool, encryptor)
	tmplRepo := repository.NewWABATemplateRepository(pool)
	auditRepo := repository.NewAuditRepository(pool)
	auditQuerier := audit.NewQuerier(pool)
	recipientSessionRepo := repository.NewRecipientSessionRepository(pool)
	contactRepo := repository.NewContactRepository(pool)
	userActionLogRepo := repository.NewUserActionLogRepository(pool)

	e := echosrv.New()

	// Redirect /admin to /admin/ to prevent trailing-slash 404s (as in main.go:486)
	e.GET("/admin", func(c *echo.Context) error {
		return c.Redirect(http.StatusMovedPermanently, "/admin/")
	})

	// Health endpoints
	healthHandler := &handler.HealthHandler{
		Pool: pool,
		NATS: &bootstrapNoopNATS{},
	}
	healthHandler.RegisterRoutes(e)

	// Documentation endpoints (Scalar portal at /docs)
	docsHandler := handler.NewDocsHandler()
	docsHandler.RegisterRoutes(e)

	// Public admin routes (login/logout)
	adminPublic := e.Group("/admin")
	adminPublic.GET("/login", func(c *echo.Context) error {
		return admin.LoginPage(c, false)
	})
	adminPublic.GET("/login/", func(c *echo.Context) error {
		return admin.LoginPage(c, false)
	})
	adminPublic.POST("/login", func(c *echo.Context) error {
		return admin.LoginPost(c, wsRepo, adminPassword)
	})
	adminPublic.POST("/login/", func(c *echo.Context) error {
		return admin.LoginPost(c, wsRepo, adminPassword)
	})
	adminPublic.POST("/logout", func(c *echo.Context) error {
		return admin.Logout(c)
	})

	// Protected admin routes (session auth + active workspace middleware)
	adminGroup := e.Group("/admin")
	adminGroup.Use(mw.HTMXMiddleware())
	adminGroup.Use(mw.SessionAuthMiddleware())
	adminGroup.Use(mw.DashboardAuditMiddleware(userActionLogRepo))
	adminGroup.Use(mw.ActiveWorkspaceMiddleware(wsRepo))

	dashboardHandler := &admin.DashboardHandler{
		Pool:        pool,
		Workspaces:  wsRepo,
		Audit:       auditQuerier,
		APIKeys:     apiKeyRepo,
		Connections: connRepo,
	}
	adminGroup.GET("/", dashboardHandler.Index)

	inboxHandler := &admin.InboxHandler{
		Repo:           auditRepo,
		Sessions:       recipientSessionRepo,
		Workspaces:     wsRepo,
		Connections:    connRepo,
		Templates:      tmplRepo,
		ContactRepo:    contactRepo,
		UserActionLogs: userActionLogRepo,
	}
	adminGroup.GET("/inbox", inboxHandler.View)

	return e, wsRepo
}

// TestServerBootstrapE2E_SeedAndCoreEndpoints implements the Seam 2 integration check:
// 1. Executes cmd/pergo-seed to verify successful deterministic database seeding.
// 2. Starts the PerGo server routing stack and validates HTTP 200 OK across:
//    - /healthz (liveness probe)
//    - /docs (Scalar interactive documentation)
//    - /admin (dashboard after authentication)
//    - /admin/inbox (omnichannel inbox after authentication)
func TestServerBootstrapE2E_SeedAndCoreEndpoints(t *testing.T) {
	root := findRepoRoot(t)

	dbURL := os.Getenv("PERGO_DATABASE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("PERGO_TEST_DSN")
	}
	if dbURL == "" {
		t.Skip("No PostgreSQL database URL configured; skipping bootstrap E2E integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("cannot connect to PostgreSQL at %s: %v", dbURL, err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("PostgreSQL ping failed at %s: %v", dbURL, err)
	}

	// 1. Run migrations to ensure up-to-date schema
	sqlDB, err := postgres.NewSQLDB(pool)
	require.NoError(t, err)
	defer sqlDB.Close()
	require.NoError(t, postgres.RunMigrations(sqlDB))

	// 2. Execute cmd/pergo-seed harness
	workspaceName := "Bootstrap E2E Workspace"
	adminPassword := "pergo-dev-2026"
	t.Setenv("PERGO_DATABASE_URL", dbURL)
	t.Setenv("PERGO_WORKSPACE", workspaceName)
	t.Setenv("PERGO_ADMIN_PASSWORD", adminPassword)
	t.Setenv("PERGO_SERVER_PORT", "8080")

	seedCmd := exec.Command("go", "run", "./cmd/pergo-seed")
	seedCmd.Dir = root
	seedCmd.Env = append(os.Environ(),
		"PERGO_DATABASE_URL="+dbURL,
		"PERGO_WORKSPACE="+workspaceName,
		"PERGO_ADMIN_PASSWORD="+adminPassword,
	)

	var seedStdout, seedStderr bytes.Buffer
	seedCmd.Stdout = &seedStdout
	seedCmd.Stderr = &seedStderr

	err = seedCmd.Run()
	require.NoError(t, err, "cmd/pergo-seed failed: %v\nStdout:\n%s\nStderr:\n%s", err, seedStdout.String(), seedStderr.String())

	seedOut := seedStdout.String()
	assert.Contains(t, seedOut, "✓ Seeded Workspace:")
	assert.Contains(t, seedOut, workspaceName)
	assert.Contains(t, seedOut, "✓ Channels/Connections:")
	assert.Contains(t, seedOut, "✓ Contacts Seeded:")
	assert.Contains(t, seedOut, "✓ PII Safety:")

	// 3. Boot server routing stack
	e, wsRepo := setupBootstrapE2EServer(t, pool, adminPassword)
	srv := httptest.NewServer(e)
	defer srv.Close()

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)

	client := &http.Client{
		Jar:     jar,
		Timeout: 10 * time.Second,
	}

	// --- Check 1: GET /healthz (HTTP 200 OK) ---
	t.Run("GET /healthz serves 200 OK", func(t *testing.T) {
		resp, err := client.Get(srv.URL + "/healthz")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		assert.Equal(t, "ok", strings.TrimSpace(buf.String()))
	})

	// --- Check 2: GET /docs (HTTP 200 OK) ---
	t.Run("GET /docs serves 200 OK with Scalar documentation", func(t *testing.T) {
		resp, err := client.Get(srv.URL + "/docs")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		body := buf.String()
		assert.Contains(t, body, "<title>PerGo API Reference</title>")
		assert.Contains(t, body, "/docs/scalar.js")
	})

	// --- Check 3: GET /admin unauthenticated redirects to /admin/login ---
	t.Run("GET /admin redirects unauthenticated client to /admin/login", func(t *testing.T) {
		unauthClient := &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse // don't follow redirect
			},
		}

		resp, err := unauthClient.Get(srv.URL + "/admin")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusMovedPermanently, resp.StatusCode)
		assert.Equal(t, "/admin/", resp.Header.Get("Location"))

		respSlash, err := unauthClient.Get(srv.URL + "/admin/")
		require.NoError(t, err)
		defer respSlash.Body.Close()

		assert.Equal(t, http.StatusFound, respSlash.StatusCode)
		assert.Contains(t, respSlash.Header.Get("Location"), "/admin/login")

		// /admin/login serves 200 OK with form
		loginResp, err := client.Get(srv.URL + "/admin/login")
		require.NoError(t, err)
		defer loginResp.Body.Close()
		assert.Equal(t, http.StatusOK, loginResp.StatusCode)
	})

	// --- Check 4: Authenticate client via POST /admin/login ---
	t.Run("POST /admin/login authenticates successfully", func(t *testing.T) {
		form := url.Values{}
		form.Set("password", adminPassword)

		resp, err := client.PostForm(srv.URL+"/admin/login", form)
		require.NoError(t, err)
		defer resp.Body.Close()

		// Login redirects to /admin/ with session cookie set in jar
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		// Set active workspace to the newly seeded workspace
		ws, err := wsRepo.GetByName(ctx, workspaceName)
		if err == nil && ws != nil {
			u, _ := url.Parse(srv.URL)
			jar.SetCookies(u, []*http.Cookie{
				{
					Name:  mw.ActiveWorkspaceCookieName,
					Value: ws.ID.String(),
					Path:  "/admin",
				},
			})
		}
	})

	// --- Check 5: GET /admin serves HTTP 200 OK when authenticated ---
	t.Run("GET /admin serves 200 OK after authentication", func(t *testing.T) {
		resp, err := client.Get(srv.URL + "/admin")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		body := buf.String()

		// Must render dashboard with navigation links and active connections
		assert.Contains(t, body, "Dashboard")
		assert.Contains(t, body, "/admin/inbox")
		assert.Contains(t, body, "/admin/connections")
		assert.Contains(t, body, "WhatsApp Cloud Primary")
	})

	// --- Check 6: GET /admin/inbox serves HTTP 200 OK when authenticated ---
	t.Run("GET /admin/inbox serves 200 OK with seeded conversations", func(t *testing.T) {
		resp, err := client.Get(srv.URL + "/admin/inbox")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		body := buf.String()

		// Inbox view elements and seeded contacts
		assert.Contains(t, body, "Inbox")
		assert.Contains(t, body, "Ana Clara Silva")
		assert.Contains(t, body, "Carlos Eduardo Santos")
	})
}
