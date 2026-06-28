// Package main is the composition root for the OmniGo server.
// It wires dependencies, starts the HTTP server, and handles graceful shutdown.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"
	"github.com/nats-io/nats.go"

	"github.com/pablojhp.omnigo/internal/api/handler"
	"github.com/pablojhp.omnigo/internal/api/handler/admin"
	"github.com/pablojhp.omnigo/internal/api/middleware"
	"github.com/pablojhp.omnigo/internal/channel"
	"github.com/pablojhp.omnigo/internal/channel/telegram"
	"github.com/pablojhp.omnigo/internal/channel/whatsapp"
	"github.com/pablojhp.omnigo/internal/config"
	"github.com/pablojhp.omnigo/internal/platform/audit"
	"github.com/pablojhp.omnigo/internal/platform/crypto"
	echosrv "github.com/pablojhp.omnigo/internal/platform/echo"
	"github.com/pablojhp.omnigo/internal/platform/obs"
	"github.com/pablojhp.omnigo/internal/platform/postgres"
	"github.com/pablojhp.omnigo/internal/platform/postgres/tenant"
	"github.com/pablojhp.omnigo/internal/platform/queue"
	"github.com/pablojhp.omnigo/internal/platform/shutdown"
	"github.com/pablojhp.omnigo/internal/platform/storage"
	"github.com/pablojhp.omnigo/internal/repository"
	"github.com/pablojhp.omnigo/internal/session"
	"github.com/pablojhp.omnigo/templates/pages"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	// --- Config from env vars ---
	cfg := config.Load()

	// --- PostgreSQL ---
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to create pgxpool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		slog.Error("failed to create stdlib sql.DB", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := postgres.RunMigrations(db); err != nil {
		slog.Error("migrations failed", "error", err)
		os.Exit(1)
	}
	slog.Info("migrations applied successfully")

	// --- NATS ---
	nc, err := nats.Connect(cfg.NATSUrl)
	if err != nil {
		slog.Error("failed to connect to NATS", "error", err, "url", cfg.NATSUrl)
		os.Exit(1)
	}
	defer nc.Close()
	slog.Info("connected to NATS", "url", cfg.NATSUrl)

	// --- JetStream ---
	stream, err := queue.EnsureStream(ctx, nc)
	if err != nil {
		slog.Error("failed to create JetStream stream", "error", err)
		os.Exit(1)
	}
	publisher := queue.NewJetStreamPublisher(nc)

	// --- Worker (reads from JetStream, logs dispatched messages) ---
	consumer, err := queue.EnsureConsumer(ctx, stream, "worker-1")
	if err != nil {
		slog.Error("failed to create JetStream consumer", "error", err)
		os.Exit(1)
	}
	// --- Rate limiter (per-workspace token bucket) ---
	rateLimiter := middleware.NewRateLimiter(10, 10) // 10 req/s, burst 10
	queueDepth := middleware.NewQueueDepthTracker()

	// --- Cryptography Encryptor & Credentials Repository ---
	kek := cfg.KEKBytes
	if len(kek) != 32 {
		slog.Warn("OMNIGO_KEK_BASE64 is not set or not 32 bytes; using a default development key. DO NOT USE IN PRODUCTION.")
		kek = make([]byte, 32)
		copy(kek, []byte("dev-development-key-32-bytes-kek"))
	}
	encryptor, err := crypto.NewEncryptor(kek)
	if err != nil {
		slog.Error("failed to initialize encryptor", "error", err)
		os.Exit(1)
	}

	// --- S3 Storage Client ---
	s3Client, err := storage.NewS3Client(
		cfg.S3Endpoint,
		cfg.S3Region,
		cfg.S3AccessKey,
		cfg.S3SecretKey,
		cfg.S3Bucket,
		cfg.S3UsePathStyle,
	)
	if err != nil {
		slog.Error("failed to initialize S3 client", "error", err)
		os.Exit(1)
	}

	credentialsRepo := repository.NewCredentialsRepository(pool, encryptor)
	recipientSessionRepo := repository.NewRecipientSessionRepository(pool)
	windowChecker := session.NewWindowChecker(recipientSessionRepo)

	// --- REST Adapters ---
	wabaAdapter := whatsapp.NewWABAAdapter(credentialsRepo, nil, windowChecker, cfg.ExternalURL)
	telegramAdapter := telegram.NewTelegramAdapter(credentialsRepo, nil, s3Client)

	// --- Worker (reads from JetStream, dispatches with retry/TTL/dedup) ---
	sessionRegistry := session.NewActiveSession()
	dispatcherRegistry := channel.NewRegistry(nil) // populated by session manager
	dispatcherRegistry.Register("whatsapp_cloud", wabaAdapter)
	dispatcherRegistry.Register("telegram", telegramAdapter)

	// --- Audit writer ---
	auditWriter := audit.NewWriter(pool, 5000, 2)

	deviceRepo := session.NewDeviceRepository(pool)
	dedupRepo := repository.NewInboundDedupRepository(pool)
	wsRepo := repository.NewWorkspaceRepository(pool)
	sessionManager := session.NewManager(db, deviceRepo, sessionRegistry, dispatcherRegistry, "2.3000.1025000000", recipientSessionRepo, s3Client, dedupRepo, publisher, auditWriter, wsRepo)
	dispatchRepo := repository.NewMessageDispatchRepository(pool)
	worker := queue.NewWorker(ctx, consumer, 5, 60*time.Second, dispatcherRegistry, dispatchRepo, publisher, queueDepth)
	slog.Info("message worker started", "consumer", "worker-1")

	// --- Webhook Worker ---
	webhookDLQRepo := repository.NewWebhookDLQRepository(pool, encryptor)
	webhookWorker, err := queue.NewWebhookWorker(ctx, nc, webhookDLQRepo)
	if err != nil {
		slog.Error("failed to start webhook worker", "error", err)
		os.Exit(1)
	}
	webhookWorker.SetWorkspaceRepository(wsRepo)

	slog.Info("rate limiter configured", "rps", 10, "burst", 10)
	slog.Info("queue depth limit", "max", 1000)

	// --- Debug server (pprof + expvar) ---
	debugSrv := obs.StartDebugServer(net.JoinHostPort("127.0.0.1", cfg.DebugPort))
	slog.Info("debug server started", "addr", debugSrv.Addr())

	// --- Shutdown orchestrator ---
	orch := shutdown.NewOrchestrator()

	// Register cleanup in reverse order of startup.
	// Shutdown order: Echo → debug → audit → worker → NATS → pgxpool → sqlDB
	orch.Register(func() error {
		slog.Info("closing sql.DB")
		return db.Close()
	})
	orch.Register(func() error {
		slog.Info("closing pgxpool")
		pool.Close()
		return nil
	})
	orch.Register(func() error {
		slog.Info("closing NATS connection")
		nc.Close()
		return nil
	})
	// Worker shutdown runs before NATS close — drains the consumer
	orch.Register(func() error {
		slog.Info("stopping webhook worker")
		webhookWorker.Stop()
		return nil
	})
	orch.Register(func() error {
		slog.Info("stopping message worker")
		worker.Stop()
		return nil
	})
	// Session manager stops all device sessions before worker drains
	orch.Register(func() error {
		slog.Info("stopping all WhatsApp sessions")
		sessionManager.StopAll()
		return nil
	})
	orch.Register(func() error {
		slog.Info("flushing audit buffer")
		err := auditWriter.Close()
		slog.Info("audit buffer flushed")
		return err
	})
	orch.Register(func() error {
		slog.Info("shutting down debug server")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return debugSrv.Shutdown(ctx)
	})

	// --- Repositories ---
	apiKeyRepo := repository.NewAPIKeyRepository(pool)
	wabaTemplateRepo := repository.NewWABATemplateRepository(pool)

	wabaTemplateHandler := admin.NewWABATemplateHandler(wabaTemplateRepo, credentialsRepo)

	// --- Echo HTTP server ---
	e := echosrv.New()


	// Middleware stack: RequestID → Trace → Recover → Auth (on protected routes)
	e.Use(middleware.TraceMiddleware())

	// Auth middleware — protects /api/* routes
	e.Use(middleware.AuthMiddleware(apiKeyRepo))

	// Health endpoints
	healthHandler := &handler.HealthHandler{
		Pool: pool,
		NATS: &natsConn{nc: nc},
	}
	healthHandler.RegisterRoutes(e)

	// --- Message handler (POST /messages) ---
	messageHandler := &handler.MessageHandler{
		Publisher:  publisher,
		QueueDepth: queueDepth,
		S3Client:   s3Client,
	}
	messageHandler.RegisterRoutes(e, middleware.RateLimiterMiddleware(rateLimiter))

	// --- Media proxy handler (GET /media/:workspace_id/:hash) ---
	mediaHandler := handler.NewMediaHandler(s3Client)
	e.GET("/media/:workspace_id/:hash", mediaHandler.Handle)

	// --- Telegram Inbound Webhook handler ---
	telegramWebhookHandler := handler.NewTelegramWebhookHandler(wsRepo, credentialsRepo, recipientSessionRepo, dedupRepo, s3Client, publisher, auditWriter)
	e.POST("/webhooks/telegram/:workspace_id", telegramWebhookHandler.Handle)

	// --- WABA Inbound Webhook handler ---
	wabaWebhookHandler := handler.NewWABAWebhookHandler(wsRepo, credentialsRepo, recipientSessionRepo, dedupRepo, s3Client, publisher, auditWriter)
	e.GET("/webhooks/waba/:workspace_id", wabaWebhookHandler.HandleGet)
	e.POST("/webhooks/waba/:workspace_id", wabaWebhookHandler.HandlePost)

	// --- Admin panel routes ---
	// Repositories for admin dashboard
	auditQuerier := audit.NewQuerier(pool)

	// Public admin routes (no session auth required)
	adminPublic := e.Group("/admin")
	adminPublic.GET("/login", func(c *echo.Context) error {
		return admin.LoginPage(c, false)
	})
	adminPublic.POST("/login", func(c *echo.Context) error {
		return admin.LoginPost(c, wsRepo)
	})
	adminPublic.POST("/logout", func(c *echo.Context) error {
		return admin.Logout(c)
	})

	// Protected admin routes (session auth required)
	adminGroup := e.Group("/admin")
	adminGroup.Use(middleware.HTMXMiddleware())
	adminGroup.Use(middleware.SessionAuthMiddleware())

	// Admin dashboard
	dashboardHandler := &admin.DashboardHandler{
		Pool:       pool,
		Workspaces: wsRepo,
		Audit:      auditQuerier,
	}
	adminGroup.GET("/", dashboardHandler.Index)

	// Workspace management routes
	workspaceHandler := &admin.WorkspaceHandler{Repo: wsRepo, APIKeys: apiKeyRepo}
	adminGroup.GET("/workspaces", workspaceHandler.List)
	adminGroup.POST("/workspaces", workspaceHandler.Create)
	adminGroup.GET("/workspaces/new", func(c *echo.Context) error {
		return middleware.Render(c, http.StatusOK, pages.WorkspaceCreateForm())
	})
	adminGroup.GET("/workspaces/:id", workspaceHandler.Detail)
	adminGroup.GET("/workspaces/:id/confirm-delete", workspaceHandler.ConfirmDelete)
	adminGroup.DELETE("/workspaces/:id", workspaceHandler.Delete)

	// API key management routes
	apiKeyHandler := &admin.APIKeyHandler{Repo: apiKeyRepo, Workspaces: wsRepo}
	adminGroup.GET("/workspaces/:id/keys", apiKeyHandler.List)
	adminGroup.POST("/workspaces/:id/keys", apiKeyHandler.Generate)
	adminGroup.GET("/workspaces/:id/keys/new", func(c *echo.Context) error {
		idStr, err := echo.PathParam[string](c, "id")
		if err != nil {
			return c.String(http.StatusBadRequest, "invalid workspace ID")
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return c.String(http.StatusBadRequest, "invalid workspace ID")
		}
		return middleware.Render(c, http.StatusOK, pages.APIKeyGenerateForm(id))
	})
	adminGroup.GET("/workspaces/:id/keys/:key_id/confirm-revoke", apiKeyHandler.ConfirmRevoke)
	adminGroup.DELETE("/workspaces/:id/keys/:key_id", apiKeyHandler.Revoke)

	// Audit log review routes
	auditRepo := repository.NewAuditRepository(pool)
	auditHandler := &admin.AuditHandler{Repo: auditRepo, Workspaces: wsRepo}
	adminGroup.GET("/audit", auditHandler.List)
	adminGroup.GET("/audit/export", auditHandler.ExportCSV)

	// Device management routes (WhatsApp Web pairing & session control)
	deviceHandler := &admin.DeviceHandler{
		Repo:     deviceRepo,
		Sessions: sessionRegistry,
		Manager:  sessionManager,
	}
	adminGroup.GET("/devices", deviceHandler.List)
	adminGroup.GET("/devices/pair-form", deviceHandler.PairForm)
	adminGroup.POST("/devices/pair", deviceHandler.StartPairing)
	adminGroup.GET("/devices/qr", deviceHandler.GetQR)
	adminGroup.DELETE("/devices/:jid", deviceHandler.Disconnect)

	// Telemetry page (system health: sessions, NATS, uptime)
	telemetryHandler := &admin.TelemetryHandler{
		Manager:    sessionManager,
		Sessions:   sessionRegistry,
		QueueDepth: queueDepth,
		NC:         &natsConn{nc: nc},
		StartTime:  time.Now(),
	}
	adminGroup.GET("/telemetry", telemetryHandler.Index)

	// WABA template routes
	adminGroup.GET("/workspaces/:workspace_id/templates", wabaTemplateHandler.List)
	adminGroup.POST("/workspaces/:workspace_id/templates", wabaTemplateHandler.Create)
	adminGroup.GET("/workspaces/:workspace_id/templates/new", wabaTemplateHandler.NewForm)
	adminGroup.POST("/workspaces/:workspace_id/templates/:template_id/sync", wabaTemplateHandler.Sync)
	adminGroup.DELETE("/workspaces/:workspace_id/templates/:template_id", wabaTemplateHandler.Delete)

	// Webhooks & DLQ routes
	webhookHandler := admin.NewWebhookDLQHandler(webhookDLQRepo, wsRepo, publisher)
	adminGroup.GET("/webhooks", webhookHandler.GlobalPage)
	adminGroup.GET("/webhooks/dlq/badge", webhookHandler.GetBadgeCount)
	adminGroup.GET("/webhooks/dlq/:dlq_id/details", webhookHandler.GetDetails)
	adminGroup.POST("/webhooks/dlq/:dlq_id/retry", webhookHandler.RetryDLQ)
	adminGroup.DELETE("/webhooks/dlq/:dlq_id", webhookHandler.DeleteDLQ)

	adminGroup.GET("/workspaces/:workspace_id/webhooks", webhookHandler.Page)
	adminGroup.POST("/workspaces/:workspace_id/webhooks/config", webhookHandler.SaveConfig)
	adminGroup.DELETE("/workspaces/:workspace_id/webhooks/config", webhookHandler.DeleteConfig)

	// Static files
	e.Static("/static", "static")


	// Test route: GET /api/v1/me (returns workspace_id from auth context)
	e.GET("/api/v1/me", func(c *echo.Context) error {
		wsID, ok := tenant.WorkspaceIDFrom(c.Request().Context())
		if !ok {
			return c.String(http.StatusUnauthorized, "missing workspace context")
		}
		return c.JSON(http.StatusOK, map[string]string{
			"workspace_id": wsID.String(),
		})
	})

	// Start HTTP server
	srv := &http.Server{
		Addr:    net.JoinHostPort("", cfg.ServerPort),
		Handler: e,
	}

	// Register Echo shutdown in orchestrator (runs first — stops accepting new requests)
	orch.Register(func() error {
		slog.Info("shutting down HTTP server")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	})

	go func() {
		slog.Info("starting server", "port", cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// --- Graceful shutdown on SIGTERM/SIGINT ---
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	slog.Info("received shutdown signal", "signal", sig)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := orch.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "error", err)
	}

	cancel() // signal background goroutines to exit
	slog.Info("server stopped")
}

// --- helpers ---

// runServer blocks until ctx is cancelled. Used by tests to simulate server lifecycle.
func runServer(ctx context.Context, pool *pgxpool.Pool, db *sql.DB) error {
	_ = pool
	_ = db
	<-ctx.Done()
	return nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// natsConn wraps *nats.Conn to satisfy the handler.NATSConn interface.
type natsConn struct {
	nc *nats.Conn
}

func (c *natsConn) Ping() error {
	if !c.nc.IsConnected() {
		return fmt.Errorf("nats not connected")
	}
	return nil
}

// IsConnected returns true if the NATS connection is active.
// Satisfies admin.NATSStatus for the TelemetryHandler.
func (c *natsConn) IsConnected() bool {
	return c.nc.IsConnected()
}

