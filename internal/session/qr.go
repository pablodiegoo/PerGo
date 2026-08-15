// Package session provides WhatsApp device session lifecycle management.
package session

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	qrcode "github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"

	whatsapp "github.com/pablojhp.pergo/internal/channel/whatsapp"
	"github.com/pablojhp.pergo/internal/repository"
)

const (
	// defaultPairingTimeout is the maximum time to wait for a QR scan if not overridden.
	defaultPairingTimeout = 2 * time.Minute
)

// QREventType classifies legacy QR pairing channel events.
type QREventType string

const (
	QREventCode   QREventType = "qr_code" // new QR code available
	QREventPaired QREventType = "paired"  // device successfully paired
	QREventError  QREventType = "error"   // pairing failed
)

// QRPairingEvent is emitted on the legacy channel returned by StartPairing.
type QRPairingEvent struct {
	Type    QREventType
	Data    []byte // raw QR code string bytes for qr_code; nil otherwise
	Message string // human-readable description
}

// QREvent represents a standardized QR pairing event with base64 PNG data URL and expiry.
type QREvent struct {
	Status       string     `json:"status"`                 // "pending", "paired", "error"
	Code         string     `json:"code,omitempty"`         // raw QR code string
	QRDataURL    string     `json:"qr_data_url,omitempty"`  // "data:image/png;base64,..."
	PairingCode  string     `json:"pairing_code,omitempty"` // pairing code if any
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	Message      string     `json:"message,omitempty"`
	ConnectionID *uuid.UUID `json:"connection_id,omitempty"`
}

// PairingSession maintains in-memory state and pub-sub broadcaster for a pairing session.
type PairingSession struct {
	phone        string
	workspaceID  uuid.UUID
	connectionID uuid.UUID
	latestEvent  QREvent
	subscribers  map[chan QREvent]struct{}
	cancel       context.CancelFunc
	closed       bool
	mu           sync.RWMutex
}

// NewPairingSession creates a new in-memory PairingSession instance.
func NewPairingSession(workspaceID uuid.UUID, phone string, connID uuid.UUID) *PairingSession {
	ctx, cancel := context.WithCancel(context.Background())
	_ = ctx
	return &PairingSession{
		phone:        phone,
		workspaceID:  workspaceID,
		connectionID: connID,
		latestEvent: QREvent{
			Status:       "pending",
			Message:      "Waiting for QR code...",
			ConnectionID: &connID,
		},
		subscribers: make(map[chan QREvent]struct{}),
		cancel:      cancel,
	}
}

// ConnectionID returns the connection ID associated with this pairing session.
func (ps *PairingSession) ConnectionID() uuid.UUID {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.connectionID
}

// Phone returns the phone number associated with this pairing session.
func (ps *PairingSession) Phone() string {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.phone
}

// WorkspaceID returns the workspace ID associated with this pairing session.
func (ps *PairingSession) WorkspaceID() uuid.UUID {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.workspaceID
}

// LatestEvent returns the most recent QREvent emitted for this pairing session.
func (ps *PairingSession) LatestEvent() QREvent {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.latestEvent
}

// Subscribe adds a subscriber channel to receive QR events.
func (ps *PairingSession) Subscribe() (<-chan QREvent, func()) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ch := make(chan QREvent, 16)
	if ps.closed {
		if ps.latestEvent.Status != "" {
			ch <- ps.latestEvent
		}
		close(ch)
		return ch, func() {}
	}

	ps.subscribers[ch] = struct{}{}
	if ps.latestEvent.Status != "" {
		select {
		case ch <- ps.latestEvent:
		default:
		}
	}

	var once sync.Once
	unsub := func() {
		once.Do(func() {
			ps.mu.Lock()
			if _, ok := ps.subscribers[ch]; ok {
				delete(ps.subscribers, ch)
				close(ch)
			}
			ps.mu.Unlock()
		})
	}
	return ch, unsub
}

func (ps *PairingSession) broadcast(evt QREvent) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ps.latestEvent = evt
	for ch := range ps.subscribers {
		select {
		case ch <- evt:
		default:
			// Buffer full: drop oldest and retry push
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- evt:
			default:
			}
		}
	}
}

