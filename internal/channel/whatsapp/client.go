package whatsapp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

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
	JID       *types.JID
	ProxyURL  string
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
	if err := container.Upgrade(context.Background()); err != nil {
		return nil, fmt.Errorf("whatsapp: upgrade database store: %w", err)
	}

	var deviceStore *store.Device
	var err error
	if cfg.JID != nil && !cfg.JID.IsEmpty() {
		deviceStore, err = container.GetDevice(context.Background(), *cfg.JID)
	}
	if err != nil || deviceStore == nil {
		deviceStore = container.NewDevice()
	}

	clientLog := slog.With("component", "whatsapp")

	cli := whatsmeow.NewClient(deviceStore, waLog.Noop)

	if cfg.ProxyURL != "" {
		if err := ConfigureProxy(cli, cfg.ProxyURL); err != nil {
			clientLog.Warn("whatsapp: failed to configure proxy", "url", cfg.ProxyURL, "error", err)
		}
	}

	UpdateWAVersion(context.Background(), cfg.WAVersion, clientLog)

	wc := &WhatsAppClient{
		client: cli,
		log:    clientLog,
	}
	if cfg.JID != nil {
		wc.jid = *cfg.JID
	}

	wc.setupEventHandlers()

	return wc, nil
}

// UpdateWAVersion updates the WhatsApp Web client version in store.
// If explicitVersion is provided, it parses and applies it.
// If explicitVersion is empty, it attempts to fetch the latest version from web.whatsapp.com.
func UpdateWAVersion(ctx context.Context, explicitVersion string, log *slog.Logger) store.WAVersionContainer {
	if explicitVersion != "" {
		if ver, err := store.ParseVersion(explicitVersion); err == nil {
			store.SetWAVersion(ver)
			if log != nil {
				log.Info("whatsapp: configured explicit WA version", "version", explicitVersion)
			}
			return ver
		} else if log != nil {
			log.Warn("whatsapp: failed to parse explicit WA version", "version", explicitVersion, "error", err)
		}
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if latestVer, err := whatsmeow.GetLatestVersion(fetchCtx, nil); err == nil && latestVer != nil {
		store.SetWAVersion(*latestVer)
		if log != nil {
			log.Info("whatsapp: resolved latest WA version from WhatsApp Web",
				"version", fmt.Sprintf("%d.%d.%d", latestVer[0], latestVer[1], latestVer[2]))
		}
		return *latestVer
	} else if log != nil {
		cur := store.GetWAVersion()
		log.Debug("whatsapp: using fallback WA version",
			"version", fmt.Sprintf("%d.%d.%d", cur[0], cur[1], cur[2]),
			"fetch_err", err)
	}

	return store.GetWAVersion()
}

// JID returns the device's JID after pairing. Empty before pairing.
func (wc *WhatsAppClient) JID() types.JID {
	if wc.client != nil && wc.client.Store != nil && wc.client.Store.ID != nil && !wc.client.Store.ID.IsEmpty() {
		return *wc.client.Store.ID
	}
	return wc.jid
}

// Client returns the underlying whatsmeow client.
func (wc *WhatsAppClient) Client() *whatsmeow.Client {
	return wc.client
}

// SetJID sets the device JID after pairing.
func (wc *WhatsAppClient) SetJID(jid types.JID) {
	wc.jid = jid
	if wc.client != nil && wc.client.Store != nil {
		wc.client.Store.ID = &jid
	}
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
			wc.log.Warn("whatsapp: client outdated event received, fetching latest WA version from WhatsApp Web")
			go func() {
				fetchCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if latestVer, err := whatsmeow.GetLatestVersion(fetchCtx, nil); err == nil && latestVer != nil {
					store.SetWAVersion(*latestVer)
					wc.log.Info("whatsapp: auto-updated WA version after outdated event",
						"version", fmt.Sprintf("%d.%d.%d", latestVer[0], latestVer[1], latestVer[2]))
				} else {
					curVer := store.GetWAVersion()
					curVer[2]++ // increment patch as fallback
					store.SetWAVersion(curVer)
					wc.log.Warn("whatsapp: failed to fetch latest version, fallback patch incremented",
						"version", fmt.Sprintf("%d.%d.%d", curVer[0], curVer[1], curVer[2]),
						"error", err)
				}
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

// Run connects the client (if not already connected) and blocks until ctx is cancelled.
func (wc *WhatsAppClient) Run(ctx context.Context) error {
	if !wc.client.IsConnected() {
		if err := wc.client.Connect(); err != nil && !errors.Is(err, whatsmeow.ErrAlreadyConnected) {
			return fmt.Errorf("whatsapp connect: %w", err)
		}
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

// ConfigureProxy sets up SOCKS5/HTTP proxy for whatsmeow client connection.
func ConfigureProxy(client *whatsmeow.Client, proxyStr string) error {
	if proxyStr == "" {
		client.SetProxy(nil)
		return nil
	}
	return client.SetProxyAddress(proxyStr)
}
