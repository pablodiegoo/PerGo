package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	waEvents "go.mau.fi/whatsmeow/types/events"

	"github.com/pablojhp.pergo/internal/channel"
	whatsapp "github.com/pablojhp.pergo/internal/channel/whatsapp"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/inbound"
	"github.com/pablojhp.pergo/internal/repository"
)

// ConnectionStatusEvent represents a standardized connection lifecycle event emitted to webhooks.
type ConnectionStatusEvent struct {
	Event          string    `json:"event"`
	TraceID        string    `json:"trace_id,omitempty"`
	WorkspaceID    uuid.UUID `json:"workspace_id"`
	ConnectionID   uuid.UUID `json:"connection_id"`
	Channel        string    `json:"channel"`
	SenderIdentity string    `json:"sender_identity"`
	Status         string    `json:"status"`
	Timestamp      string    `json:"timestamp"`
}

type SessionState string

const (
	StateInitializing SessionState = "initializing"
	StateConnecting   SessionState = "connecting"
	StateConnected    SessionState = "connected"
	StateDisconnected SessionState = "disconnected"
	StateReconnecting SessionState = "reconnecting"
	StateTerminal     SessionState = "terminal"
)

type SessionHealthInfo struct {
	ConnectionID uuid.UUID    `json:"connection_id"`
	State        SessionState `json:"state"`
	LastSeen     time.Time    `json:"last_seen"`
	LastError    string       `json:"last_error,omitempty"`
}

type WhatsAppClientInterface interface {
	SetJID(jid types.JID)
	JID() types.JID
	Run(ctx context.Context) error
	Connect() error
	Disconnect()
	GetQRChannel(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error)
}

type ClientFactory interface {
	CreateClient(cfg whatsapp.ClientConfig) (WhatsAppClientInterface, error)
}

type defaultClientFactory struct{}

func (f *defaultClientFactory) CreateClient(cfg whatsapp.ClientConfig) (WhatsAppClientInterface, error) {
	return whatsapp.NewWhatsAppClient(cfg)
}

const (
	// maxConcurrentReconnect limits how many devices reconnect simultaneously
	// on startup to prevent storming WhatsApp servers.
	maxConcurrentReconnect = 5

	// defaultReconnectBackoff is the base backoff for reconnection attempts.
	defaultReconnectBackoff = 5 * time.Second

	// maxReconnectBackoff caps the exponential backoff.
	maxReconnectBackoff = 5 * time.Minute
)

// InboundEventProcessor defines the processor interface for inbound events.
type InboundEventProcessor interface {
	Process(ctx context.Context, ev *inbound.InboundEvent) error
}

// Manager coordinates WhatsApp device lifecycle: startup reconnection,
// session registration, and graceful shutdown.
type Manager struct {
	db               *sql.DB
	repo             *repository.ConnectionRepository
	registry         *ActiveSession
	dispatchers      *channel.Registry
	waVersion        string
	inboundProcessor InboundEventProcessor
	clientFactory    ClientFactory
	publisher        Publisher
	qrTimeout        time.Duration
	pairingCancels   map[uuid.UUID]context.CancelFunc
	healthMap        map[uuid.UUID]*SessionHealthInfo
	pairingSessions  map[string]*PairingSession
	mu               sync.Mutex
}

// NewManager creates a session manager.

func (m *Manager) SetInboundProcessor(p InboundEventProcessor) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inboundProcessor = p
}

func (m *Manager) SetClientFactory(f ClientFactory) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clientFactory = f
}

func (m *Manager) SetPublisher(p Publisher) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.publisher = p
}

func (m *Manager) SetQRTimeout(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.qrTimeout = d
}