// Broadcast emits a QREvent to all active subscribers of this pairing session.
func (ps *PairingSession) Broadcast(evt QREvent) {
	ps.broadcast(evt)
}

func (ps *PairingSession) closeWithEvent(evt QREvent) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.closed {
		return
	}
	ps.closed = true
	ps.latestEvent = evt
	for ch := range ps.subscribers {
		select {
		case ch <- evt:
		default:
		}
		close(ch)
		delete(ps.subscribers, ch)
	}
}

// CloseWithEvent closes the pairing session and emits a final terminal event.
func (ps *PairingSession) CloseWithEvent(evt QREvent) {
	ps.closeWithEvent(evt)
}

// BroadcastQR emits a QREvent to the pairing session identified by key (phone or connectionID).
func (m *Manager) BroadcastQR(key string, evt QREvent) bool {
	m.mu.Lock()
	ps, ok := m.pairingSessions[key]
	m.mu.Unlock()
	if !ok {
		return false
	}
	if evt.Status == "paired" || evt.Status == "error" {
		ps.closeWithEvent(evt)
	} else {
		ps.broadcast(evt)
	}
	return true
}

// GenerateQRDataURL generates a base64-encoded PNG data URL from a raw QR code string.
func GenerateQRDataURL(code string) string {
	if code == "" {
		return ""
	}
	pngBytes, err := qrcode.Encode(code, qrcode.Medium, 256)
	if err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes)
}

// ErrMaxConnectionsExceeded is returned when the workspace exceeds its WhatsApp connection limit.
var ErrMaxConnectionsExceeded = errors.New("maximum active WhatsApp connections limit exceeded")

// StartPairingSession initiates WhatsApp Web device pairing and registers a pub-sub broadcaster in Manager.
func (m *Manager) StartPairingSession(ctx context.Context, workspaceID uuid.UUID, phone string, existingConnID *uuid.UUID, proxyURL string) (*PairingSession, error) {
	maxLimit := 5
	if limitStr := os.Getenv("PERGO_MAX_WHATSAPP_CONNECTIONS"); limitStr != "" {
		if val, err := strconv.Atoi(limitStr); err == nil && val >= 0 {
			maxLimit = val
		}
	}

	if m.repo != nil {
		devices, err := m.repo.ListByWorkspace(ctx, workspaceID)
		if err == nil {
			activeCount := 0
			for _, dev := range devices {
				if dev.Channel == "whatsapp" && dev.JID != nil && *dev.JID != "" {
					parsedJID, parseErr := parseJID(*dev.JID)
					if parseErr == nil && m.registry.Get(parsedJID) != nil {
						if existingConnID != nil && dev.ID == *existingConnID {
							continue
						}
						activeCount++
					}
				}
			}
			if activeCount >= maxLimit {
				return nil, ErrMaxConnectionsExceeded
			}
		}
	}

	if existingConnID != nil && proxyURL == "" && m.repo != nil {
		if conn, err := m.repo.GetByID(ctx, *existingConnID); err == nil && conn != nil && conn.ProxyURL != nil {
			proxyURL = *conn.ProxyURL
		}
	}

	var connID uuid.UUID
	isExisting := false
	if existingConnID != nil {
		connID = *existingConnID
		isExisting = true
	} else {
		connID = uuid.New()
	}

	m.mu.Lock()
	if oldPs, ok := m.pairingSessions[phone]; ok {
		oldPs.cancel()
		delete(m.pairingCancels, oldPs.connectionID)
		delete(m.pairingSessions, phone)
		delete(m.pairingSessions, oldPs.connectionID.String())
	}
	if oldPs, ok := m.pairingSessions[connID.String()]; ok {
		oldPs.cancel()
		delete(m.pairingCancels, oldPs.connectionID)
		delete(m.pairingSessions, oldPs.phone)
		delete(m.pairingSessions, connID.String())
	}

	ctxPair, cancelPair := context.WithCancel(context.Background())
	ps := &PairingSession{
		phone:        phone,
		workspaceID:  workspaceID,
		connectionID: connID,
		latestEvent: QREvent{
			Status:       "pending",
			Message:      "Waiting for QR code...",
			ConnectionID: &connID,
		},
		subscribers: make(map[chan QREvent]struct{}),
		cancel:      cancelPair,
	}

	m.pairingSessions[phone] = ps
	m.pairingSessions[connID.String()] = ps
	m.pairingCancels[connID] = cancelPair
	m.mu.Unlock()

	cfg := whatsapp.ClientConfig{
		DB:        m.db,
		WAVersion: m.waVersion,
		ProxyURL:  proxyURL,
	}

	wc, err := m.clientFactory.CreateClient(cfg)
	if err != nil {
		m.cleanupSession(phone, connID)
		cancelPair()
		return nil, fmt.Errorf("session manager: create whatsapp client: %w", err)
	}

	qrCh, err := wc.GetQRChannel(ctxPair)
	if err != nil {
		m.cleanupSession(phone, connID)
		cancelPair()
		return nil, fmt.Errorf("session manager: get QR channel: %w", err)
	}

	if err := wc.Connect(); err != nil {
		m.cleanupSession(phone, connID)
		cancelPair()
		return nil, fmt.Errorf("session manager: connect for pairing: %w", err)
	}

	go m.runPairingLoop(ctxPair, ps, wc, qrCh, proxyURL, isExisting)

	return ps, nil
}

