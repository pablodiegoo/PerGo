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

	"github.com/pablojhp.pergo/internal/channel"
	whatsapp "github.com/pablojhp.pergo/internal/channel/whatsapp"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/repository"
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
	db               *sql.DB
	repo             *repository.ConnectionRepository
	registry         *ActiveSession
	dispatchers      *channel.Registry
	waVersion        string
	inboundProcessor *inbound.InboundProcessor
	mu               sync.Mutex
}

// NewManager creates a session manager.
func NewManager(
	db *sql.DB,
	repo *repository.ConnectionRepository,
	registry *ActiveSession,
	dispatchers *channel.Registry,
	waVersion string,
	inboundProcessor *inbound.InboundProcessor,
) *Manager {
	return &Manager{
		db:               db,
		repo:             repo,
		registry:         registry,
		dispatchers:      dispatchers,
		waVersion:        waVersion,
		inboundProcessor: inboundProcessor,
	}
}

// ReconnectAll reconnects all known devices from the database with
// backoff and storm protection (semaphore cap).
// It blocks until all reconnection attempts complete or ctx is cancelled.
func (m *Manager) ReconnectAll(ctx context.Context) error {
	allConns, err := m.repo.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("session manager: list connections: %w", err)
	}

	var devices []*repository.Connection
	for _, conn := range allConns {
		if conn.Channel == "whatsapp" && conn.JID != nil && *conn.JID != "" {
			devices = append(devices, conn)
		}
	}

	slog.Info("session manager: reconnecting devices", "count", len(devices))

	// Semaphore limits concurrent reconnections
	sem := make(chan struct{}, maxConcurrentReconnect)
	var wg sync.WaitGroup

	for _, d := range devices {
		if d.Status == string(DeviceStatusTerminal) {
			slog.Warn("session manager: skipping terminal device",
				"device_id", d.ID,
				"jid", *d.JID,
			)
			continue
		}

		wg.Add(1)
		go func(d *repository.Connection) {
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
					"jid", *d.JID,
				)
				// Update status to disconnected on failure
				_ = m.repo.UpdateStatus(ctx, d.ID, string(DeviceStatusDisconnected))
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
func (m *Manager) reconnectDevice(ctx context.Context, d *repository.Connection) error {
	slog.Info("session manager: reconnecting device",
		"jid", *d.JID,
		"device_id", d.ID,
	)

	cfg := whatsapp.ClientConfig{
		DB:        m.db,
		WAVersion: m.waVersion,
	}
	if d.ProxyURL != nil {
		cfg.ProxyURL = *d.ProxyURL
	}

	wc, err := whatsapp.NewWhatsAppClient(cfg)
	if err != nil {
		return fmt.Errorf("create whatsapp client: %w", err)
	}

	// Set the JID from the persisted device record
	jid, err := parseJID(*d.JID)
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

	// Register session atomically
	m.registry.Add(sess)

	// Register event handler to update recipient_sessions on incoming messages
	wc.Client().AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *waEvents.LoggedOut:
			slog.Warn("session manager: whatsmeow logged out event received, marking device terminal", "device_id", d.ID)
			_ = m.repo.UpdateStatus(context.Background(), d.ID, string(DeviceStatusTerminal))
			cancel()
		case *waEvents.Message:
			if v.Info.IsFromMe {
				return
			}

			senderJID := v.Info.Sender.String()
			ctxBg := context.Background()

			// Download media from WhatsApp CDN (needs active whatsmeow client)
			var mediaBytes []byte
			var mediaType string
			var mediaFilename string
			var mediaCaption string
			hasMedia := false

			if imageMsg := v.Message.GetImageMessage(); imageMsg != nil {
				data, err := wc.Client().Download(ctxBg, imageMsg)
				if err == nil {
					mediaBytes = data
				}
				mediaType = "image"
				hasMedia = true
				if imageMsg.Caption != nil {
					mediaCaption = *imageMsg.Caption
				}
			} else if docMsg := v.Message.GetDocumentMessage(); docMsg != nil {
				data, err := wc.Client().Download(ctxBg, docMsg)
				if err == nil {
					mediaBytes = data
				}
				mediaType = "document"
				hasMedia = true
				if docMsg.FileName != nil {
					mediaFilename = *docMsg.FileName
				}
				if docMsg.Caption != nil {
					mediaCaption = *docMsg.Caption
				}
			} else if audioMsg := v.Message.GetAudioMessage(); audioMsg != nil {
				data, err := wc.Client().Download(ctxBg, audioMsg)
				if err == nil {
					mediaBytes = data
				}
				mediaType = "audio"
				hasMedia = true
			} else if videoMsg := v.Message.GetVideoMessage(); videoMsg != nil {
				data, err := wc.Client().Download(ctxBg, videoMsg)
				if err == nil {
					mediaBytes = data
				}
				mediaType = "video"
				hasMedia = true
				if videoMsg.Caption != nil {
					mediaCaption = *videoMsg.Caption
				}
			}

			// Delegate to processor
			if m.inboundProcessor != nil {
				recipientIdentity := d.SenderIdentity
				if recipientIdentity == "" && d.JID != nil {
					recipientIdentity = *d.JID
				}

				var inboundMedia *inbound.InboundMedia
				if hasMedia {
					inboundMedia = &inbound.InboundMedia{
						Bytes:     mediaBytes,
						MediaType: mediaType,
						Filename:  mediaFilename,
						Caption:   mediaCaption,
					}
				}

				var inboundLocation *inbound.InboundLocation
				if locMsg := v.Message.GetLocationMessage(); locMsg != nil {
					inboundLocation = &inbound.InboundLocation{
						Latitude:  *locMsg.DegreesLatitude,
						Longitude: *locMsg.DegreesLongitude,
						Name:      locMsg.GetName(),
						Address:   locMsg.GetAddress(),
					}
				}

				var inboundContacts []inbound.InboundContact
				if contactMsg := v.Message.GetContactMessage(); contactMsg != nil {
					inboundContacts = append(inboundContacts, inbound.InboundContact{
						Name:  contactMsg.GetDisplayName(),
						Phone: contactMsg.GetVcard(),
					})
				}

				event := &inbound.InboundEvent{
					WorkspaceID:  d.WorkspaceID,
					ConnectionID: d.ID,
					MessageID:    v.Info.ID,
					Channel:      "whatsapp",
					From:         senderJID,
					To:           recipientIdentity,
					Body:         extractWhatsAppBody(v),
					Media:        inboundMedia,
					Location:     inboundLocation,
					Contacts:     inboundContacts,
				}

				_ = m.inboundProcessor.Process(ctxBg, event)
			}
		}
	})

	// Start the client goroutine
	go func() {
		if err := wc.Run(sessionCtx); err != nil && sessionCtx.Err() == nil {
			slog.Error("session manager: device run error",
				"error", err,
				"jid", jid.String(),
			)
		}
		// Update status when goroutine exits
		_ = m.repo.UpdateStatus(context.Background(), d.ID, string(DeviceStatusDisconnected))
		m.registry.Remove(jid)
	}()

	// Update status to connected
	return m.repo.UpdateStatus(ctx, d.ID, string(DeviceStatusConnected))
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

// extractWhatsAppBody pulls the human-readable text from a WhatsApp message.
func extractWhatsAppBody(v *waEvents.Message) string {
	if msgText := v.Message.GetConversation(); msgText != "" {
		return msgText
	}
	if extText := v.Message.GetExtendedTextMessage().GetText(); extText != "" {
		return extText
	}
	if imageMsg := v.Message.GetImageMessage(); imageMsg != nil && imageMsg.Caption != nil {
		return *imageMsg.Caption
	}
	if documentMsg := v.Message.GetDocumentMessage(); documentMsg != nil && documentMsg.Caption != nil {
		return *documentMsg.Caption
	}
	if videoMsg := v.Message.GetVideoMessage(); videoMsg != nil && videoMsg.Caption != nil {
		return *videoMsg.Caption
	}
	return ""
}



