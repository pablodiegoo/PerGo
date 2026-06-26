# Phase 4: WhatsApp Web & QR Pairing - Context

**Gathered:** 2026-06-26
**Status:** Ready for planning
**Mode:** Auto-generated (smart discuss — autonomous mode, recommended defaults applied)

<domain>
## Phase Boundary

Messages dispatch end-to-end through WhatsApp Web (unofficial via whatsmeow) with multi-session management, QR pairing, and ban-risk resilience — completing the first real send path. This phase replaces the Worker's stub dispatch with a real WhatsApp Web adapter and adds the operator-facing QR pairing UI to the admin panel.

</domain>

<decisions>
## Implementation Decisions

### whatsmeow Integration Architecture
- whatsmeow client lives in `internal/channel/whatsapp/` — new top-level `channel/` package for all adapter implementations
- Worker calls a `Dispatcher` interface injected via constructor; Phase 4 provides `WhatsAppDispatcher` implementation
- Session persistence via whatsmeow's built-in `sqlstore.Container` with PostgreSQL via `pgx/v5/stdlib` bridge
- whatsmeow plaintext device keys accepted as DB-level encryption boundary for MVP — documented risk; key encryption deferred to security hardening follow-up

### Session Lifecycle & Ban-Risk Resilience
- Multi-session reconnection on startup: SessionManager reads active devices from DB, starts goroutines with semaphore cap (5 concurrent) and jittered backoff (1-5s random delay) to prevent thundering herd
- WhatsApp forced logout (LoggedOut/403): Mark session `terminal` in DB, emit audit event, alert operator via slog warning + admin panel badge
- "Client outdated" version refresh: Hook whatsmeow's `StreamEvents`, detect `ClientOutdated`, call `cli.SetWAVersion()` with latest version, auto-reconnect
- Staggered dispatch: 1-3s random delay (`time.Sleep`) before each WhatsApp message send in the dispatcher goroutine

### QR Pairing & Admin UI
- New admin page `/admin/devices` with "Link Device" button; HTMX replaces target div with QR base64 image; SSE for auto-refresh when QR expires
- Prominent yellow warning banner above QR code: "⚠️ WhatsApp Web is unofficial — pairing business numbers risks account suspension. Use test numbers only."
- New `/admin/telemetry` page: device table (JID, status, connected-since, messages-sent, last-error), queue depth from expvar, NATS connection status

### the agent's Discretion
- whatsmeow pseudo-version pinning strategy (exact commit hash vs. go.sum pin)
- SSE vs. HTMX polling for QR refresh (SSE preferred per context, but polling acceptable if simpler)
- Exact layout/styling of devices and telemetry admin pages (follow Phase 2 admin shell patterns)

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/platform/queue/worker.go` — Worker with `dispatch()` stub method ready for real Dispatcher injection; retry/TTL/dedup already implemented
- `internal/platform/queue/jetstream.go` — JetStreamPublisher, EnsureStream, stream configuration
- `internal/domain/message.go` — CreateMessageRequest with `Channel` field ("whatsapp"), MessageStatus enum
- Phase 2 admin shell: Templ templates, HTMX fragments, `/admin/` route prefix, sidebar navigation
- Phase 1: pgxpool, `pgx/v5/stdlib` bridge, audit batch writer, tenant context, crypto (AES-256-GCM)

### Established Patterns
- Repository pattern: `internal/repository/` for database operations
- Middleware pattern: `internal/api/middleware/` for cross-cutting concerns
- Handler pattern: `internal/api/handler/` (public) + `internal/api/handler/admin/` (admin)
- Templ templates: `templates/components/`, `templates/pages/`, `templates/layout/`
- Composition root: `cmd/omnigo/main.go` wires all dependencies

### Integration Points
- Worker needs a `Dispatcher` interface — currently `dispatch()` is a private method returning nil
- `internal/platform/postgres/pool.go` provides `NewSQLDB` (stdlib bridge) needed for whatsmeow's `sqlstore.Container`
- Admin routes mount on existing Echo instance under `/admin/`
- whatsmeow events (LoggedOut, ClientOutdated, etc.) feed into slog and audit

</code_context>

<specifics>
## Specific Ideas

- The Dispatcher interface should be generic enough for Phase 5 (WABA, Telegram) — method signature like `Dispatch(ctx, message) error`
- QR pairing should feel magical — operator clicks button, scans with phone, done. No manual token entry.
- Staggered dispatch is critical for ban-risk — even a small random delay (1-3s) dramatically reduces detection
- The admin telemetry page should update without full page reload (HTMX or SSE polling)

</specifics>

<deferred>
## Deferred Ideas

- Custom whatsmeow device key encryption wrapper (security hardening — post-MVP)
- whatsmeow upgrade ritual documentation (INFRA-07 — include in phase but not a blocking concern)
- Multi-user admin authentication with roles (MVP uses single-operator model)
- Real-time WebSocket dashboard updates (SSE/HTMX polling sufficient for MVP)

</deferred>
