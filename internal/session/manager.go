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

	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go.mau.fi/whatsmeow/types"
	waEvents "go.mau.fi/whatsmeow/types/events"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/channel"
	whatsapp "github.com/pablojhp.pergo/internal/channel/whatsapp"
	"github.com/pablojhp.pergo/internal/platform/audit"
	"github.com/pablojhp.pergo/internal/platform/queue"
	"github.com/pablojhp.pergo/internal/platform/storage"
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
	db                  *sql.DB
	repo                *DeviceRepository
	registry            *ActiveSession
	dispatchers         *channel.Registry
	waVersion           string
	recipientSessionRepo *repository.RecipientSessionRepository
	s3Client            *storage.S3Client
	dedupRepo           *repository.InboundDedupRepository
	publisher           *queue.JetStreamPublisher
	auditWriter         audit.Writer
	wsRepo              *repository.WorkspaceRepository
	mu                  sync.Mutex
}

// NewManager creates a session manager.
func NewManager(
	db *sql.DB,
	repo *DeviceRepository,
	registry *ActiveSession,
	dispatchers *channel.Registry,
	waVersion string,
	recipientSessionRepo *repository.RecipientSessionRepository,
	s3Client *storage.S3Client,
	dedupRepo *repository.InboundDedupRepository,
	publisher *queue.JetStreamPublisher,
	auditWriter audit.Writer,
	wsRepo *repository.WorkspaceRepository,
) *Manager {
	return &Manager{
		db:                  db,
		repo:                repo,
		registry:            registry,
		dispatchers:         dispatchers,
		waVersion:           waVersion,
		recipientSessionRepo: recipientSessionRepo,
		s3Client:            s3Client,
		dedupRepo:           dedupRepo,
		publisher:           publisher,
		auditWriter:         auditWriter,
		wsRepo:              wsRepo,
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

	// Register session atomically
	m.registry.Add(sess)

	// Register event handler to update recipient_sessions on incoming messages
	wc.Client().AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *waEvents.LoggedOut:
			slog.Warn("session manager: whatsmeow logged out event received, marking device terminal", "device_id", d.ID)
			_ = m.repo.UpdateStatus(context.Background(), d.ID, DeviceStatusTerminal)
			cancel()
		case *waEvents.Message:
			if v.Info.IsFromMe {
				return
			}
			senderJID := v.Info.Sender.String()
			ctxBg := context.Background()

			// 1. Recipient Session Tracking
			if m.recipientSessionRepo != nil {
				_ = m.recipientSessionRepo.Upsert(ctxBg, d.WorkspaceID, senderJID, "whatsapp", time.Now().UTC())
			}

			// 2. Deduplication check
			if m.dedupRepo != nil {
				unique, err := m.dedupRepo.InsertAndCheck(ctxBg, d.WorkspaceID, "whatsapp", v.Info.ID)
				if err != nil {
					slog.Error("whatsapp inbound: dedup check failed", "error", err)
					return
				}
				if !unique {
					slog.Info("whatsapp inbound: duplicate message ignored", "message_id", v.Info.ID)
					return
				}
			}

			// 3. Retrieve Workspace PII Opt-In
			var wsOptIn bool
			if m.wsRepo != nil {
				if ws, err := m.wsRepo.GetByID(ctxBg, d.WorkspaceID); err == nil && ws != nil {
					wsOptIn = ws.PIIOptIn
				}
			}

			// 4. Populate payload
			traceID := uuid.New().String()
			inboundEvt := struct {
				Event       string           `json:"event"`
				TraceID     string           `json:"trace_id"`
				MessageID   string           `json:"message_id"`
				Channel     string           `json:"channel"`
				Timestamp   string           `json:"timestamp"`
				WorkspaceID string           `json:"workspace_id"`
				From        string           `json:"from"`
				Body        string           `json:"body,omitempty"`
				Media       *InboundMedia    `json:"media,omitempty"`
				Location    *InboundLocation `json:"location,omitempty"`
				Contacts    []InboundContact `json:"contacts,omitempty"`
			}{
				Event:       "inbound_message",
				TraceID:     traceID,
				MessageID:   v.Info.ID,
				Channel:     "whatsapp",
				Timestamp:   time.Now().UTC().Format(time.RFC3339),
				WorkspaceID: d.WorkspaceID.String(),
				From:        senderJID,
			}

			// Extract Text/Body
			if msgText := v.Message.GetConversation(); msgText != "" {
				inboundEvt.Body = msgText
			} else if extText := v.Message.GetExtendedTextMessage().GetText(); extText != "" {
				inboundEvt.Body = extText
			} else if imageMsg := v.Message.GetImageMessage(); imageMsg != nil && imageMsg.Caption != nil {
				inboundEvt.Body = *imageMsg.Caption
			} else if documentMsg := v.Message.GetDocumentMessage(); documentMsg != nil && documentMsg.Caption != nil {
				inboundEvt.Body = *documentMsg.Caption
			} else if videoMsg := v.Message.GetVideoMessage(); videoMsg != nil && videoMsg.Caption != nil {
				inboundEvt.Body = *videoMsg.Caption
			}

			// Extract Media
			var mediaType string
			var filename string
			var caption string
			var downloadedBytes []byte

			if imageMsg := v.Message.GetImageMessage(); imageMsg != nil {
				mediaType = "image"
				if imageMsg.Caption != nil {
					caption = *imageMsg.Caption
				}
				data, err := wc.Client().Download(ctxBg, imageMsg)
				if err == nil {
					downloadedBytes = data
				}
			} else if docMsg := v.Message.GetDocumentMessage(); docMsg != nil {
				mediaType = "document"
				if docMsg.FileName != nil {
					filename = *docMsg.FileName
				}
				if docMsg.Caption != nil {
					caption = *docMsg.Caption
				}
				data, err := wc.Client().Download(ctxBg, docMsg)
				if err == nil {
					downloadedBytes = data
				}
			} else if audioMsg := v.Message.GetAudioMessage(); audioMsg != nil {
				mediaType = "audio"
				data, err := wc.Client().Download(ctxBg, audioMsg)
				if err == nil {
					downloadedBytes = data
				}
			} else if videoMsg := v.Message.GetVideoMessage(); videoMsg != nil {
				mediaType = "video"
				if videoMsg.Caption != nil {
					caption = *videoMsg.Caption
				}
				data, err := wc.Client().Download(ctxBg, videoMsg)
				if err == nil {
					downloadedBytes = data
				}
			}

			if len(downloadedBytes) > 0 && m.s3Client != nil {
				// Enforce size limit
				if len(downloadedBytes) <= 25*1024*1024 {
					hashKey := hashBytes(downloadedBytes)
					ext := "bin"
					if mediaType == "image" {
						ext = "jpg"
					}
					s3Key := fmt.Sprintf("%s/%s.%s", d.WorkspaceID.String(), hashKey, ext)
					mime := "application/octet-stream"
					if mediaType == "image" {
						mime = "image/jpeg"
					}
					err = m.s3Client.Upload(ctxBg, s3Key, downloadedBytes, mime)
					if err == nil {
						inboundEvt.Media = &InboundMedia{
							MediaURL:  fmt.Sprintf("/media/%s/%s.%s", d.WorkspaceID.String(), hashKey, ext),
							MediaType: mediaType,
							Filename:  filename,
							Caption:   caption,
						}
					}
				}
			}

			// PII Opt-In Check (Location & Contacts)
			if wsOptIn {
				if locMsg := v.Message.GetLocationMessage(); locMsg != nil {
					inboundEvt.Location = &InboundLocation{
						Latitude:  *locMsg.DegreesLatitude,
						Longitude: *locMsg.DegreesLongitude,
						Name:      locMsg.GetName(),
						Address:   locMsg.GetAddress(),
					}
				}
				if contactMsg := v.Message.GetContactMessage(); contactMsg != nil {
					inboundEvt.Contacts = []InboundContact{
						{
							Name:  contactMsg.GetDisplayName(),
							Phone: contactMsg.GetVcard(), // simplistic vcard mapping
						},
					}
				}
			}

			// Drop if empty
			if inboundEvt.Body == "" && inboundEvt.Media == nil && inboundEvt.Location == nil && len(inboundEvt.Contacts) == 0 {
				return
			}

			// Publish to NATS and Audit Log
			if m.publisher != nil {
				eventData, _ := json.Marshal(inboundEvt)
				subject := fmt.Sprintf("inbound.events.%s", d.WorkspaceID.String())
				_ = m.publisher.Publish(ctxBg, subject, eventData, traceID)

				if m.auditWriter != nil {
					_ = m.auditWriter.Write(audit.NewEvent(d.WorkspaceID, traceID, "inbound_message", eventData))
				}
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

func hashBytes(data []byte) string {
	h := sha256.New()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

type InboundMedia struct {
	MediaURL  string `json:"media_url"`
	MediaType string `json:"media_type"`
	Filename  string `json:"filename,omitempty"`
	Caption   string `json:"caption,omitempty"`
}

type InboundLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Name      string  `json:"name,omitempty"`
	Address   string  `json:"address,omitempty"`
}

type InboundContact struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
}
