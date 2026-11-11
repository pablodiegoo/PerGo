package telegram

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/client"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/media"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
)

// ConnectionLister lists channel connections across all workspaces.
type ConnectionLister interface {
	ListAll(ctx context.Context) ([]*repository.Connection, error)
}

// InboundEventProcessor processes parsed inbound channel events.
type InboundEventProcessor interface {
	Process(ctx context.Context, ev *inbound.InboundEvent) error
}

// PollerOption customizes a PollerManager.
type PollerOption func(*PollerManager)

// WithDiscoveryInterval sets how often active Telegram connections are discovered from the database.
func WithDiscoveryInterval(d time.Duration) PollerOption {
	return func(m *PollerManager) {
		if d > 0 {
			m.discoveryInterval = d
		}
	}
}

// WithPollTimeoutSeconds sets the long-polling timeout in seconds passed to Telegram getUpdates.
func WithPollTimeoutSeconds(sec int) PollerOption {
	return func(m *PollerManager) {
		if sec >= 0 {
			m.pollTimeoutSec = sec
		}
	}
}

// WithErrorBackoff sets the retry delay when getUpdates returns an error.
func WithErrorBackoff(d time.Duration) PollerOption {
	return func(m *PollerManager) {
		if d > 0 {
			m.errorBackoff = d
		}
	}
}

type connectionWorker struct {
	token  string
	cancel context.CancelFunc
}

// PollerManager manages background long-polling workers for active Telegram connections
// when PerGo runs locally without a public HTTPS webhook URL (e.g. Solo Mode).
type PollerManager struct {
	connLister        ConnectionLister
	botClient         *client.HTTPTelegramBotClient
	processor         InboundEventProcessor
	adapter           *TelegramInboundAdapter
	discoveryInterval time.Duration
	pollTimeoutSec    int
	errorBackoff      time.Duration

	mu      sync.Mutex
	workers map[uuid.UUID]*connectionWorker
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewPollerManager creates a new Telegram PollerManager.
func NewPollerManager(
	connLister ConnectionLister,
	botClient *client.HTTPTelegramBotClient,
	processor InboundEventProcessor,
	downloader media.Downloader,
	opts ...PollerOption,
) *PollerManager {
	if botClient == nil {
		botClient = client.NewTelegramBotClient(nil, "")
	}
	adapter := NewTelegramInboundAdapter(downloader)
	adapter.SetBaseURL(botClient.BaseURL())

	m := &PollerManager{
		connLister:        connLister,
		botClient:         botClient,
		processor:         processor,
		adapter:           adapter,
		discoveryInterval: 10 * time.Second,
		pollTimeoutSec:    25,
		errorBackoff:      2 * time.Second,
		workers:           make(map[uuid.UUID]*connectionWorker),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(m)
		}
	}
	return m
}

// Start launches the background connection discovery and long-polling workers until ctx is cancelled or Stop is called.
func (m *PollerManager) Start(ctx context.Context) {
	if m == nil || m.connLister == nil {
		return
	}
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.mu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.reconcile(runCtx)

		ticker := time.NewTicker(m.discoveryInterval)
		defer ticker.Stop()

		for {
			select {
			case <-runCtx.Done():
				m.stopAllWorkers()
				return
			case <-ticker.C:
				m.reconcile(runCtx)
			}
		}
	}()
}

// Stop cancels all active polling workers and waits for them to exit cleanly.
func (m *PollerManager) Stop() {
	if m == nil {
		return
	}
	m.mu.Lock()
	cancel := m.cancel
	m.cancel = nil
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	m.wg.Wait()
}

func (m *PollerManager) stopAllWorkers() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, w := range m.workers {
		w.cancel()
		delete(m.workers, id)
	}
}

