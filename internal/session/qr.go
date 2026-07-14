// Package session provides WhatsApp device session lifecycle management.
package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"

	whatsapp "github.com/pablojhp.pergo/internal/channel/whatsapp"
	"github.com/pablojhp.pergo/internal/repository"
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

// ErrMaxConnectionsExceeded is returned when the workspace exceeds its WhatsApp connection limit.
var ErrMaxConnectionsExceeded = errors.New("maximum active WhatsApp connections limit exceeded")

// StartPairing initiates WhatsApp Web device pairing for a workspace.
// It creates a new whatsmeow client, starts the QR pairing flow, and
// returns a channel that emits QR events until pairing succeeds or fails.
//
// The caller must drain the returned channel until it is closed.
// On success (paired event), the device is persisted to the database
// and added to the session registry.
//
// Note: per whatsmeow API contract, GetQRChannel is called before Connect.
func (m *Manager) StartPairing(ctx context.Context, workspaceID uuid.UUID, phone string, existingConnID *uuid.UUID, proxyURL string) (<-chan QRPairingEvent, error) {
	// Read max connections limit from environment (default: 5)
	maxLimit := 5
	if limitStr := os.Getenv("PERGO_MAX_WHATSAPP_CONNECTIONS"); limitStr != "" {
		if val, err := strconv.Atoi(limitStr); err == nil && val >= 0 {
			maxLimit = val
		}
	}

	// Count active connections in workspace
	devices, err := m.repo.ListByWorkspace(ctx, workspaceID)
	if err == nil {
		activeCount := 0
		for _, dev := range devices {
			if dev.Channel == "whatsapp" && dev.JID != nil && *dev.JID != "" {
				parsedJID, parseErr := parseJID(*dev.JID)
				if parseErr == nil && m.registry.Get(parsedJID) != nil {
					// If we are re-pairing this specific connection slot, do not count it against the limit
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

	if existingConnID != nil && proxyURL == "" {
		if conn, err := m.repo.GetByID(ctx, *existingConnID); err == nil && conn != nil && conn.ProxyURL != nil {
			proxyURL = *conn.ProxyURL
		}
	}

	cfg := whatsapp.ClientConfig{
		DB:        m.db,
		WAVersion: m.waVersion,
		ProxyURL:  proxyURL,
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
					if err := m.onPairingSuccess(ctx, wc, workspaceID, phone, existingConnID, proxyURL); err != nil {
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
func (m *Manager) onPairingSuccess(ctx context.Context, wc *whatsapp.WhatsAppClient, workspaceID uuid.UUID, phone string, existingConnID *uuid.UUID, proxyURL string) error {
	jid := wc.JID()
	now := time.Now()

	var dID uuid.UUID
	if existingConnID != nil {
		dID = *existingConnID
		// Perform direct UPDATE on connections table to reuse the slot
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
	} else {
		dID = uuid.New()
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

	// Register active session.
	sessionCtx, cancel := context.WithCancel(context.Background())
	sess := &Session{
		DeviceID: dID.String(),
		JID:      jid,
		Client:   wc,
		Cancel:   cancel,
	}
	m.registry.Add(sess)

	// Keep the client running in background.
	go func() {
		_ = wc.Run(sessionCtx)
		_ = m.repo.UpdateStatus(context.Background(), dID, string(DeviceStatusDisconnected))
		m.registry.Remove(jid)
	}()

	slog.Info("session manager: device paired",
		"jid", jid.String(),
		"phone", phone,
		"workspace_id", workspaceID,
	)
	return nil
}
