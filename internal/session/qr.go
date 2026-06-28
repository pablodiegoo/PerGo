// Package session provides WhatsApp device session lifecycle management.
package session

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	whatsapp "github.com/pablojhp.pergo/internal/channel/whatsapp"
)

const (
	// pairingTimeout is the maximum time to wait for a QR scan.
	pairingTimeout = 5 * time.Minute
)

// QREventType classifies QR pairing channel events.
type QREventType string

const (
	QREventCode   QREventType = "qr_code" // new QR code available
	QREventPaired QREventType = "paired"  // device successfully paired
	QREventError  QREventType = "error"   // pairing failed
)

// QRPairingEvent is emitted on the channel returned by StartPairing.
//
// For qr_code events, Data holds the raw QR code string as bytes (not a PNG —
// the admin template renders this using an <img data-qr="..."> + JS library,
// or displays the raw code for manual entry). Using the raw code avoids
// adding a QR image generation dependency.
//
// For paired/error events, Data is nil; Message contains a human-readable note.
type QRPairingEvent struct {
	Type    QREventType
	Data    []byte // raw QR code string bytes for qr_code; nil otherwise
	Message string // human-readable description
}

// StartPairing initiates WhatsApp Web device pairing for a workspace.
// It creates a new whatsmeow client, starts the QR pairing flow, and
// returns a channel that emits QR events until pairing succeeds or fails.
//
// The caller must drain the returned channel until it is closed.
// On success (paired event), the device is persisted to the database
// and added to the session registry.
//
// Note: per whatsmeow API contract, GetQRChannel is called before Connect.
func (m *Manager) StartPairing(ctx context.Context, workspaceID uuid.UUID, phone string) (<-chan QRPairingEvent, error) {
	cfg := whatsapp.ClientConfig{
		DB:        m.db,
		WAVersion: m.waVersion,
	}

	wc, err := whatsapp.NewWhatsAppClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("session manager: create whatsapp client: %w", err)
	}

	// GetQRChannel MUST be called before Connect per whatsmeow API contract.
	qrCh, err := wc.GetQRChannel(ctx)
	if err != nil {
		return nil, fmt.Errorf("session manager: get QR channel: %w", err)
	}

	// Connect the client after setting up the QR channel.
	if err := wc.Connect(); err != nil {
		return nil, fmt.Errorf("session manager: connect for pairing: %w", err)
	}

	out := make(chan QRPairingEvent, 8)

	go func() {
		defer close(out)

		timer := time.NewTimer(pairingTimeout)
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				wc.Disconnect()
				out <- QRPairingEvent{Type: QREventError, Message: "pairing cancelled"}
				return
			case <-timer.C:
				wc.Disconnect()
				out <- QRPairingEvent{Type: QREventError, Message: "pairing timeout: no scan in 5 minutes"}
				return
			case item, ok := <-qrCh:
				if !ok {
					// channel closed without success event
					wc.Disconnect()
					return
				}
				switch item.Event {
				case "code":
					// Reset timeout on each new code (codes arrive every ~20s).
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					timer.Reset(pairingTimeout)
					out <- QRPairingEvent{
						Type:    QREventCode,
						Data:    []byte(item.Code),
						Message: "scan QR code in WhatsApp",
					}
				case "success":
					// Pairing succeeded — persist device and register session.
					if err := m.onPairingSuccess(ctx, wc, workspaceID, phone); err != nil {
						slog.Error("session manager: pairing success handler failed",
							"error", err,
							"workspace_id", workspaceID,
							"phone", phone,
						)
						wc.Disconnect()
						out <- QRPairingEvent{Type: QREventError, Message: fmt.Sprintf("pairing succeeded but setup failed: %v", err)}
						return
					}
					out <- QRPairingEvent{Type: QREventPaired, Message: "device paired successfully"}
					return
				case "error":
					errMsg := "pairing error"
					if item.Error != nil {
						errMsg = item.Error.Error()
					}
					wc.Disconnect()
					out <- QRPairingEvent{Type: QREventError, Message: errMsg}
					return
				default:
					// timeout, err-unexpected-state, err-client-outdated, etc.
					wc.Disconnect()
					out <- QRPairingEvent{Type: QREventError, Message: fmt.Sprintf("pairing ended: %s", item.Event)}
					return
				}
			}
		}
	}()

	return out, nil
}

// onPairingSuccess persists the newly paired device and registers its session.
func (m *Manager) onPairingSuccess(ctx context.Context, wc *whatsapp.WhatsAppClient, workspaceID uuid.UUID, phone string) error {
	jid := wc.JID()
	now := time.Now()

	d := &Device{
		ID:             uuid.New(),
		WorkspaceID:    workspaceID,
		Channel:        "whatsapp",
		JID:            jid.String(),
		Phone:          phone,
		Status:         DeviceStatusConnected,
		ConnectedSince: &now,
	}

	if err := m.repo.Create(ctx, d); err != nil {
		return fmt.Errorf("persist device: %w", err)
	}

	// Register active session.
	sessionCtx, cancel := context.WithCancel(context.Background())
	sess := &Session{
		DeviceID: d.ID.String(),
		JID:      jid,
		Client:   wc,
		Cancel:   cancel,
	}
	m.registry.Add(sess)

	// Keep the client running in background.
	go func() {
		_ = wc.Run(sessionCtx)
		_ = m.repo.UpdateStatus(context.Background(), d.ID, DeviceStatusDisconnected)
		m.registry.Remove(jid)
	}()

	slog.Info("session manager: device paired",
		"jid", jid.String(),
		"phone", phone,
		"workspace_id", workspaceID,
	)
	return nil
}
