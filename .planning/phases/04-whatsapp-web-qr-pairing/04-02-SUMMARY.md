---
plan: 04-02
phase: 04-whatsapp-web-qr-pairing
status: complete
wave: 2
autonomous: true
---

## Summary

**What was built:** Session management layer — device persistence, in-memory registry, and startup reconnection with storm protection.

**Tasks completed:**
- Task 1: Device domain model + DeviceRepository (pgxpool CRUD, maps to `devices` table + migration 003 for whatsapp-specific columns)
- Task 2: ActiveSession in-memory registry (RWMutex, Add/Remove/Get/All/ForEach/StopAll, auto-cancel on replace)
- Task 3: Session Manager with ReconnectAll (semaphore cap 5, jittered stagger, per-device goroutines, exponential backoff)
- Task 4: Composition root wiring in main.go (session manager created before worker, shutdown order: sessions→worker→NATS)

**Files created:**
- `internal/session/device.go` — Device model + DeviceRepository
- `internal/session/registry.go` — ActiveSession (in-memory concurrent registry)
- `internal/session/manager.go` — Session lifecycle manager with reconnect
- `internal/platform/postgres/migrations/003_add_device_whatsapp_fields.sql`

**Files modified:**
- `cmd/omnigo/main.go` — Manager wiring, shutdown registration

**Tests:** 10 tests passing (device: 2, registry: 8)

**Key design decisions:**
- DeviceRepository reuses existing `devices` table (001 migration) + extends with 003 (jid, phone, connected_since)
- ActiveSession uses JID string as key (not device UUID) for dispatcher lookup alignment
- Manager reconnects with semaphore + jitter to prevent WhatsApp server storm
- Sessions auto-update DB status to "disconnected" when Run goroutine exits
- Terminal devices (status="terminal") are skipped during ReconnectAll

**Next:** Plan 03 builds the admin UI for QR pairing and telemetry.
