---
plan: 04-03
phase: 04-whatsapp-web-qr-pairing
status: complete
wave: 3
autonomous: true
---

## Summary

**What was built:** Admin UI for QR pairing and session telemetry — completes the operator-facing WhatsApp Web experience.

**Tasks completed:**

- **Task 1: QR pairing flow** — `StartPairing(ctx, workspaceID, phone)` on `Manager` returns `<-chan QRPairingEvent`. Uses whatsmeow `GetQRChannel` (before `Connect`, per API contract). Events: `qr_code` (raw QR string bytes), `paired` (device saved + session registered), `error`. Added `Connect()`/`Disconnect()` to `WhatsAppClient`, `DisconnectByJID` to `ActiveSession`.

- **Task 2: Device management admin page** — `/admin/devices` renders device list table with status badges. `PairForm` renders phone input + BanWarning component. `QRFragment` displays QR with 5-second HTMX polling (`hx-trigger="every 5s"`). Ban-risk warning: ⚠️ yellow banner prominently displayed during pairing. `DeviceCard` component with status badge and HTMX Disconnect button. In-memory pairing state (phone → QRState) with RWMutex.

- **Task 3: Telemetry admin page** — `/admin/telemetry` shows active sessions table (JID, status, connected-since, messages-sent), NATS connection status, system uptime. Self-refreshes via `hx-trigger="every 10s"` on the content div. `TelemetryHandler` aggregates live state from `ActiveSession`. `NATSStatus` interface implemented by `natsConn` wrapper in `main.go`.

- **Task 4: Route wiring and sidebar** — Sidebar updated: Dashboard → Workspaces → **Devices** → **Telemetry** → Audit Logs. Admin routes registered: `GET /admin/devices`, `GET /admin/devices/pair-form`, `POST /admin/devices/pair`, `GET /admin/devices/qr`, `DELETE /admin/devices/:jid`, `GET /admin/telemetry`. `natsConn` extended with `IsConnected() bool` to satisfy `admin.NATSStatus`.

**Files created:**
- `internal/session/qr.go` — `StartPairing`, `QRPairingEvent`, `onPairingSuccess`
- `internal/session/qr_test.go` — event type and API surface tests
- `internal/api/handler/admin/device.go` — `DeviceHandler` (List, PairForm, StartPairing, GetQR, Disconnect)
- `internal/api/handler/admin/device_test.go` — handler construction and validation tests
- `internal/api/handler/admin/telemetry.go` — `TelemetryHandler`, `TelemetryData`, `SessionInfo`
- `internal/api/handler/admin/telemetry_test.go` — handler nil-safe tests
- `templates/components/ban_warning.templ` — BanWarning component
- `templates/components/device_card.templ` — DeviceCard + DeviceStatusBadge
- `templates/pages/devices.templ` — DeviceListPage, DeviceListContent, PairForm, QRFragment
- `templates/pages/telemetry.templ` — TelemetryPage, TelemetryContent (with SessionInfo, TelemetryData types)

**Files modified:**
- `internal/session/registry.go` — `DisconnectByJID` method
- `internal/channel/whatsapp/client.go` — `Connect()`, `Disconnect()` methods
- `templates/layout/sidebar.templ` — Devices + Telemetry links
- `cmd/omnigo/main.go` — Device/Telemetry handler wiring + `natsConn.IsConnected()`

**Tests:** All session and API tests pass (session: 5+10 existing + 5 new QR tests; admin handlers: 6 tests)

**Build:** `go build ./...` passes; `templ generate` produces 5 updated files.

**Key design decisions:**
- QR code stored as raw string (not PNG image) — avoids adding a QR image library dependency. Admin template displays via `data-qr` attribute for potential JS QR library, or as raw code for copy-paste.
- Pairing state held in-memory (phone → QRState map with RWMutex) — MVP single-instance design; would need Redis/shared store for multi-instance deployments.
- `GetQRChannel` called before `Connect` — enforces whatsmeow API contract discovered in plan 01 deviations.
- Workspace ID uses `uuid.Nil` in StartPairing for single-tenant MVP; multi-tenant requires operator session extraction.
- `NATSStatus` interface in telemetry handler allows testing without real NATS connections.

**Next:** Phase 4 complete. Phase 5 builds WABA and Telegram (official stateless channels) + smart fallback pipeline.
