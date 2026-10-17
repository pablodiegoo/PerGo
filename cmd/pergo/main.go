// Package main is the composition root for the PerGo server.
// It wires dependencies, starts the HTTP server, and handles graceful shutdown.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/redis/go-redis/v9"

	"github.com/pablojhp.pergo/api"
	"github.com/pablojhp.pergo/internal/api/handler"
	"github.com/pablojhp.pergo/internal/api/handler/admin"
	apipkg "github.com/pablojhp.pergo/internal/api/handler/api"
	"github.com/pablojhp.pergo/internal/api/mcp"
	"github.com/pablojhp.pergo/internal/api/middleware"
	"github.com/pablojhp.pergo/internal/campaign"
	"github.com/pablojhp.pergo/internal/channel"
	"github.com/pablojhp.pergo/internal/channel/email"
	"github.com/pablojhp.pergo/internal/channel/instagram"
	"github.com/pablojhp.pergo/internal/channel/telegram"
	"github.com/pablojhp.pergo/internal/channel/whatsapp"
	"github.com/pablojhp.pergo/internal/chat"
	"github.com/pablojhp.pergo/internal/client"
	"github.com/pablojhp.pergo/internal/config"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/integration/chatwoot"
	"github.com/pablojhp.pergo/internal/integration/typebot"
	"github.com/pablojhp.pergo/internal/media"
	"github.com/pablojhp.pergo/internal/outbound"
	"github.com/pablojhp.pergo/internal/platform/audit"
	"github.com/pablojhp.pergo/internal/platform/crypto"
	echosrv "github.com/pablojhp.pergo/internal/platform/echo"
	"github.com/pablojhp.pergo/internal/platform/obs"
	"github.com/pablojhp.pergo/internal/platform/postgres"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/platform/queue"
	"github.com/pablojhp.pergo/internal/platform/shutdown"
	"github.com/pablojhp.pergo/internal/platform/storage"
	"github.com/pablojhp.pergo/internal/presence"
	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/retention"
	"github.com/pablojhp.pergo/internal/security"
	"github.com/pablojhp.pergo/internal/session"
	"github.com/pablojhp.pergo/internal/webhook"
	"github.com/pablojhp.pergo/templates/layout"
	"github.com/pablojhp.pergo/templates/pages"
)

// CoreDeps holds the shared domain graph constructed for both HTTP server mode
// and `pergo mcp` stdio mode, as well as integration tests.
type CoreDeps struct {
	Config               *config.Config
	Pool                 *pgxpool.Pool
	DB                   *sql.DB
	NC                   *nats.Conn
	JS                   jetstream.JetStream
	NATSHealth           handler.NATSConn
	Publisher            outbound.Publisher
	JetStreamPublisher   *queue.JetStreamPublisher
	KEK                  []byte
	Encryptor            *crypto.Encryptor
	S3Client             *storage.S3Client
	MediaEngine          media.Engine
	RateLimiter          *middleware.RateLimiter
	QueueDepth           *middleware.QueueDepthTracker
	AuditWriter          audit.Writer
	AuditQuerier         *audit.Querier
	WindowChecker        *session.WindowChecker
	VerbsEngine          *webhook.VerbsEngine
	WebhookAllowlist     []string
	WebhookDispatcher    webhook.WebhookDispatcher
	SessionRegistry      *session.ActiveSession
	DispatcherRegistry   *channel.Registry
	SessionManager       *session.Manager
	ChatwootSyncer       *chatwoot.ChatwootSyncer
	TypebotForwarder     *typebot.Forwarder
	InboundRouter        inbound.InboundRouter
	Debouncer            *inbound.Debouncer
	InboundProcessor     *inbound.InboundProcessor
	OutboundProcessor    *outbound.Processor
	ChatEngine           *chat.Engine
	CampaignEngine       campaign.BroadcasterEngine
	MCPServer            *mcp.Server
	WorkspaceRepo        *repository.WorkspaceRepository
	APIKeyRepo           *repository.APIKeyRepository
	ConnectionRepo       *repository.ConnectionRepository
	RecipientSessionRepo *repository.RecipientSessionRepository
	ContactRepo          *repository.ContactRepository
	ChatRepo             *repository.ChatRepository
	AuditRepo            *repository.AuditRepository
	DispatchRepo         *repository.MessageDispatchRepository
	DedupRepo            *repository.InboundDedupRepository
	UserActionLogRepo    *repository.UserActionLogRepository
	WABATemplateRepo     *repository.WABATemplateRepository
	WebhookSubRepo       *repository.WebhookSubscriptionRepository
	WebhookDLQRepo       *repository.WebhookDLQRepository
	CampaignRepo         *repository.CampaignRepository
	TagRepo              *repository.TagRepository
	IntegrationRepo      *repository.IntegrationRepository
	ChatwootMappingRepo  *repository.ChatwootMappingRepository
	TypebotSessionRepo   *repository.TypebotSessionRepository

	ownsDB    bool
	closeOnce sync.Once
	closeErr  error
}

// Close stops active inbound debounce timers and flushes the audit writer.
func (d *CoreDeps) Close() error {
	if d == nil {
		return nil
	}
	d.closeOnce.Do(func() {
		if d.InboundProcessor != nil {
			d.InboundProcessor.Close()
		}
		if d.AuditWriter != nil {
			d.closeErr = d.AuditWriter.Close()
		}
		if d.ownsDB && d.DB != nil {
			_ = d.DB.Close()
		}
	})
	return d.closeErr
}

// CoreOption customizes dependency construction in BootstrapCore.
type CoreOption func(*CoreDeps)

// WithCorePublisher overrides the outbound/event publisher in CoreDeps.
func WithCorePublisher(pub outbound.Publisher) CoreOption {
	return func(d *CoreDeps) {
		d.Publisher = pub
	}
}

// WithCoreJetStream overrides the JetStream context in CoreDeps.
func WithCoreJetStream(js jetstream.JetStream) CoreOption {
	return func(d *CoreDeps) {
		d.JS = js
	}
}

// WithCoreMediaEngine overrides the media engine in CoreDeps.
func WithCoreMediaEngine(me media.Engine) CoreOption {
	return func(d *CoreDeps) {
		d.MediaEngine = me
	}
}

// WithCoreDebouncer overrides the inbound Redis debouncer in CoreDeps.
func WithCoreDebouncer(deb *inbound.Debouncer) CoreOption {
	return func(d *CoreDeps) {
		d.Debouncer = deb
	}
}

// WithCoreWebhookDispatcher overrides the webhook dispatcher in CoreDeps.
func WithCoreWebhookDispatcher(wd webhook.WebhookDispatcher) CoreOption {
	return func(d *CoreDeps) {
		d.WebhookDispatcher = wd
	}
}

// WithCoreQueueDepth overrides the queue depth tracker in CoreDeps.
func WithCoreQueueDepth(qd *middleware.QueueDepthTracker) CoreOption {
	return func(d *CoreDeps) {
		d.QueueDepth = qd
	}
}

// WithCoreNATSHealth overrides the NATS health probe used by /readyz.
func WithCoreNATSHealth(nh handler.NATSConn) CoreOption {
	return func(d *CoreDeps) {
		d.NATSHealth = nh
	}
}

