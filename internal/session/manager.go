package session

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types"
	waEvents "go.mau.fi/whatsmeow/types/events"

	"github.com/pablojhp.omnigo/internal/channel"
	whatsapp "github.com/pablojhp.omnigo/internal/channel/whatsapp"
	"github.com/pablojhp.omnigo/internal/platform/storage"
	"github.com/pablojhp.omnigo/internal/repository"
)

const (
	// maxConcurrentReconnect limits how many devices reconnect simultaneously
	// on startup to prevent storming WhatsApp servers.
	maxConcurrentReconnect = 5

	// defaultReconnectBackoff is the base backoff for reconnection attempts.
	defaultReconnectBackoff = 5 * time.Second

	// maxReconnectBackoff caps the exponential backoff.
	maxReconnectBackoff = 5 * time.Minute
)

// Manager coordinates WhatsApp device lifecycle: startup reconnection,
// session registration, and graceful shutdown.
type Manager struct {
	db                  *sql.DB
	repo                *DeviceRepository
	registry            *ActiveSession
	dispatchers         *channel.Registry
	waVersion           string
	recipientSessionRepo *repository.RecipientSessionRepository
	s3Client            *storage.S3Client
	mu                  sync.Mutex
}

// NewManager creates a session manager.
func NewManager(db *sql.DB, repo *DeviceRepository, registry *ActiveSession, dispatchers *channel.Registry, waVersion string, recipientSessionRepo *repository.RecipientSessionRepository, s3Client *storage.S3Client) *Manager {
	return &Manager{
		db:                  db,
		repo:                repo,
		registry:            registry,
		dispatchers:         dispatchers,
		waVersion:           waVersion,
		recipientSessionRepo: recipientSessionRepo,
		s3Client:            s3Client,
	}
}

// ReconnectAll reconnects all known devices from the database with
// backoff and storm protection (semaphore cap).
// It blocks until all reconnection attempts complete or ctx is cancelled.
func (m *Manager) ReconnectAll(ctx context.Context) error {
	devices, err := m.repo.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("session manager: list devices: %w", err)
	}

	slog.Info("session manager: reconnecting devices", "count", len(devices))

	// Semaphore limits concurrent reconnections
	sem := make(chan struct{}, maxConcurrentReconnect)
	var wg sync.WaitGroup

	for _, d := range devices {
		if d.Status == DeviceStatusTerminal {
			slog.Warn("session manager: skipping terminal device",
				"device_id", d.ID,
				"jid", d.JID,
			)
			continue
		}

		wg.Add(1)
		go func(d *Device) {
			defer wg.Done()

			// Add jitter to prevent thundering herd
			jitter := time.Duration(rand.Int64N(int64(defaultReconnectBackoff)))
			select {
			case <-time.After(jitter):
			case <-ctx.Done():
				return
			}

			sem <- struct{}{}
			defer func() { <-sem }()

			if err := m.reconnectDevice(ctx, d); err != nil {
				slog.Error("session manager: failed to reconnect device",
					"error", err,
					"device_id", d.ID,
					"jid", d.JID,
				)
				// Update status to disconnected on failure
				_ = m.repo.UpdateStatus(ctx, d.ID, DeviceStatusDisconnected)
			}
		}(d)
	}

	wg.Wait()
	slog.Info("session manager: reconnection complete",
		"reconnected", m.registry.Len(),
	)
	return nil
}

// reconnectDevice creates a whatsmeow client for a persisted device and
// attempts to connect. On success, it registers the session and dispatcher.
func (m *Manager) reconnectDevice(ctx context.Context, d *Device) error {
	slog.Info("session manager: reconnecting device",
		"jid", d.JID,
		"device_id", d.ID,
	)

	cfg := whatsapp.ClientConfig{
		DB:        m.db,
		WAVersion: m.waVersion,
	}

	wc, err := whatsapp.NewWhatsAppClient(cfg)
	if err != nil {
		return fmt.Errorf("create whatsapp client: %w", err)
	}

	// Set the JID from the persisted device record
	jid, err := parseJID(d.JID)
	if err != nil {
		return fmt.Errorf("parse JID: %w", err)
	}
	wc.SetJID(jid)

	// Create session with cancelable context
	sessionCtx, cancel := context.WithCancel(ctx)

	sess := &Session{
		DeviceID: d.ID.String(),
		JID:      jid,
		Client:   wc,
		Cancel:   cancel,
	}

	// Register session and dispatcher atomically
	m.registry.Add(sess)
	adapter := whatsapp.NewWhatsAppAdapter(wc, m.s3Client)
	m.dispatchers.Register("whatsapp", adapter)

	// Register event handler to update recipient_sessions on incoming messages
	if m.recipientSessionRepo != nil {
		wc.Client().AddEventHandler(func(evt interface{}) {
			switch v := evt.(type) {
			case *waEvents.Message:
				if v.Info.IsFromMe {
					return
				}
				senderJID := v.Info.Sender.String()
				err := m.recipientSessionRepo.Upsert(context.Background(), d.WorkspaceID, senderJID, "whatsapp", time.Now().UTC())
				if err != nil {
					slog.Error("session manager: failed to upsert recipient session",
						"error", err,
						"workspace_id", d.WorkspaceID,
						"sender_jid", senderJID,
					)
				}
			}
		})
	}

	// Start the client goroutine
	go func() {
		if err := wc.Run(sessionCtx); err != nil && sessionCtx.Err() == nil {
			slog.Error("session manager: device run error",
				"error", err,
				"jid", jid.String(),
			)
		}
		// Update status when goroutine exits
		_ = m.repo.UpdateStatus(context.Background(), d.ID, DeviceStatusDisconnected)
		m.registry.Remove(jid)
	}()

	// Update status to connected
	return m.repo.UpdateStatus(ctx, d.ID, DeviceStatusConnected)
}

// parseJID is a helper that parses a JID string.
func parseJID(jid string) (types.JID, error) {
	parsed, err := types.ParseJID(jid)
	if err != nil {
		return types.JID{}, err
	}
	return parsed, nil
}

// StopAll gracefully stops all active sessions.
func (m *Manager) StopAll() {
	slog.Info("session manager: stopping all sessions", "count", m.registry.Len())
	m.registry.StopAll()
}

// ActiveDevices returns a snapshot of all active sessions.
func (m *Manager) ActiveDevices() []*Session {
	return m.registry.All()
}

// calcBackoff computes exponential backoff with jitter.
func calcBackoff(attempt int) time.Duration {
	backoff := float64(defaultReconnectBackoff) * math.Pow(2, float64(attempt))
	if backoff > float64(maxReconnectBackoff) {
		backoff = float64(maxReconnectBackoff)
	}
	// Add 10% jitter
	jitter := backoff * 0.1 * (rand.Float64()*2 - 1)
	return time.Duration(backoff + jitter)
}