func (m *Manager) cleanupSession(phone string, connID uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.pairingSessions, phone)
	delete(m.pairingSessions, connID.String())
	delete(m.pairingCancels, connID)
}

func (m *Manager) runPairingLoop(ctx context.Context, ps *PairingSession, wc WhatsAppClientInterface, qrCh <-chan whatsmeow.QRChannelItem, proxyURL string, isExisting bool) {
	timeout := m.qrTimeout
	if timeout <= 0 {
		timeout = defaultPairingTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	defer func() {
		time.AfterFunc(30*time.Second, func() {
			m.cleanupSession(ps.phone, ps.connectionID)
		})
	}()

	for {
		select {
		case <-ctx.Done():
			wc.Disconnect()
			ps.closeWithEvent(QREvent{
				Status:       "error",
				Message:      "pairing cancelled",
				ConnectionID: &ps.connectionID,
			})
			return
		case <-timer.C:
			wc.Disconnect()
			ps.closeWithEvent(QREvent{
				Status:       "error",
				Message:      fmt.Sprintf("pairing timeout: no scan in %v", timeout),
				ConnectionID: &ps.connectionID,
			})
			return
		case item, ok := <-qrCh:
			if !ok {
				wc.Disconnect()
				ps.closeWithEvent(QREvent{
					Status:       "error",
					Message:      "pairing channel closed",
					ConnectionID: &ps.connectionID,
				})
				return
			}
			switch item.Event {
			case "code":
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(timeout)
				expiresAt := time.Now().Add(25 * time.Second)
				qrDataURL := GenerateQRDataURL(item.Code)
				ps.broadcast(QREvent{
					Status:       "pending",
					Code:         item.Code,
					QRDataURL:    qrDataURL,
					PairingCode:  "",
					ExpiresAt:    &expiresAt,
					Message:      "Scan the QR code in WhatsApp",
					ConnectionID: &ps.connectionID,
				})
			case "success":
				if err := m.onPairingSuccess(ctx, wc, ps.workspaceID, ps.phone, &ps.connectionID, proxyURL, isExisting); err != nil {
					slog.Error("session manager: pairing success handler failed",
						"error", err,
						"workspace_id", ps.workspaceID,
						"phone", ps.phone,
					)
					wc.Disconnect()
					ps.closeWithEvent(QREvent{
						Status:       "error",
						Message:      fmt.Sprintf("pairing succeeded but setup failed: %v", err),
						ConnectionID: &ps.connectionID,
					})
					return
				}
				ps.closeWithEvent(QREvent{
					Status:       "paired",
					Message:      "device paired successfully",
					ConnectionID: &ps.connectionID,
				})
				return
			case "error":
				errMsg := "pairing error"
				if item.Error != nil {
					errMsg = item.Error.Error()
				}
				wc.Disconnect()
				ps.closeWithEvent(QREvent{
					Status:       "error",
					Message:      errMsg,
					ConnectionID: &ps.connectionID,
				})
				return
			default:
				wc.Disconnect()
				ps.closeWithEvent(QREvent{
					Status:       "error",
					Message:      fmt.Sprintf("pairing ended: %s", item.Event),
					ConnectionID: &ps.connectionID,
				})
				return
			}
		}
	}
}

// StartPairing provides backwards compatibility with legacy callers returning <-chan QRPairingEvent.
func (m *Manager) StartPairing(ctx context.Context, workspaceID uuid.UUID, phone string, existingConnID *uuid.UUID, proxyURL string) (<-chan QRPairingEvent, error) {
	ps, err := m.StartPairingSession(ctx, workspaceID, phone, existingConnID, proxyURL)
	if err != nil {
		return nil, err
	}

	subCh, unsub := ps.Subscribe()
	out := make(chan QRPairingEvent, 8)

	go func() {
		defer unsub()
		defer close(out)

		for evt := range subCh {
			var qpType QREventType
			var data []byte
			switch evt.Status {
			case "pending":
				if evt.Code != "" {
					qpType = QREventCode
					data = []byte(evt.Code)
				}
			case "paired":
				qpType = QREventPaired
			case "error":
				qpType = QREventError
			}
			if qpType != "" {
				out <- QRPairingEvent{
					Type:    qpType,
					Data:    data,
					Message: evt.Message,
				}
			}
		}
	}()

	return out, nil
}

// onPairingSuccess persists the newly paired device and registers its session.
func (m *Manager) onPairingSuccess(ctx context.Context, wc WhatsAppClientInterface, workspaceID uuid.UUID, phone string, connID *uuid.UUID, proxyURL string, isExisting bool) error {
	jid := wc.JID()
	now := time.Now().UTC()

	var dID uuid.UUID
	if connID != nil {
		dID = *connID
	} else {
		dID = uuid.New()
	}

	if isExisting && m.db != nil {
		_, err := m.db.ExecContext(ctx, `
			UPDATE connections SET
				jid = $2,
				sender_identity = $3,
				status = $4,
				connected_since = $5,
				proxy_url = $6,
				updated_at = NOW()
			WHERE id = $1
		`, dID, jid.String(), phone, string(DeviceStatusConnected), &now, sql.NullString{String: proxyURL, Valid: proxyURL != ""})
		if err != nil {
			return fmt.Errorf("update connection during re-pair: %w", err)
		}
	} else if m.repo != nil {
		jidStr := jid.String()
		var proxyPtr *string
		if proxyURL != "" {
			proxyPtr = &proxyURL
		}
		conn := &repository.Connection{
			ID:             dID,
			WorkspaceID:    workspaceID,
			Name:           "WhatsApp Web - " + phone,
			Channel:        "whatsapp",
			JID:            &jidStr,
			SenderIdentity: phone,
			Status:         string(DeviceStatusConnected),
			ConnectedSince: &now,
			ProxyURL:       proxyPtr,
		}
		if err := m.repo.Create(ctx, conn); err != nil {
			return fmt.Errorf("persist connection: %w", err)
		}
	}

	// Register active session if registry exists.
	if m.registry != nil {
		sessionCtx, cancel := context.WithCancel(context.Background())
		var clientPtr *whatsapp.WhatsAppClient
		if realWC, ok := wc.(*whatsapp.WhatsAppClient); ok {
			clientPtr = realWC
		}
		sess := &Session{
			DeviceID: dID.String(),
			JID:      jid,
			Client:   clientPtr,
			Cancel:   cancel,
		}
		m.registry.Add(sess)

		// Keep the client running in background.
		go func() {
			_ = wc.Run(sessionCtx)
			if m.repo != nil {
				_ = m.repo.UpdateStatus(context.Background(), dID, string(DeviceStatusDisconnected))
			}
			_ = m.EmitStatusEvent(context.Background(), workspaceID, dID, "whatsapp", phone, string(StateDisconnected))
			m.registry.Remove(jid)
		}()
	}

	_ = m.EmitStatusEvent(ctx, workspaceID, dID, "whatsapp", phone, string(StateConnected))

	slog.Info("session manager: device paired",
		"jid", jid.String(),
		"phone", phone,
		"workspace_id", workspaceID,
	)
	return nil
}