// BootstrapCore initializes and wires the shared core domain graph (PostgreSQL
// repositories, AES-256-GCM encryptor, S3 media engine, Customer Service Window
// checker, webhook dispatcher, OutboundProcessor, InboundProcessor + Debouncer,
// ChatEngine, and MCPServer) for both HTTP server mode and `pergo mcp` stdio mode.
func BootstrapCore(
	ctx context.Context,
	cfg *config.Config,
	pool *pgxpool.Pool,
	db *sql.DB,
	nc *nats.Conn,
	opts ...CoreOption,
) (*CoreDeps, error) {
	if cfg == nil {
		cfg = config.Load()
	}

	deps := &CoreDeps{
		Config: cfg,
		Pool:   pool,
		DB:     db,
		NC:     nc,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(deps)
		}
	}

	if deps.DB == nil && pool != nil {
		sqlDB, err := postgres.NewSQLDB(pool)
		if err != nil {
			return nil, fmt.Errorf("create sql.DB: %w", err)
		}
		deps.DB = sqlDB
		deps.ownsDB = true
	}

	kek := cfg.KEKBytes
	if len(kek) != 32 {
		if cfg.IsProduction() {
			return nil, fmt.Errorf("PERGO_KEK_BASE64 is required and must be 32 bytes in production")
		}
		slog.Warn("PERGO_KEK_BASE64 is not set or not 32 bytes; using a default development key. DO NOT USE IN PRODUCTION.")
		kek = make([]byte, 32)
		copy(kek, []byte("dev-development-key-32-bytes-kek"))
	}
	deps.KEK = kek

	encryptor, err := crypto.NewEncryptor(kek)
	if err != nil {
		return nil, fmt.Errorf("initialize encryptor: %w", err)
	}
	deps.Encryptor = encryptor

	s3Client, err := storage.NewS3Client(
		cfg.S3Endpoint,
		cfg.S3Region,
		cfg.S3AccessKey,
		cfg.S3SecretKey,
		cfg.S3Bucket,
		cfg.S3UsePathStyle,
	)
	if err != nil {
		return nil, fmt.Errorf("initialize S3 client: %w", err)
	}
	deps.S3Client = s3Client
	if deps.MediaEngine == nil {
		deps.MediaEngine = media.NewDefaultEngine(s3Client)
	}

	if deps.RateLimiter == nil {
		deps.RateLimiter = middleware.NewRateLimiter(10, 10)
	}
	if deps.QueueDepth == nil {
		deps.QueueDepth = middleware.NewQueueDepthTracker()
	}

	if nc != nil {
		if deps.JS == nil {
			js, err := jetstream.New(nc)
			if err != nil {
				return nil, fmt.Errorf("initialize JetStream client: %w", err)
			}
			deps.JS = js
		}
		deps.JetStreamPublisher = queue.NewJetStreamPublisher(nc)
		if deps.Publisher == nil {
			deps.Publisher = deps.JetStreamPublisher
		}
	}
	if deps.NATSHealth == nil {
		deps.NATSHealth = &natsConn{nc: nc}
	}

	// --- PostgreSQL Repositories ---
	deps.WorkspaceRepo = repository.NewWorkspaceRepository(pool)
	deps.APIKeyRepo = repository.NewAPIKeyRepository(pool)
	deps.ConnectionRepo = repository.NewConnectionRepository(pool, encryptor)
	deps.RecipientSessionRepo = repository.NewRecipientSessionRepository(pool)
	deps.ContactRepo = repository.NewContactRepository(pool)
	deps.ChatRepo = repository.NewChatRepository(pool)
	deps.AuditRepo = repository.NewAuditRepository(pool)
	deps.DispatchRepo = repository.NewMessageDispatchRepository(pool)
	deps.DedupRepo = repository.NewInboundDedupRepository(pool)
	deps.UserActionLogRepo = repository.NewUserActionLogRepository(pool)
	deps.WABATemplateRepo = repository.NewWABATemplateRepository(pool)
	if pool != nil {
		if err := deps.WABATemplateRepo.LoadCache(ctx); err != nil {
			slog.Warn("failed to warm up WABA template cache", "error", err)
		} else {
			slog.Info("WABA template cache warmed up successfully")
		}
	}
	deps.WebhookSubRepo = repository.NewWebhookSubscriptionRepository(pool, encryptor)
	deps.WebhookDLQRepo = repository.NewWebhookDLQRepository(pool, encryptor)
	deps.CampaignRepo = repository.NewCampaignRepository(pool)
	deps.TagRepo = repository.NewTagRepository(pool)
	deps.IntegrationRepo = repository.NewIntegrationRepository(pool, encryptor)
	deps.ChatwootMappingRepo = repository.NewChatwootMappingRepository(pool)
	deps.TypebotSessionRepo = repository.NewTypebotSessionRepository(pool)

	if pool != nil {
		deps.AuditWriter = audit.NewWriter(pool, 5000, 2)
		deps.AuditQuerier = audit.NewQuerier(pool)
	}

	deps.WindowChecker = session.NewWindowChecker(deps.RecipientSessionRepo)

	// --- Webhook Dispatcher ---
	deps.VerbsEngine = webhook.NewVerbsEngine(deps.Publisher, deps.ContactRepo, deps.UserActionLogRepo, deps.ConnectionRepo)
	if !cfg.IsProduction() || os.Getenv("PERGO_ALLOW_LOCAL_WEBHOOKS") == "true" {
		deps.WebhookAllowlist = []string{"localhost", "127.0.0.1", "::1", "0.0.0.0", "host.docker.internal"}
	}
	if deps.WebhookDispatcher == nil {
		dispatcherHTTPClient := security.NewSafeWebhookClient(security.WithTimeout(10*time.Second), security.WithAllowlist(deps.WebhookAllowlist...))
		deps.WebhookDispatcher = webhook.NewDefaultDispatcher(deps.WebhookSubRepo, deps.WebhookDLQRepo, deps.WorkspaceRepo, dispatcherHTTPClient, deps.VerbsEngine)
	}

	// --- Channel Adapters & Session Manager ---
	wabaAdapter := whatsapp.NewWABAAdapter(deps.ConnectionRepo, nil, deps.WindowChecker, cfg.ExternalURL)
	telegramAdapter := telegram.NewTelegramAdapter(deps.ConnectionRepo, nil, s3Client)
	whatsAppAdapter := whatsapp.NewWhatsAppAdapter(nil, s3Client)
	instagramAdapter := instagram.NewAdapter(deps.ConnectionRepo, nil, cfg.ExternalURL)
	emailProvider := email.NewSMTPProvider(email.SMTPConfig{
		Host:        "localhost",
		Port:        1025,
		FromAddress: "noreply@pergo.dev",
		FromName:    "PerGo Platform",
	})
	emailAdapter := email.NewEmailAdapter(emailProvider)

	deps.SessionRegistry = session.NewActiveSession()
	whatsAppAdapter.SetSessionFinder(deps.SessionRegistry)

	deps.DispatcherRegistry = channel.NewRegistry(nil)
	deps.DispatcherRegistry.Register("whatsapp_cloud", wabaAdapter)
	deps.DispatcherRegistry.Register("telegram", telegramAdapter)
	deps.DispatcherRegistry.Register("whatsapp", whatsAppAdapter)
	deps.DispatcherRegistry.Register("instagram", instagramAdapter)
	deps.DispatcherRegistry.Register("email", emailAdapter)
	deps.DispatcherRegistry.Register("email_smtp", emailAdapter)

	// --- Inbound Router, Debouncer & InboundProcessor ---
	deps.ChatwootSyncer = chatwoot.NewChatwootSyncer(deps.IntegrationRepo, deps.ChatwootMappingRepo, nil)
	deps.TypebotForwarder = typebot.NewForwarder(deps.TypebotSessionRepo, deps.IntegrationRepo, deps.Publisher)
	deps.InboundRouter = inbound.NewDefaultRouter(deps.ChatwootSyncer, deps.TypebotForwarder)

	deps.InboundProcessor = inbound.NewInboundProcessor(
		deps.DedupRepo,
		deps.WorkspaceRepo,
		deps.MediaEngine,
		deps.Publisher,
		deps.AuditWriter,
		deps.RecipientSessionRepo,
		deps.ContactRepo,
		deps.DispatchRepo,
		deps.InboundRouter,
	)
	deps.InboundProcessor.SetChatRepository(deps.ChatRepo)
	deps.InboundProcessor.SetReactionWebhookDispatcher(deps.WebhookDispatcher)

	if deps.Debouncer == nil && cfg.RedisURL != "" {
		redisOpts, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			redisOpts = &redis.Options{Addr: cfg.RedisURL}
		}
		rdb := redis.NewClient(redisOpts)
		ctxPing, cancelPing := context.WithTimeout(ctx, 2*time.Second)
		if err := rdb.Ping(ctxPing).Err(); err == nil {
			deps.Debouncer = inbound.NewDebouncer(rdb)
			slog.Info("inbound debounce buffer connected", "addr", redisOpts.Addr)
		} else {
			slog.Warn("inbound debounce buffer: redis ping failed, operating in bypass mode", "error", err)
		}
		cancelPing()
	}
	if deps.Debouncer != nil {
		deps.InboundProcessor.SetDebouncer(deps.Debouncer)
	}

	deps.SessionManager = session.NewManager(deps.DB, deps.ConnectionRepo, deps.SessionRegistry, deps.DispatcherRegistry, cfg.WAVersion, deps.InboundProcessor)
	if deps.JetStreamPublisher != nil {
		deps.SessionManager.SetPublisher(deps.JetStreamPublisher)
	} else if deps.Publisher != nil {
		deps.SessionManager.SetPublisher(deps.Publisher)
	}

	// --- OutboundProcessor ---
	deps.OutboundProcessor = outbound.NewProcessor(deps.QueueDepth, deps.MediaEngine, deps.ConnectionRepo, deps.Publisher)
	deps.OutboundProcessor.SetWindowChecker(deps.WindowChecker)
	deps.OutboundProcessor.SetTemplateRepository(deps.WABATemplateRepo)
	deps.OutboundProcessor.SetChatRepository(deps.ChatRepo)
	deps.OutboundProcessor.SetContactRepository(deps.ContactRepo)
	if deps.Debouncer != nil {
		deps.OutboundProcessor.SetDebounceCanceller(deps.Debouncer)
	}

	// --- ChatEngine ---
	chatEngineOpts := []chat.Option{
		chat.WithContactRepo(deps.ContactRepo),
		chat.WithConnectionRepo(deps.ConnectionRepo),
		chat.WithWorkspaceRepo(deps.WorkspaceRepo),
		chat.WithAuditRepo(deps.AuditRepo),
		chat.WithMessagePublisher(deps.Publisher),
		chat.WithReactionWebhookDispatcher(deps.WebhookDispatcher),
		chat.WithMessageIngestor(deps.OutboundProcessor),
	}
	if deps.Debouncer != nil {
		chatEngineOpts = append(chatEngineOpts, chat.WithDebounceCanceller(deps.Debouncer))
	}
	deps.ChatEngine = chat.NewEngine(deps.ChatRepo, chatEngineOpts...)

	// --- CampaignEngine ---
	deps.CampaignEngine = campaign.NewBroadcasterEngine(
		deps.CampaignRepo,
		deps.ConnectionRepo,
		deps.DispatchRepo,
		deps.Publisher,
		deps.AuditWriter,
		deps.TagRepo,
	)

	// --- MCPServer ---
	mcpOpts := []mcp.ServerOption{
		mcp.WithWebhookDLQRepo(deps.WebhookDLQRepo),
		mcp.WithChatRepo(deps.ChatRepo),
		mcp.WithChatEngine(deps.ChatEngine),
		mcp.WithWABATemplateRepo(deps.WABATemplateRepo),
		mcp.WithPublisher(deps.Publisher),
		mcp.WithJetStream(deps.JS),
		mcp.WithNATSConn(nc),
	}
	if deps.Debouncer != nil {
		mcpOpts = append(mcpOpts, mcp.WithDebounceCanceller(deps.Debouncer))
	}
	deps.MCPServer = mcp.NewServer(
		deps.WorkspaceRepo,
		deps.ConnectionRepo,
		deps.ContactRepo,
		deps.AuditRepo,
		deps.OutboundProcessor,
		deps.APIKeyRepo,
		deps.WebhookSubRepo,
		deps.SessionManager,
		deps.WebhookDispatcher,
		[]byte(cfg.SessionSecret),
		cfg.ExternalURL,
		mcpOpts...,
	)

	return deps, nil
}

