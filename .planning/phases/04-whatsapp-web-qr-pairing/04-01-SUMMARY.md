---
plan: 04-01
phase: 04-whatsapp-web-qr-pairing
status: complete
wave: 1
autonomous: true
---

## Summary

**What was built:** Channel abstraction layer and WhatsApp Web adapter stub.

**Tasks completed:**
- Task 1: `channel.Dispatcher` interface, `MessagePayload` type, `TerminalError` for non-retryable classification, `IsTerminal` helper
- Task 2: `channel.Registry` with concurrent-safe Get/Register/GetOrDefault/Names
- Task 3: `WhatsAppClient` wrapping whatsmeow with event handlers (LoggedOut, ClientOutdated, Connected, Disconnected), PostgreSQL-backed sqlstore device persistence
- Task 4: `WhatsAppAdapter` implementing Dispatcher with 1-3s random staggered dispatch, phone→JID conversion, terminal vs transient error classification
- Bonus: Wired `Worker.dispatch()` to use `channel.Registry` instead of log-only stub

**Files created:**
- `internal/channel/dispatcher.go` — Dispatcher interface, MessagePayload, TerminalError
- `internal/channel/registry.go` — Concurrent-safe dispatcher registry
- `internal/channel/whatsapp/client.go` — whatsmeow client wrapper with lifecycle events
- `internal/channel/whatsapp/adapter.go` — WhatsApp Dispatcher implementation

**Files modified:**
- `internal/platform/queue/worker.go` — Real dispatch via Registry
- `cmd/pergo/main.go` — Registry wiring (empty for now, populated in Plan 02)

**Tests:** 8 tests passing (channel: 3, whatsapp: 5)

**Deviation from plan:** whatsmeow API differs from initial assumptions:
- `NewClient` takes `*store.Device` + `waLog.Logger`, not `*sqlstore.Container`
- `GetQRChannel` returns `<-chan QRChannelItem` (with Event/Code/Error/Timeout fields), not `<-chan waEvents.QR`
- `SetWAVersion` is on the `store` package (global), not the client
- `Disconnect()` returns nothing (not error)
- `Disconnected` event has no `Reason` field (empty struct)
- `QRChannelItem.Code` is a string (ref codes), not `[]byte`

**Next:** Plan 02 wires session management, device persistence, and startup reconnection.