// EmitStatusEvent updates session health and publishes a connection.status event to JetStream if publisher is configured.
func (m *Manager) EmitStatusEvent(ctx context.Context, wsID uuid.UUID, connID uuid.UUID, channelName, senderIdentity, status string) error {
	m.mu.Lock()
	if m.healthMap == nil {
		m.healthMap = make(map[uuid.UUID]*SessionHealthInfo)
	}
	m.healthMap[connID] = &SessionHealthInfo{
		ConnectionID: connID,
		State:        SessionState(status),
		LastSeen:     time.Now().UTC(),
	}
	pub := m.publisher
	m.mu.Unlock()

	if pub == nil {
		return nil
	}

	traceID := uuid.New().String()
	evt := ConnectionStatusEvent{
		Event:          "connection.status",
		TraceID:        traceID,
		WorkspaceID:    wsID,
		ConnectionID:   connID,
		Channel:        channelName,
		SenderIdentity: senderIdentity,
		Status:         status,
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
	}

	data, err := json.Marshal(evt)
	if err != nil {
		slog.Error("session manager: failed to marshal connection.status event", "error", err, "connection_id", connID)
		return err
	}

	if err := pub.Publish(ctx, "webhooks.events", data, traceID); err != nil {
		slog.Error("session manager: failed to publish connection.status event", "error", err, "connection_id", connID, "trace_id", traceID)
		return err
	}

	slog.Info("session manager: published connection.status event", "connection_id", connID, "status", status, "trace_id", traceID)
	return nil
}

func (m *Manager) SessionHealth(connectionID uuid.UUID) (*SessionHealthInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if info, ok := m.healthMap[connectionID]; ok {
		return info, nil
	}
	return &SessionHealthInfo{
		ConnectionID: connectionID,
		State:        StateDisconnected,
		LastSeen:     time.Time{},
	}, nil
}

func (m *Manager) ListSessions() []*SessionHealthInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := make([]*SessionHealthInfo, 0, len(m.healthMap))
	for _, info := range m.healthMap {
		list = append(list, info)
	}
	return list
}

func (m *Manager) SubscribeQR(key string) (<-chan QREvent, func()) {
	ch, unsub, _ := m.SubscribeQRForWorkspace(uuid.Nil, key)
	return ch, unsub
}

func (m *Manager) SubscribeQRForWorkspace(wsID uuid.UUID, key string) (<-chan QREvent, func(), bool) {
	m.mu.Lock()
	ps, ok := m.pairingSessions[key]
	m.mu.Unlock()

	if !ok || (wsID != uuid.Nil && ps.WorkspaceID() != wsID) {
		ch := make(chan QREvent, 1)
		ch <- QREvent{
			Status:  "error",
			Message: "No active pairing session",
		}
		close(ch)
		return ch, func() {}, false
	}
	ch, unsub := ps.Subscribe()
	return ch, unsub, true
}

func (m *Manager) GetPairingState(key string) (*QREvent, bool) {
	return m.GetPairingStateForWorkspace(uuid.Nil, key)
}

func (m *Manager) GetPairingStateForWorkspace(wsID uuid.UUID, key string) (*QREvent, bool) {
	m.mu.Lock()
	ps, ok := m.pairingSessions[key]
	m.mu.Unlock()

	if !ok {
		return nil, false
	}
	if wsID != uuid.Nil && ps.WorkspaceID() != wsID {
		return nil, false
	}
	ps.mu.RLock()
	evt := ps.latestEvent
	ps.mu.RUnlock()
	return &evt, true
}

func (m *Manager) CancelPairing(connectionID uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cancel, ok := m.pairingCancels[connectionID]; ok {
		cancel()
		delete(m.pairingCancels, connectionID)
	}
	if ps, ok := m.pairingSessions[connectionID.String()]; ok {
		delete(m.pairingSessions, connectionID.String())
		if ps.phone != "" {
			delete(m.pairingSessions, ps.phone)
		}
	}
}

func (m *Manager) CancelPairingByPhone(phone string) {
	if phone == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if ps, ok := m.pairingSessions[phone]; ok {
		ps.cancel()
		delete(m.pairingCancels, ps.connectionID)
		delete(m.pairingSessions, phone)
		delete(m.pairingSessions, ps.connectionID.String())
	}
}