// BuildHTTPRouter assembles the complete *echo.Echo middleware and route tree
// consuming the bootstrapped CoreDeps.
func BuildHTTPRouter(core *CoreDeps) *echo.Echo {
	cfg := core.Config
	if cfg == nil {
		cfg = config.Load()
	}

	wabaTemplateHandler := admin.NewWABATemplateHandler(core.WABATemplateRepo, core.ConnectionRepo)
	userLogsHandler := admin.NewUserLogsHandler(core.UserActionLogRepo)
	chatwootAdminHandler := admin.NewChatwootAdminHandler(core.IntegrationRepo)
	typebotAdminHandler := admin.NewTypebotSettingsHandler(core.IntegrationRepo, core.ConnectionRepo)
	developerHandler := admin.NewDeveloperHandler(core.WorkspaceRepo, core.APIKeyRepo, []byte(cfg.SessionSecret), cfg.ExternalURL)

	e := echosrv.New()

	// Redirect /admin to /admin/ to prevent trailing-slash 404s
	e.GET("/admin", func(c *echo.Context) error {
		return c.Redirect(http.StatusMovedPermanently, "/admin/")
	})

	// Middleware stack: RequestID → Trace → Recover → Auth (on protected routes)
	e.Use(middleware.TraceMiddleware())
	e.Use(middleware.LocaleMiddleware())

	// Auth middleware — protects /api/* routes
	e.Use(middleware.AuthMiddleware(core.APIKeyRepo, cfg.MasterKey))

	// Audit middleware — audits API operations
	e.Use(middleware.AuditMiddleware(core.UserActionLogRepo))

	// Health endpoints
	healthHandler := &handler.HealthHandler{
		Pool: core.Pool,
		NATS: core.NATSHealth,
	}
	healthHandler.RegisterRoutes(e)

	// Documentation endpoints (embedded Scalar at /docs)
	docsHandler := handler.NewDocsHandler()
	docsHandler.RegisterRoutes(e)

	// --- Message handler (POST /messages) ---
	messageHandler := &handler.MessageHandler{
		Ingestor: core.OutboundProcessor,
	}
	messageHandler.RegisterRoutes(e, middleware.RateLimiterMiddleware(core.RateLimiter), middleware.EnforceWorkspaceScope())

	// --- MCP Server (Model Context Protocol) ---
	if core.MCPServer != nil {
		e.Any("/api/mcp/*", echo.WrapHandler(core.MCPServer.SSEServer))

		// --- Universal Agnostic MCP Gateway & Auto-Discovery Endpoints ---
		universalGateway := mcp.NewUniversalGateway(core.MCPServer, cfg.ExternalURL)
		e.Any("/mcp", echo.WrapHandler(universalGateway))
		e.Any("/mcp/*", echo.WrapHandler(universalGateway))
		e.POST("/oauth/register", echo.WrapHandler(http.HandlerFunc(universalGateway.HandleOAuthRegister)))
		e.GET("/oauth/authorize", echo.WrapHandler(http.HandlerFunc(universalGateway.HandleOAuthAuthorize)))
		e.POST("/oauth/token", echo.WrapHandler(http.HandlerFunc(universalGateway.HandleOAuthToken)))
		e.GET("/.well-known/mcp", echo.WrapHandler(http.HandlerFunc(universalGateway.HandleWellKnownMCP)))
		e.GET("/.well-known/mcp.json", echo.WrapHandler(http.HandlerFunc(universalGateway.HandleWellKnownMCP)))
		e.GET("/.well-known/opencode", echo.WrapHandler(http.HandlerFunc(universalGateway.HandleWellKnownOpenCode)))
		e.GET("/.well-known/opencode.json", echo.WrapHandler(http.HandlerFunc(universalGateway.HandleWellKnownOpenCode)))
		e.GET("/.well-known/oauth-protected-resource", echo.WrapHandler(http.HandlerFunc(universalGateway.HandleWellKnownOAuthProtectedResource)))
		e.GET("/.well-known/oauth-authorization-server", echo.WrapHandler(http.HandlerFunc(universalGateway.HandleWellKnownOAuthAuthorizationServer)))
	}

	// --- Media proxy handler (GET /media/:workspace_id/:hash) ---
	mediaHandler := handler.NewMediaHandler(core.S3Client)
	e.GET("/media/:workspace_id/:hash", mediaHandler.Handle)

	// --- Telegram Inbound Webhook handler ---
	telegramWebhookHandler := handler.NewTelegramWebhookHandler(core.ConnectionRepo, core.InboundProcessor, core.MediaEngine)
	e.POST("/webhooks/telegram/:workspace_id", telegramWebhookHandler.Handle)

	// --- WABA Inbound Webhook handler ---
	wabaWebhookHandler := handler.NewWABAWebhookHandler(core.ConnectionRepo, core.InboundProcessor, core.MediaEngine)
	wabaWebhookHandler.SetTemplatesRepo(core.WABATemplateRepo)
	e.GET("/webhooks/waba/:workspace_id", wabaWebhookHandler.HandleGet)
	e.POST("/webhooks/waba/:workspace_id", wabaWebhookHandler.HandlePost)

	// --- Workspace REST API handler ---
	workspaceAPIHandler := apipkg.NewWorkspaceAPIHandler(core.WorkspaceRepo, core.APIKeyRepo)
	workspaceAPIHandler.RegisterRoutes(e, middleware.MasterAuth(cfg))

	// --- Connection & Device REST API handler ---
	wabaMetaClient := client.NewWABAMetaClient(nil, "")
	telegramBotClient := client.NewTelegramBotClient(nil, "")
	connectionAPIHandler := apipkg.NewConnectionAPIHandler(
		core.ConnectionRepo,
		core.SessionManager,
		core.SessionRegistry,
		apipkg.WithWABAMetaClient(wabaMetaClient),
		apipkg.WithWABATemplateRepo(core.WABATemplateRepo),
		apipkg.WithTelegramClient(telegramBotClient),
		apipkg.WithExternalURL(cfg.ExternalURL),
	)
	connectionAPIHandler.RegisterRoutes(e, middleware.EnforceWorkspaceScope())

	// --- WABA Template REST API handler ---
	wabaTemplateAPIHandler := apipkg.NewWABATemplateAPIHandler(core.WABATemplateRepo, core.ConnectionRepo, wabaMetaClient)
	wabaTemplateAPIHandler.RegisterRoutes(e, middleware.EnforceWorkspaceScope())

	// --- Webhook Subscription REST API handler ---
	webhookSubscriptionAPIHandler := apipkg.NewWebhookSubscriptionAPIHandler(core.WebhookSubRepo, apipkg.WithSubscriptionAllowlist(core.WebhookAllowlist...))
	webhookSubscriptionAPIHandler.RegisterRoutes(e, middleware.EnforceWorkspaceScope())

	// --- Public Chat REST API handler (sole owner of /api/v1/chats/*) ---
	chatAPIHandler := apipkg.NewChatAPIHandler(
		core.ChatRepo,
		apipkg.WithChatEngine(core.ChatEngine),
		apipkg.WithChatContactRepo(core.ContactRepo),
		apipkg.WithChatConnectionRepo(core.ConnectionRepo),
		apipkg.WithChatWorkspaceRepo(core.WorkspaceRepo),
		apipkg.WithChatAuditRepo(core.AuditRepo),
		apipkg.WithChatMessagePublisher(core.Publisher),
		apipkg.WithChatWebhookDispatcher(core.WebhookDispatcher),
		apipkg.WithChatMessageIngestor(core.OutboundProcessor),
		apipkg.WithChatDebounceCanceller(core.Debouncer),
	)
	chatAPIHandler.RegisterRoutes(e, middleware.EnforceWorkspaceScope())

	// --- WABA Meta Flows Data Exchange endpoint ---
	flowDataExchangeHandler := handler.NewFlowDataExchangeHandler(core.ConnectionRepo, core.WorkspaceRepo)
	e.POST("/api/v1/waba/flows/data-exchange", flowDataExchangeHandler.HandleFlowDataExchange)

	// --- Chatwoot Inbound Webhook handler ---
	chatwootWebhookHandler := handler.NewChatwootWebhookHandler(core.Pool, core.ChatwootMappingRepo, core.ContactRepo, core.Publisher)
	e.POST("/api/integrations/chatwoot", chatwootWebhookHandler.Handle)

	// --- Typebot Inbound Webhook handler ---
	typebotWebhookHandler := handler.NewTypebotWebhookHandler(core.Pool, core.Publisher)
	e.POST("/api/integrations/typebot", typebotWebhookHandler.Handle)

	// --- Email Tracking & Webhook handler ---
	emailTrackingHandler := inbound.NewEmailTrackingHandler(string(core.KEK), core.DispatchRepo)
	e.GET("/v1/webhooks/email/open", emailTrackingHandler.HandleOpen)
	e.GET("/v1/webhooks/email/click", emailTrackingHandler.HandleClick)
	e.POST("/v1/webhooks/email/ses", emailTrackingHandler.HandleSESWebhook)

	// --- Landing Page ---
	e.GET("/", func(c *echo.Context) error {
		return middleware.Render(c, http.StatusOK, pages.Landing())
	}, middleware.AgentDiscoveryMiddleware(), middleware.ContentNegotiationMiddleware(api.LLMsTxt))

	// --- Admin panel routes ---
	ssoHandler := admin.NewSSOHandler(core.WorkspaceRepo, []byte(cfg.SessionSecret))

	adminPublic := e.Group("/admin")
	adminPublic.GET("/login", func(c *echo.Context) error {
		return admin.LoginPage(c, false)
	})
	adminPublic.GET("/login/", func(c *echo.Context) error {
		return admin.LoginPage(c, false)
	})
	adminPublic.POST("/login", func(c *echo.Context) error {
		return admin.LoginPost(c, core.WorkspaceRepo, cfg.AdminPassword)
	})
	adminPublic.POST("/login/", func(c *echo.Context) error {
		return admin.LoginPost(c, core.WorkspaceRepo, cfg.AdminPassword)
	})
	adminPublic.POST("/logout", func(c *echo.Context) error {
		return admin.Logout(c)
	})
	adminPublic.GET("/sso", func(c *echo.Context) error {
		return ssoHandler.HandleSSO(c)
	})
	adminPublic.GET("/sso/", func(c *echo.Context) error {
		return ssoHandler.HandleSSO(c)
	})
	adminPublic.POST("/locale", admin.SetLocale)
	adminPublic.POST("/locale/", admin.SetLocale)

	// Protected admin routes (session auth or proxy auth required)
	adminGroup := e.Group("/admin")
	adminGroup.Use(middleware.HTMXMiddleware())
	adminGroup.Use(middleware.InternalProxyAuthMiddleware(cfg.MasterKey))
	adminGroup.Use(middleware.SessionAuthMiddleware())
	adminGroup.Use(middleware.DashboardAuditMiddleware(core.UserActionLogRepo))
	adminGroup.Use(middleware.ActiveWorkspaceMiddleware(core.WorkspaceRepo))
	adminGroup.Use(middleware.EnforceWorkspaceScope())

	// Admin dashboard
	dashboardHandler := &admin.DashboardHandler{
		Pool:        core.Pool,
		Workspaces:  core.WorkspaceRepo,
		Audit:       core.AuditQuerier,
		APIKeys:     core.APIKeyRepo,
		Connections: core.ConnectionRepo,
		Publisher:   core.JetStreamPublisher,
	}
	adminGroup.GET("/", dashboardHandler.Index)
	adminGroup.POST("/webhook/simulate", dashboardHandler.SimulateWebhook)
	adminGroup.POST("/workspaces/active", dashboardHandler.SelectWorkspace)
	adminGroup.GET("/workspaces/selector", dashboardHandler.WorkspaceSelector)

	// Workspace management routes
	workspaceHandler := &admin.WorkspaceHandler{
		Repo:        core.WorkspaceRepo,
		APIKeys:     core.APIKeyRepo,
		ExternalURL: cfg.ExternalURL,
	}
	adminGroup.GET("/workspaces", workspaceHandler.ActiveWorkspace)
	adminGroup.GET("/workspace", workspaceHandler.ActiveWorkspace)
	adminGroup.POST("/workspaces", workspaceHandler.Create)
	adminGroup.GET("/workspaces/new", func(c *echo.Context) error {
		onboarding := c.QueryParam("onboarding") == "true"
		form := pages.WorkspaceCreateForm(onboarding)
		if middleware.IsHTMX(c) {
			return middleware.Render(c, http.StatusOK, form)
		}
		return middleware.Render(c, http.StatusOK, layout.Base("New Workspace", form))
	})
	adminGroup.GET("/workspaces/:id", workspaceHandler.Detail)
	adminGroup.GET("/workspace/:id", workspaceHandler.Detail)
	adminGroup.GET("/workspaces/:id/confirm-delete", workspaceHandler.ConfirmDelete)
	adminGroup.DELETE("/workspaces/:id", workspaceHandler.Delete)
	adminGroup.GET("/workspaces/:id/webhook-secret", workspaceHandler.GetWebhookSecret)
	adminGroup.POST("/workspaces/:id/webhook-secret", workspaceHandler.GenerateWebhookSecret)
	adminGroup.POST("/workspaces/:id/flow-webhook-url", workspaceHandler.SetFlowWebhookURL)
	adminGroup.POST("/workspace/:id/flow-webhook-url", workspaceHandler.SetFlowWebhookURL)

	// API key management routes
	apiKeyHandler := &admin.APIKeyHandler{Repo: core.APIKeyRepo, Workspaces: core.WorkspaceRepo}
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
	auditHandler := &admin.AuditHandler{Repo: core.AuditRepo, Workspaces: core.WorkspaceRepo}
	adminGroup.GET("/audit", auditHandler.Redirect)
	adminGroup.GET("/logs", func(c *echo.Context) error {
		return c.Redirect(http.StatusMovedPermanently, "/admin/logs/outbound")
	})
	adminGroup.GET("/audit/inbound", auditHandler.ListInbound)
	adminGroup.GET("/logs/inbound", auditHandler.ListInbound)
	adminGroup.GET("/audit/inbound/export", auditHandler.ExportInboundCSV)
	adminGroup.GET("/logs/inbound/export", auditHandler.ExportInboundCSV)
	adminGroup.GET("/audit/outbound", auditHandler.ListOutbound)
	adminGroup.GET("/logs/outbound", auditHandler.ListOutbound)
	adminGroup.GET("/audit/outbound/export", auditHandler.ExportOutboundCSV)
	adminGroup.GET("/logs/outbound/export", auditHandler.ExportOutboundCSV)

	// Inbox routes (backed by ChatEngine & OutboundProcessor)
	presenceTracker := presence.NewTracker(15 * time.Second)
	inboxHandler := &admin.InboxHandler{
		Repo:              core.AuditRepo,
		ChatRepo:          core.ChatRepo,
		Sessions:          core.RecipientSessionRepo,
		Workspaces:        core.WorkspaceRepo,
		Connections:       core.ConnectionRepo,
		Publisher:         core.Publisher,
		Templates:         core.WABATemplateRepo,
		ContactRepo:       core.ContactRepo,
		UserActionLogs:    core.UserActionLogRepo,
		Presence:          presenceTracker,
		WebhookDispatcher: core.WebhookDispatcher,
		ChatEngine:        core.ChatEngine,
		Ingestor:          core.OutboundProcessor,
		Debouncer:         core.Debouncer,
	}
	adminGroup.GET("/inbox", inboxHandler.View)
	adminGroup.GET("/inbox/conversations/poll", inboxHandler.PollConversations)
	adminGroup.GET("/inbox/chat", inboxHandler.ChatPanel)
	adminGroup.GET("/inbox/presence", inboxHandler.PresenceStream)
	adminGroup.POST("/inbox/presence", inboxHandler.PresenceHeartbeat)
	adminGroup.POST("/inbox/assign", inboxHandler.AssignChat)
	adminGroup.POST("/inbox/tags/add", inboxHandler.AddChatTag)
	adminGroup.POST("/inbox/tags/remove", inboxHandler.RemoveChatTag)
	adminGroup.POST("/inbox/status", inboxHandler.UpdateChatStatus)
	adminGroup.GET("/inbox/messages", inboxHandler.PollMessages)
	adminGroup.POST("/inbox/send", inboxHandler.SendMessage)
	adminGroup.POST("/inbox/notes", inboxHandler.CreateNote)
	adminGroup.POST("/inbox/reactions", inboxHandler.ToggleReaction)
	adminGroup.GET("/inbox/new-message-modal", inboxHandler.NewMessageModal)
	adminGroup.POST("/inbox/new-message-send", inboxHandler.NewMessageSend)
	adminGroup.GET("/contacts/search", inboxHandler.SearchContacts)
	adminGroup.GET("/inbox/contacts/search", inboxHandler.SearchContacts)
	adminGroup.POST("/contacts/merge", inboxHandler.MergeContacts)
	adminGroup.POST("/inbox/contacts/merge", inboxHandler.MergeContacts)
	adminGroup.POST("/contacts/:id/toggle-bot", inboxHandler.ToggleBot)
	adminGroup.POST("/inbox/contacts/:id/toggle-bot", inboxHandler.ToggleBot)
	adminGroup.POST("/inbox/chats/:id/enable-ai", inboxHandler.EnableChatAI)
	adminGroup.POST("/inbox/chat_enable_ai", inboxHandler.EnableChatAI)
	adminGroup.POST("/inbox/summarize", inboxHandler.SummarizeChat)
	adminGroup.POST("/inbox/chat/summarize", inboxHandler.SummarizeChat)

	// Device/Connection management routes
	deviceHandler := &admin.DeviceHandler{
		Sessions:      core.SessionRegistry,
		Manager:       core.SessionManager,
		Connections:   core.ConnectionRepo,
		Publisher:     core.Publisher,
		NC:            core.NC,
		TemplatesRepo: core.WABATemplateRepo,
		ExternalURL:   cfg.ExternalURL,
		ContactRepo:   core.ContactRepo,
	}
	adminGroup.GET("/devices", deviceHandler.List)
	adminGroup.GET("/connections", deviceHandler.List)
	adminGroup.GET("/devices/pair-form", deviceHandler.PairForm)
	adminGroup.POST("/devices/pair", deviceHandler.StartPairing)
	adminGroup.GET("/devices/qr", deviceHandler.GetQR)
	adminGroup.DELETE("/devices/:id", deviceHandler.Disconnect)
	adminGroup.POST("/devices/create", deviceHandler.Create)
	adminGroup.POST("/devices/:id/slug", deviceHandler.UpdateSlug)
	adminGroup.GET("/devices/test", deviceHandler.TestForm)
	adminGroup.POST("/devices/test", deviceHandler.RunTest)
	adminGroup.GET("/devices/test/ws", deviceHandler.WS)
	adminGroup.GET("/devices/flow-key", deviceHandler.FlowKey)
	adminGroup.GET("/connections/flow-key", deviceHandler.FlowKey)

	// Telemetry page (system health: sessions, NATS, uptime)
	var natsStatus admin.NATSStatus = &natsConn{nc: core.NC}
	if ns, ok := core.NATSHealth.(admin.NATSStatus); ok {
		natsStatus = ns
	}
	telemetryHandler := &admin.TelemetryHandler{
		Manager:    core.SessionManager,
		Sessions:   core.SessionRegistry,
		QueueDepth: core.QueueDepth,
		NC:         natsStatus,
		StartTime:  time.Now(),
	}
	adminGroup.GET("/telemetry", telemetryHandler.Index)

	// WABA template routes (flat admin routes)
	adminGroup.GET("/templates", wabaTemplateHandler.List)
	adminGroup.POST("/templates", wabaTemplateHandler.Create)
	adminGroup.GET("/templates/new", wabaTemplateHandler.NewForm)
	adminGroup.POST("/templates/:template_id/sync", wabaTemplateHandler.Sync)
	adminGroup.DELETE("/templates/:template_id", wabaTemplateHandler.Delete)
	adminGroup.POST("/templates/preview", wabaTemplateHandler.Preview)

	// Legacy WABA template routes (303 redirects / backward compatibility)
	adminGroup.GET("/workspaces/:workspace_id/templates", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/templates")
	})
	adminGroup.GET("/workspaces/:workspace_id/templates/new", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/templates/new")
	})
	adminGroup.POST("/workspaces/:workspace_id/templates", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/templates")
	})
	adminGroup.POST("/workspaces/:workspace_id/templates/:template_id/sync", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/templates")
	})
	adminGroup.DELETE("/workspaces/:workspace_id/templates/:template_id", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/templates")
	})

	// Developer Console & Sandbox routes (flat admin routes)
	adminGroup.GET("/developers", developerHandler.GetPortal)
	adminGroup.POST("/developers/keys", developerHandler.CreateAPIKey)
	adminGroup.DELETE("/developers/keys/:key_id", developerHandler.RevokeAPIKey)
	adminGroup.POST("/developers/webhook-secret/rotate", developerHandler.RotateWebhookSecret)
	adminGroup.POST("/developers/sandbox/test", developerHandler.SandboxTest)
	adminGroup.POST("/developers/sso-generate", developerHandler.GenerateSSO)

	// Headless CPaaS & Developer Integration routes (flat admin routes / backward compatibility)
	adminGroup.GET("/integrations", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/integrations/headless")
	})
	adminGroup.GET("/integrations/headless", developerHandler.GetPortal)
	adminGroup.POST("/integrations/headless/sso-generate", developerHandler.GenerateSSO)

	// Chatwoot integration routes (flat admin routes)
	adminGroup.GET("/integrations/chatwoot", chatwootAdminHandler.GetSettings)
	adminGroup.POST("/integrations/chatwoot", chatwootAdminHandler.PostSettings)

	// Typebot integration routes (flat admin routes)
	adminGroup.GET("/integrations/typebot", typebotAdminHandler.GetSettings)
	adminGroup.POST("/integrations/typebot", typebotAdminHandler.PostSettings)

	// Legacy integration routes (303 redirects / backward compatibility)
	adminGroup.GET("/workspaces/:workspace_id/integrations", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/integrations/headless")
	})
	adminGroup.GET("/workspaces/:workspace_id/integrations/headless", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/integrations/headless")
	})
	adminGroup.POST("/workspaces/:workspace_id/integrations/headless/sso-generate", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/integrations/headless")
	})
	adminGroup.GET("/workspaces/:workspace_id/integrations/chatwoot", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/integrations/chatwoot")
	})
	adminGroup.POST("/workspaces/:workspace_id/integrations/chatwoot", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/integrations/chatwoot")
	})
	adminGroup.GET("/workspaces/:workspace_id/integrations/typebot", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/integrations/typebot")
	})
	adminGroup.POST("/workspaces/:workspace_id/integrations/typebot", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/integrations/typebot")
	})

	// Webhooks & DLQ routes (flat admin routes)
	webhookHandler := admin.NewWebhookDLQHandler(core.WebhookDLQRepo, core.WebhookSubRepo, core.WorkspaceRepo, core.JetStreamPublisher)
	adminGroup.GET("/webhooks", webhookHandler.Page)
	adminGroup.GET("/webhooks/subscriptions/new", webhookHandler.GetSubscriptionNewForm)
	adminGroup.GET("/webhooks/subscriptions/:subscription_id/edit", webhookHandler.GetSubscriptionEditForm)
	adminGroup.GET("/webhooks/subscriptions/:subscription_id/rotate-form", webhookHandler.GetRotateSecretForm)
	adminGroup.POST("/webhooks/subscriptions", webhookHandler.CreateSubscription)
	adminGroup.POST("/webhooks/subscriptions/:subscription_id", webhookHandler.UpdateSubscription)
	adminGroup.POST("/webhooks/subscriptions/:subscription_id/rotate", webhookHandler.RotateSubscriptionSecret)
	adminGroup.POST("/webhooks/subscriptions/:subscription_id/ping", webhookHandler.PingSubscription)
	adminGroup.DELETE("/webhooks/subscriptions/:subscription_id", webhookHandler.DeleteSubscription)
	adminGroup.GET("/webhooks/subscriptions/:subscription_id/test-form", webhookHandler.GetSubscriptionTestForm)
	adminGroup.POST("/webhooks/subscriptions/:subscription_id/test", webhookHandler.TestSubscription)
	adminGroup.GET("/webhooks/dlq/badge", webhookHandler.GetBadgeCount)
	adminGroup.GET("/webhooks/dlq/:dlq_id/details", webhookHandler.GetDetails)
	adminGroup.POST("/webhooks/dlq/:dlq_id/retry", webhookHandler.RetryDLQ)
	adminGroup.DELETE("/webhooks/dlq/:dlq_id", webhookHandler.DeleteDLQ)

	// Legacy Webhooks routes (303 redirects / backward compatibility)
	adminGroup.GET("/workspaces/:workspace_id/webhooks", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/webhooks")
	})
	adminGroup.GET("/workspaces/:workspace_id/webhooks/subscriptions/new", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/webhooks/subscriptions/new")
	})
	adminGroup.GET("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id/edit", func(c *echo.Context) error {
		id, _ := echo.PathParam[string](c, "subscription_id")
		return c.Redirect(http.StatusSeeOther, "/admin/webhooks/subscriptions/"+id+"/edit")
	})
	adminGroup.GET("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id/rotate-form", func(c *echo.Context) error {
		id, _ := echo.PathParam[string](c, "subscription_id")
		return c.Redirect(http.StatusSeeOther, "/admin/webhooks/subscriptions/"+id+"/rotate-form")
	})
	adminGroup.POST("/workspaces/:workspace_id/webhooks/subscriptions", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/webhooks")
	})
	adminGroup.POST("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/webhooks")
	})
	adminGroup.POST("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id/rotate", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/webhooks")
	})
	adminGroup.POST("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id/ping", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/webhooks")
	})
	adminGroup.DELETE("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/webhooks")
	})
	adminGroup.GET("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id/test-form", func(c *echo.Context) error {
		id, _ := echo.PathParam[string](c, "subscription_id")
		return c.Redirect(http.StatusSeeOther, "/admin/webhooks/subscriptions/"+id+"/test-form")
	})
	adminGroup.POST("/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id/test", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/webhooks")
	})

	// User action logs routes
	adminGroup.GET("/logs/actions", userLogsHandler.List)
	adminGroup.GET("/logs/actions/:id/metadata", userLogsHandler.GetMetadata)

	// Tags & Contact Import/Export routes (flat admin routes)
	tagAdminHandler := admin.NewTagAdminHandler(core.TagRepo, core.ContactRepo, core.WorkspaceRepo)
	adminGroup.GET("/tags", tagAdminHandler.Page)
	adminGroup.POST("/tags", tagAdminHandler.CreateTag)
	adminGroup.DELETE("/tags/:id", tagAdminHandler.DeleteTag)
	adminGroup.POST("/contacts/import", tagAdminHandler.ImportContactsCSV)
	adminGroup.GET("/contacts/export", tagAdminHandler.ExportContactsCSV)

	// Legacy Tags & Contact routes (303 redirects / backward compatibility)
	adminGroup.GET("/workspaces/:workspace_id/tags", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/tags")
	})
	adminGroup.POST("/workspaces/:workspace_id/tags", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/tags")
	})
	adminGroup.DELETE("/workspaces/:workspace_id/tags/:id", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/tags")
	})
	adminGroup.POST("/workspaces/:workspace_id/contacts/import", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/tags")
	})
	adminGroup.GET("/workspaces/:workspace_id/contacts/export", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/contacts/export")
	})

	// Campaigns routes (flat admin routes)
	campaignHandler := admin.NewCampaignHandler(core.CampaignRepo, core.WABATemplateRepo, core.ConnectionRepo, core.TagRepo, core.JetStreamPublisher, core.CampaignEngine)
	adminGroup.GET("/campaigns", campaignHandler.List)
	adminGroup.GET("/campaigns/new", campaignHandler.NewForm)
	adminGroup.POST("/campaigns/upload", campaignHandler.UploadCSV)
	adminGroup.POST("/campaigns", campaignHandler.Create)
	adminGroup.GET("/campaigns/:id/row", campaignHandler.GetRow)
	adminGroup.GET("/campaigns/:id/skipped/download", campaignHandler.DownloadSkipped)
	adminGroup.POST("/campaigns/:id/start", campaignHandler.Start)
	adminGroup.POST("/campaigns/:id/pause", campaignHandler.Pause)
	adminGroup.POST("/campaigns/:id/resume", campaignHandler.Resume)
	adminGroup.POST("/campaigns/:id/cancel", campaignHandler.Cancel)
	adminGroup.DELETE("/campaigns/:id", campaignHandler.Delete)

	// Legacy Campaigns routes (303 redirects / backward compatibility)
	adminGroup.GET("/workspaces/:workspace_id/campaigns", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/campaigns")
	})
	adminGroup.GET("/workspaces/:workspace_id/campaigns/new", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/campaigns/new")
	})
	adminGroup.POST("/workspaces/:workspace_id/campaigns/upload", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/campaigns")
	})
	adminGroup.POST("/workspaces/:workspace_id/campaigns", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/campaigns")
	})
	adminGroup.GET("/workspaces/:workspace_id/campaigns/:id/row", func(c *echo.Context) error {
		id, _ := echo.PathParam[string](c, "id")
		return c.Redirect(http.StatusSeeOther, "/admin/campaigns/"+id+"/row")
	})
	adminGroup.GET("/workspaces/:workspace_id/campaigns/:id/skipped/download", func(c *echo.Context) error {
		id, _ := echo.PathParam[string](c, "id")
		return c.Redirect(http.StatusSeeOther, "/admin/campaigns/"+id+"/skipped/download")
	})
	adminGroup.POST("/workspaces/:workspace_id/campaigns/:id/start", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/campaigns")
	})
	adminGroup.POST("/workspaces/:workspace_id/campaigns/:id/pause", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/campaigns")
	})
	adminGroup.POST("/workspaces/:workspace_id/campaigns/:id/resume", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/campaigns")
	})
	adminGroup.POST("/workspaces/:workspace_id/campaigns/:id/cancel", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/campaigns")
	})
	adminGroup.DELETE("/workspaces/:workspace_id/campaigns/:id", func(c *echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/admin/campaigns")
	})

	// Campaign REST API routes (v1)
	v1Group := e.Group("/api/v1")
	v1Group.Use(middleware.EnforceWorkspaceScope())
	v1Group.POST("/campaigns", campaignHandler.APICreate)
	v1Group.GET("/campaigns", campaignHandler.APIList)
	v1Group.GET("/campaigns/:id", campaignHandler.APIGet)
	v1Group.POST("/campaigns/:id/start", campaignHandler.APIStart)
	v1Group.POST("/campaigns/:id/pause", campaignHandler.APIPause)
	v1Group.POST("/campaigns/:id/resume", campaignHandler.APIResume)
	v1Group.POST("/campaigns/:id/cancel", campaignHandler.APICancel)
	v1Group.POST("/workspaces/:workspace_id/campaigns", campaignHandler.APICreate)
	v1Group.GET("/workspaces/:workspace_id/campaigns", campaignHandler.APIList)
	v1Group.GET("/workspaces/:workspace_id/campaigns/:id", campaignHandler.APIGet)
	v1Group.POST("/workspaces/:workspace_id/campaigns/:id/start", campaignHandler.APIStart)
	v1Group.POST("/workspaces/:workspace_id/campaigns/:id/pause", campaignHandler.APIPause)
	v1Group.POST("/workspaces/:workspace_id/campaigns/:id/resume", campaignHandler.APIResume)
	v1Group.POST("/workspaces/:workspace_id/campaigns/:id/cancel", campaignHandler.APICancel)

	// Tag & Contact API routes (v1)
	v1Group.GET("/tags", tagAdminHandler.ListTags)
	v1Group.POST("/tags", tagAdminHandler.CreateTag)
	v1Group.DELETE("/tags/:id", tagAdminHandler.DeleteTag)
	v1Group.POST("/contacts/import", tagAdminHandler.ImportContactsCSV)
	v1Group.GET("/contacts/export", tagAdminHandler.ExportContactsCSV)
	v1Group.GET("/workspaces/:workspace_id/tags", tagAdminHandler.ListTags)
	v1Group.POST("/workspaces/:workspace_id/tags", tagAdminHandler.CreateTag)
	v1Group.DELETE("/workspaces/:workspace_id/tags/:id", tagAdminHandler.DeleteTag)
	v1Group.POST("/workspaces/:workspace_id/contacts/:contact_id/tags/:tag_id", tagAdminHandler.AddContactTag)
	v1Group.DELETE("/workspaces/:workspace_id/contacts/:contact_id/tags/:tag_id", tagAdminHandler.RemoveContactTag)
	v1Group.POST("/workspaces/:workspace_id/contacts/import", tagAdminHandler.ImportContactsCSV)
	v1Group.GET("/workspaces/:workspace_id/contacts/export", tagAdminHandler.ExportContactsCSV)

	// Webhook Secret & Flow Webhook API routes (v1)
	v1Group.POST("/workspaces/webhook-secret", workspaceHandler.GenerateWebhookSecret)
	v1Group.POST("/workspaces/:workspace_id/webhook-secret", workspaceHandler.GenerateWebhookSecret)
	v1Group.GET("/workspaces/webhook-secret", workspaceHandler.GetWebhookSecret)
	v1Group.GET("/workspaces/:workspace_id/webhook-secret", workspaceHandler.GetWebhookSecret)
	v1Group.POST("/workspaces/flow-webhook-url", workspaceHandler.SetFlowWebhookURL)
	v1Group.POST("/workspaces/:workspace_id/flow-webhook-url", workspaceHandler.SetFlowWebhookURL)

	// Static files
	e.Static("/static", "static")

	// Test route: GET /api/v1/me (returns workspace_id from auth context)
	meHandler := func(c *echo.Context) error {
		wsID, ok := tenant.WorkspaceIDFrom(c.Request().Context())
		if !ok {
			return c.String(http.StatusUnauthorized, "missing workspace context")
		}
		return c.JSON(http.StatusOK, map[string]string{
			"workspace_id": wsID.String(),
		})
	}
	v1Group.GET("/me", meHandler)

	return e
}