func (m *PollerManager) reconcile(ctx context.Context) {
	conns, err := m.connLister.ListAll(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.WarnContext(ctx, "telegram poller: failed to list connections", "error", err)
		}
		return
	}

	activeSet := make(map[uuid.UUID]struct{})

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, conn := range conns {
		if conn == nil || conn.Channel != "telegram" {
			continue
		}
		if conn.Status != "active" && conn.Status != "connected" {
			continue
		}
		token, secretToken, preparedConn := prepareConnectionForPoller(conn)
		if token == "" {
			continue
		}

		activeSet[conn.ID] = struct{}{}

		if existing, ok := m.workers[conn.ID]; ok {
			if existing.token == token {
				continue
			}
			existing.cancel()
			delete(m.workers, conn.ID)
		}

		workerCtx, workerCancel := context.WithCancel(ctx)
		m.workers[conn.ID] = &connectionWorker{
			token:  token,
			cancel: workerCancel,
		}

		m.wg.Add(1)
		go func(c *repository.Connection, tok, sec string) {
			defer m.wg.Done()
			m.pollConnection(workerCtx, c, tok, sec)
		}(preparedConn, token, secretToken)
	}

	for id, w := range m.workers {
		if _, stillActive := activeSet[id]; !stillActive {
			w.cancel()
			delete(m.workers, id)
		}
	}
}

func prepareConnectionForPoller(conn *repository.Connection) (token string, secretToken string, connCopy *repository.Connection) {
	if len(conn.Credentials) == 0 {
		return "", "", nil
	}

	var cfg telegramConfig
	if err := json.Unmarshal(conn.Credentials, &cfg); err == nil && strings.TrimSpace(cfg.Token) != "" {
		cfg.Token = strings.TrimSpace(cfg.Token)
		if strings.TrimSpace(cfg.SecretToken) == "" {
			cfg.SecretToken = "pergo-local-poller-" + conn.ID.String()
		}
		if strings.TrimSpace(cfg.BotUsername) == "" && strings.TrimSpace(conn.SenderIdentity) != "" {
			cfg.BotUsername = strings.TrimSpace(conn.SenderIdentity)
		}
		credBytes, _ := json.Marshal(cfg)
		cp := *conn
		cp.Credentials = credBytes
		return cfg.Token, cfg.SecretToken, &cp
	}

	// Fallback if credentials were stored as a raw token string
	rawToken := strings.TrimSpace(string(conn.Credentials))
	if rawToken == "" || strings.HasPrefix(rawToken, "{") {
		return "", "", nil
	}
	cfg = telegramConfig{
		Token:       rawToken,
		SecretToken: "pergo-local-poller-" + conn.ID.String(),
		BotUsername: strings.TrimSpace(conn.SenderIdentity),
	}
	credBytes, _ := json.Marshal(cfg)
	cp := *conn
	cp.Credentials = credBytes
	return cfg.Token, cfg.SecretToken, &cp
}

func (m *PollerManager) pollConnection(ctx context.Context, conn *repository.Connection, token, secretToken string) {
	if err := m.botClient.DeleteWebhook(ctx, token); err != nil {
		if ctx.Err() != nil {
			return
		}
		slog.WarnContext(ctx, "telegram poller: deleteWebhook warning before polling", "connection_id", conn.ID, "error", err)
	} else {
		slog.InfoContext(ctx, "telegram poller: active in local long-polling mode", "connection_id", conn.ID, "workspace_id", conn.WorkspaceID, "bot", conn.SenderIdentity)
	}

	headers := map[string]string{
		"X-Telegram-Bot-Api-Secret-Token": secretToken,
	}
	tenantCtx := tenant.WithWorkspaceID(ctx, conn.WorkspaceID)

	var offset int64
	for {
		if ctx.Err() != nil {
			return
		}

		updates, nextOffset, err := m.botClient.GetUpdates(ctx, token, offset, m.pollTimeoutSec)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.WarnContext(ctx, "telegram poller: getUpdates error", "connection_id", conn.ID, "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(m.errorBackoff):
				continue
			}
		}

		for _, rawUpdate := range updates {
			events, parseErr := m.adapter.Parse(tenantCtx, rawUpdate, headers, conn)
			if parseErr != nil {
				slog.WarnContext(ctx, "telegram poller: failed to parse update", "connection_id", conn.ID, "error", parseErr)
				continue
			}
			for _, ev := range events {
				if m.processor != nil {
					if procErr := m.processor.Process(tenantCtx, ev); procErr != nil {
						slog.ErrorContext(ctx, "telegram poller: inbound processor failed", "connection_id", conn.ID, "error", procErr)
					}
				}
			}
		}

		if nextOffset > offset {
			offset = nextOffset
		}
	}
}