func NewManager(
	db *sql.DB,
	repo *repository.ConnectionRepository,
	registry *ActiveSession,
	dispatchers *channel.Registry,
	waVersion string,
	inboundProcessor InboundEventProcessor,
) *Manager {
	return &Manager{
		db:               db,
		repo:             repo,
		registry:         registry,
		dispatchers:      dispatchers,
		waVersion:        waVersion,
		inboundProcessor: inboundProcessor,
		clientFactory:    &defaultClientFactory{},
		qrTimeout:        120 * time.Second,
		pairingCancels:   make(map[uuid.UUID]context.CancelFunc),
		healthMap:        make(map[uuid.UUID]*SessionHealthInfo),
		pairingSessions:  make(map[string]*PairingSession),
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
	jid, err := parseJID(*d.JID)
	if err != nil {
		_ = m.EmitStatusEvent(ctx, d.WorkspaceID, d.ID, "whatsapp", d.SenderIdentity, string(StateDisconnected))
		return fmt.Errorf("parse JID: %w", err)
	}

	cfg := whatsapp.ClientConfig{
		DB:        m.db,
		WAVersion: m.waVersion,
		JID:       &jid,
	}
	if d.ProxyURL != nil {
		cfg.ProxyURL = *d.ProxyURL
	}

	wc, err := whatsapp.NewWhatsAppClient(cfg)
	if err != nil {
		_ = m.EmitStatusEvent(ctx, d.WorkspaceID, d.ID, "whatsapp", d.SenderIdentity, string(StateDisconnected))
		return fmt.Errorf("create whatsapp client: %w", err)
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
	m.registerEventHandler(wc, d, cancel)

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
		_ = m.EmitStatusEvent(context.Background(), d.WorkspaceID, d.ID, "whatsapp", d.SenderIdentity, string(StateDisconnected))
		m.registry.Remove(jid)
	}()

	// Update status to connected
	if err := m.repo.UpdateStatus(ctx, d.ID, string(DeviceStatusConnected)); err != nil {
		return err
	}
	_ = m.EmitStatusEvent(ctx, d.WorkspaceID, d.ID, "whatsapp", d.SenderIdentity, string(StateConnected))
	return nil
}

// parseJID is a helper that parses a JID string.
func parseJID(jid string) (types.JID, error) {
	parsed, err := types.ParseJID(jid)
	if err != nil {
		return types.JID{}, err
	}
	return parsed, nil
}

func (m *Manager) registerEventHandler(wc WhatsAppClientInterface, d *repository.Connection, cancel context.CancelFunc) {
	if realWC, ok := wc.(*whatsapp.WhatsAppClient); ok && realWC.Client() != nil {
		realWC.Client().AddEventHandler(func(evt interface{}) {
			switch v := evt.(type) {
			case *waEvents.LoggedOut:
				slog.Warn("session manager: whatsmeow logged out event received, marking device terminal", "device_id", d.ID)
				if m.repo != nil {
					_ = m.repo.UpdateStatus(context.Background(), d.ID, string(DeviceStatusTerminal))
				}
				_ = m.EmitStatusEvent(context.Background(), d.WorkspaceID, d.ID, "whatsapp", d.SenderIdentity, "degraded")
				if cancel != nil {
					cancel()
				}
			case *waEvents.Message:
				m.HandleWhatsAppMessage(context.Background(), realWC, d, v)
			}
		})
	}
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

// CalcBackoff computes exponential backoff with jitter.
func CalcBackoff(attempt int) time.Duration {
	backoff := float64(defaultReconnectBackoff) * math.Pow(2, float64(attempt))
	if backoff > float64(maxReconnectBackoff) {
		backoff = float64(maxReconnectBackoff)
	}
	// Add 10% jitter
	jitter := backoff * 0.1 * (rand.Float64()*2 - 1)
	return time.Duration(backoff + jitter)
}

// HandleWhatsAppMessage parses incoming whatsmeow message events, downloads media,
// determines group or direct message routing, builds metadata, and passes the InboundEvent to the processor.
func (m *Manager) HandleWhatsAppMessage(ctx context.Context, wc WhatsAppClientInterface, d *repository.Connection, v *waEvents.Message) {
	if v.Info.IsFromMe {
		return
	}

	var fromJID string
	var metadata map[string]string

	if v.Info.IsGroup {
		fromJID = v.Info.Chat.String()
		if fromJID == "" {
			fromJID = v.Info.Sender.String()
		}
		metadata = map[string]string{
			domain.MetaIsGroup:        "true",
			domain.MetaParticipant:    v.Info.Sender.String(),
			domain.MetaChatJID:        v.Info.Chat.String(),
			domain.MetaSenderPushName: v.Info.PushName,
		}
	} else {
		fromJID = v.Info.Sender.String()
	}

	var whatsmeowCli *whatsmeow.Client
	if wc != nil {
		if realWC, ok := wc.(*whatsapp.WhatsAppClient); ok {
			whatsmeowCli = realWC.Client()
		}
	}

	inboundMedia := extractWhatsAppMedia(ctx, whatsmeowCli, v)

	m.mu.Lock()
	proc := m.inboundProcessor
	m.mu.Unlock()

	if proc != nil {
		recipientIdentity := ""
		var wsID, connID uuid.UUID
		if d != nil {
			wsID = d.WorkspaceID
			connID = d.ID
			recipientIdentity = d.SenderIdentity
			if recipientIdentity == "" && d.JID != nil {
				recipientIdentity = *d.JID
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
			WorkspaceID:  wsID,
			ConnectionID: connID,
			MessageID:    v.Info.ID,
			Channel:      "whatsapp",
			From:         fromJID,
			To:           recipientIdentity,
			Body:         extractWhatsAppBody(v),
			Media:        inboundMedia,
			Location:     inboundLocation,
			Contacts:     inboundContacts,
			SenderName:   v.Info.PushName,
			OccurredAt:   v.Info.Timestamp,
			Metadata:     metadata,
		}

		_ = proc.Process(ctx, event)
	}
}

// extractWhatsAppMedia downloads and constructs InboundMedia from WhatsApp message attachments.
func extractWhatsAppMedia(ctx context.Context, cli *whatsmeow.Client, v *waEvents.Message) *inbound.InboundMedia {
	if imageMsg := v.Message.GetImageMessage(); imageMsg != nil {
		var mediaBytes []byte
		if cli != nil {
			if data, err := cli.Download(ctx, imageMsg); err == nil {
				mediaBytes = data
			}
		}
		var caption string
		if imageMsg.Caption != nil {
			caption = *imageMsg.Caption
		}
		return &inbound.InboundMedia{
			Bytes:     mediaBytes,
			MediaType: "image",
			Caption:   caption,
		}
	}

	if docMsg := v.Message.GetDocumentMessage(); docMsg != nil {
		var mediaBytes []byte
		if cli != nil {
			if data, err := cli.Download(ctx, docMsg); err == nil {
				mediaBytes = data
			}
		}
		var filename, caption string
		if docMsg.FileName != nil {
			filename = *docMsg.FileName
		}
		if docMsg.Caption != nil {
			caption = *docMsg.Caption
		}
		return &inbound.InboundMedia{
			Bytes:     mediaBytes,
			MediaType: "document",
			Filename:  filename,
			Caption:   caption,
		}
	}

	if audioMsg := v.Message.GetAudioMessage(); audioMsg != nil {
		var mediaBytes []byte
		if cli != nil {
			if data, err := cli.Download(ctx, audioMsg); err == nil {
				mediaBytes = data
			}
		}
		return &inbound.InboundMedia{
			Bytes:     mediaBytes,
			MediaType: "audio",
		}
	}

	if videoMsg := v.Message.GetVideoMessage(); videoMsg != nil {
		var mediaBytes []byte
		if cli != nil {
			if data, err := cli.Download(ctx, videoMsg); err == nil {
				mediaBytes = data
			}
		}
		var caption string
		if videoMsg.Caption != nil {
			caption = *videoMsg.Caption
		}
		return &inbound.InboundMedia{
			Bytes:     mediaBytes,
			MediaType: "video",
			Caption:   caption,
		}
	}

	return nil
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