func runMCPStdio(ctx context.Context, cfg *config.Config, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("create pgxpool: %w", err)
	}
	defer pool.Close()

	db, err := postgres.NewSQLDB(pool)
	if err != nil {
		return fmt.Errorf("create sql.DB: %w", err)
	}
	defer db.Close()

	nc, err := nats.Connect(cfg.NATSUrl)
	if err != nil {
		return fmt.Errorf("connect to NATS: %w", err)
	}
	defer nc.Close()

	core, err := BootstrapCore(ctx, cfg, pool, db, nc)
	if err != nil {
		return fmt.Errorf("bootstrap core dependencies: %w", err)
	}
	defer func() { _ = core.Close() }()

	slog.Info("starting MCP server in stdio mode")
	return mcp.RunStdio(ctx, core.MCPServer, core.APIKeyRepo, args, stdin, stdout, stderr)
}

func runHealthcheck(port string) int {
	if port == "" {
		port = "8080"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil || resp.StatusCode != http.StatusOK {
		return 1
	}
	_ = resp.Body.Close()
	return 0
}

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-healthcheck" || os.Args[1] == "--healthcheck") {
		os.Exit(runHealthcheck(os.Getenv("PERGO_SERVER_PORT")))
	}

	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		slog.SetDefault(slog.New(obs.NewRedactingHandler(slog.NewJSONHandler(os.Stderr, nil))))
		cfg := config.Load()
		if err := cfg.Validate(); err != nil {
			slog.Error("invalid configuration", "error", err)
			os.Exit(1)
		}
		if err := runMCPStdio(context.Background(), cfg, os.Args[2:], os.Stdin, os.Stdout, os.Stderr); err != nil {
			slog.Error("MCP stdio server execution failed", "error", err)
			os.Exit(1)
		}
		return
	}

	slog.SetDefault(slog.New(obs.NewRedactingHandler(slog.NewJSONHandler(os.Stdout, nil))))

	// --- Config from env vars ---
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

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

	stream, err := queue.EnsureStream(ctx, nc)
	if err != nil {
		slog.Error("failed to create JetStream stream", "error", err)
		os.Exit(1)
	}
	consumer, err := queue.EnsureConsumer(ctx, stream, "worker-1")
	if err != nil {
		slog.Error("failed to create JetStream consumer", "error", err)
		os.Exit(1)
	}

	// --- Bootstrap Shared Core Dependencies ---
	core, err := BootstrapCore(ctx, cfg, pool, db, nc)
	if err != nil {
		slog.Error("failed to bootstrap core dependencies", "error", err)
		os.Exit(1)
	}

	if ws, err := core.WorkspaceRepo.EnsureWorkspace(ctx, "Agora"); err != nil {
		slog.Warn("could not ensure default workspace on startup", "error", err)
	} else {
		slog.Info("default workspace ready", "workspace_id", ws.ID, "name", ws.Name)
	}

	go func() {
		if err := core.SessionManager.ReconnectAll(ctx); err != nil {
			slog.Error("session manager: startup reconnect failed", "error", err)
		}
	}()

	orchestrator := queue.NewDispatchOrchestrator(
		core.DispatcherRegistry,
		core.DispatchRepo,
		core.JetStreamPublisher,
		core.QueueDepth,
		core.AuditWriter,
		core.ContactRepo,
		5,
		60*time.Second,
	)
	orchestrator.SetContactRepository(core.ContactRepo)
	orchestrator.SetRecipientSessionRepository(core.RecipientSessionRepo)
	worker := queue.NewWorker(ctx, consumer, orchestrator)
	slog.Info("message worker started", "consumer", "worker-1")

	// --- Campaign Worker & Scheduler ---
	campStream, err := queue.EnsureCampaignStream(ctx, nc)
	if err != nil {
		slog.Error("failed to create JetStream campaign stream", "error", err)
		os.Exit(1)
	}
	campConsumer, err := queue.EnsureCampaignConsumer(ctx, campStream, "campaign-worker-1")
	if err != nil {
		slog.Error("failed to create JetStream campaign consumer", "error", err)
		os.Exit(1)
	}
	campaignWorker := queue.NewCampaignWorker(ctx, campConsumer, core.CampaignEngine)
	slog.Info("campaign worker started", "consumer", "campaign-worker-1")

	campaignScheduler := queue.NewCampaignScheduler(core.CampaignEngine)
	go campaignScheduler.Run(ctx)
	slog.Info("campaign scheduler started", "interval", "5s")

	// --- Webhook Worker ---
	webhookWorker, err := queue.NewWebhookWorker(ctx, nc, core.WebhookDispatcher, core.WebhookSubRepo)
	if err != nil {
		slog.Error("failed to start webhook worker", "error", err)
		os.Exit(1)
	}
	webhookWorker.SetWorkspaceRepository(core.WorkspaceRepo)

	// --- Session Expiration Ticker ---
	sessionTicker := session.NewSessionTicker(core.RecipientSessionRepo, core.JetStreamPublisher)
	go sessionTicker.Run(ctx)
	slog.Info("session expiration ticker started", "interval", "5m")

	// --- LGPD Data Retention & Media Purge Daemon ---
	retentionPurger := retention.NewPurger(core.WorkspaceRepo, core.AuditRepo, core.MediaEngine)
	retentionDaemon := retention.NewDaemon(retentionPurger, 1*time.Hour)
	go retentionDaemon.Run(ctx)
	slog.Info("data retention daemon started", "interval", "1h")

	slog.Info("rate limiter configured", "rps", 10, "burst", 10)
	slog.Info("queue depth limit", "max", 1000)
	if cfg.AdminPassword == "pergo-dev-2026" {
		slog.Warn("admin panel running with default password 'pergo-dev-2026'. Change PERGO_ADMIN_PASSWORD in production.")
	} else {
		slog.Info("admin panel password configured from environment")
	}

	// --- Debug server (pprof + expvar) ---
	debugSrv, err := obs.StartDebugServer(net.JoinHostPort("127.0.0.1", cfg.DebugPort))
	if err != nil {
		slog.Warn("debug server unavailable (port in use?), continuing without pprof",
			"addr", net.JoinHostPort("127.0.0.1", cfg.DebugPort),
			"error", err)
	} else {
		slog.Info("debug server started", "addr", debugSrv.Addr())
	}

	// --- Shutdown orchestrator ---
	orch := shutdown.NewOrchestrator()

	// Register cleanup in reverse order of startup.
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
	orch.Register(func() error {
		slog.Info("stopping webhook worker")
		webhookWorker.Stop()
		return nil
	})
	orch.Register(func() error {
		slog.Info("stopping campaign worker")
		campaignWorker.Stop()
		return nil
	})
	orch.Register(func() error {
		slog.Info("stopping message worker")
		worker.Stop()
		return nil
	})
	orch.Register(func() error {
		slog.Info("stopping data retention daemon")
		retentionDaemon.Stop()
		return nil
	})
	orch.Register(func() error {
		slog.Info("stopping all WhatsApp sessions")
		core.SessionManager.StopAll()
		return nil
	})
	orch.Register(func() error {
		slog.Info("closing core dependencies and flushing audit buffer")
		return core.Close()
	})
	if debugSrv != nil {
		orch.Register(func() error {
			slog.Info("shutting down debug server")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return debugSrv.Shutdown(ctx)
		})
	}

	// --- Echo HTTP server ---
	e := BuildHTTPRouter(core)

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

// runServer boots the shared CoreDeps and HTTP router on an ephemeral listener
// and blocks until ctx is cancelled, performing graceful shutdown on exit.
func runServer(ctx context.Context, pool *pgxpool.Pool, db *sql.DB) error {
	cfg := config.Load()
	core, err := BootstrapCore(ctx, cfg, pool, db, nil)
	if err != nil {
		return err
	}
	defer func() { _ = core.Close() }()

	e := BuildHTTPRouter(core)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: e}
	go func() {
		_ = srv.Serve(ln)
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	return core.Close()
}

// natsConn wraps *nats.Conn to satisfy the handler.NATSConn and admin.NATSStatus interfaces.
type natsConn struct {
	nc *nats.Conn
}

func (c *natsConn) Ping() error {
	if c == nil || c.nc == nil || !c.nc.IsConnected() {
		return fmt.Errorf("nats not connected")
	}
	return nil
}

// IsConnected returns true if the NATS connection is active.
// Satisfies admin.NATSStatus for the TelemetryHandler.
func (c *natsConn) IsConnected() bool {
	return c != nil && c.nc != nil && c.nc.IsConnected()
}
