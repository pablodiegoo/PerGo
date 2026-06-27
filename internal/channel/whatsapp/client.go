package whatsapp

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waEvents "go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// ClientConfig holds configuration for creating a WhatsApp client.
type ClientConfig struct {
	DB        *sql.DB
	WAVersion string // e.g. "2.3000.1025000000"
}

// WhatsAppClient wraps a whatsmeow client with event handlers and lifecycle
// management. It provides the Run/Stop goroutine pattern for per-device
// sessions.
type WhatsAppClient struct {
	client *whatsmeow.Client
	jid    types.JID
	log    *slog.Logger
}

// NewWhatsAppClient creates a whatsmeow client with PostgreSQL-backed
// device store. The JID is empty until pairing completes.
func NewWhatsAppClient(cfg ClientConfig) (*WhatsAppClient, error) {
	container := sqlstore.NewWithDB(cfg.DB, "postgres", waLog.Noop)

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil || deviceStore == nil {
		deviceStore = container.NewDevice()
	}

	clientLog := slog.With("component", "whatsapp")

	cli := whatsmeow.NewClient(deviceStore, waLog.Noop)

	if cfg.WAVersion != "" {
		if ver, err := store.ParseVersion(cfg.WAVersion); err == nil {
			store.SetWAVersion(ver)
		} else {
			clientLog.Warn("whatsapp: failed to parse WA version", "version", cfg.WAVersion, "error", err)
		}
	}

	wc := &WhatsAppClient{
		client: cli,
		log:    clientLog,
	}

	wc.setupEventHandlers()

	return wc, nil
}

// JID returns the device's JID after pairing. Empty before pairing.
func (wc *WhatsAppClient) JID() types.JID {
	return wc.jid
}

// Client returns the underlying whatsmeow client.
func (wc *WhatsAppClient) Client() *whatsmeow.Client {
	return wc.client
}

// SetJID sets the device JID after pairing.
func (wc *WhatsAppClient) SetJID(jid types.JID) {
	wc.jid = jid
}

// DeviceStore returns the underlying device store for persistence.
func (wc *WhatsAppClient) DeviceStore() *store.Device {
	if wc.client != nil {
		return wc.client.Store
	}
	return nil
}

// setupEventHandlers registers handlers for whatsmeow lifecycle events.
func (wc *WhatsAppClient) setupEventHandlers() {
	wc.client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *waEvents.LoggedOut:
			wc.log.Warn("whatsapp: logged out",
				"on_connect", v.OnConnect,
				"jid", wc.jid.String(),
			)
		case *waEvents.ClientOutdated:
			wc.log.Warn("whatsapp: client outdated, auto-updating WA version and reconnecting")
			curVer := store.GetWAVersion()
			curVer[2]++ // increment patch
			store.SetWAVersion(curVer)
			go func() {
				wc.client.Disconnect()
				if err := wc.client.Connect(); err != nil {
					wc.log.Error("whatsapp: failed to reconnect after client outdated update", "error", err)
				}
			}()
		case *waEvents.Connected:
			wc.log.Info("whatsapp: connected",
				"jid", wc.jid.String(),
			)
		case *waEvents.Disconnected:
			wc.log.Warn("whatsapp: disconnected",
				"jid", wc.jid.String(),
			)
		}
	})
}

// Run connects the client and blocks until ctx is cancelled.
func (wc *WhatsAppClient) Run(ctx context.Context) error {
	if err := wc.client.Connect(); err != nil {
		return fmt.Errorf("whatsapp connect: %w", err)
	}

	wc.log.Info("whatsapp: client running", "jid", wc.jid.String())
	<-ctx.Done()

	wc.client.Disconnect()
	return nil
}

// GetQRChannel returns the QR code channel for pairing a new device.
// Must be called BEFORE Connect() per whatsmeow API contract.
func (wc *WhatsAppClient) GetQRChannel(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error) {
	return wc.client.GetQRChannel(ctx)
}

// Connect connects the client to WhatsApp WebSocket. For pairing flows,
// call GetQRChannel first, then Connect.
func (wc *WhatsAppClient) Connect() error {
	return wc.client.Connect()
}

// Disconnect disconnects from the WhatsApp WebSocket.
func (wc *WhatsAppClient) Disconnect() {
	wc.client.Disconnect()
}
