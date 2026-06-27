# Roadmap: OmniGo

## Overview

OmniGo is built as a durable work-queue pipeline: a thin ingestion gateway, NATS JetStream as the durability boundary, stateless channel workers behind a plugin Dispatcher interface, PostgreSQL as the system of record for identity and audit, and a server-rendered admin console. The roadmap follows the research-validated ordering — foundation schema decisions that are expensive to retrofit land first, the highest-risk channel (unofficial WhatsApp Web) and all durability machinery land second, and official channels, fallback, webhooks, media, and inbound complete the platform. Each phase delivers a coherent, independently testable capability.

## Phases

**Phase Numbering:**

- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

- [ ] **Phase 1: Foundation** - Server, schema, identity, crypto, audit, observability — the expensive-to-retrofit decisions locked in
- [ ] **Phase 2: Admin Shell** - Server-rendered admin panel for workspace, key, and audit management
- [x] **Phase 3: Ingest API & Queue** - Unified POST /messages endpoint with JetStream durability, backpressure, dedup, and rate limiting (completed 2026-06-26)
- [x] **Phase 4: WhatsApp Web & QR Pairing** - Unofficial WhatsApp Web channel via whatsmeow with multi-session, QR pairing, and ban-risk resilience (completed 2026-06-26)
- [x] **Phase 5: Official Channels & Smart Fallback** - WABA and Telegram adapters with template management, 24h window, and ordered fallback pipeline (completed 2026-06-26)
- [x] **Phase 6: Webhook Delivery & DLQ** - Durable, HMAC-signed webhook delivery with retries and dead-letter queue (completed 2026-06-27)
- [x] **Phase 7: Media & Inbound** - Channel-agnostic media support and inbound message ingestion with webhook forwarding (completed 2026-06-27)

## Phase Details

### Phase 1: Foundation

**Goal**: The server boots with identity, audit, crypto, and observability infrastructure — the schema decisions expensive to retrofit are locked in before any message flows
**Mode**: mvp
**Depends on**: Nothing (first phase)
**Requirements**: INFRA-01, INFRA-02, INFRA-03, INFRA-04, INFRA-05, INFRA-06, AUTH-01, AUTH-02, AUTH-03, SEC-01, SEC-02, SEC-03, SEC-05, AUDIT-01, AUDIT-02, AUDIT-03, OBS-01, OBS-02, OBS-03, OBS-04
**Success Criteria** (what must be TRUE):

  1. Server starts via `cmd/omnigo` composition root, responds on `/healthz` (200) and `/readyz` (200 with pgx + NATS connectivity pings), and shuts down gracefully on SIGTERM (drains JetStream consumers, flushes audit buffer, closes connections within 30s)
  2. Operator can create a workspace and generate an API key; the auth middleware accepts valid keys (SHA-256 hashed with cleartext prefix lookup), rejects invalid ones with structured 401, and serves subsequent requests from in-memory cache (TTL refresh); revocation takes effect immediately via cache invalidation
  3. Every HTTP request carries a Trace-ID propagated through context, structured slog logs, and audit log rows — a single request's trace can be followed end-to-end across all three surfaces
  4. Credentials and session tokens stored in PostgreSQL are AES-256-GCM encrypted with per-row nonces and `key_id`/`key_version` columns for rotation; API keys are SHA-256 hashed with cleartext prefix for lookup
  5. Every database query is scoped to `workspace_id` via enforced tenant-context convention (context carries workspace, queries extract it); the `audit_logs` table is range-partitioned by `created_at` and written via a buffered batch writer (bounded channel + background workers via `pgx.CopyFrom`)

**Plans:** 4 plans
Plans:

- [ ] 01-01-PLAN.md — Server bootstrap: scaffold, Docker Compose, Echo v5, pgxpool, goose migrations, health endpoints
- [ ] 01-02-PLAN.md — Identity & auth: workspace/API key CRUD, SHA-256 hashing, AES-256-GCM encryption, tenant context, auth middleware
- [ ] 01-03-PLAN.md — Audit logging: trace middleware, slog integration, partitioned audit_logs, buffered batch writer
- [ ] 01-04-PLAN.md — Observability & shutdown: pprof, expvar, graceful shutdown orchestrator

### Phase 2: Admin Shell

**Goal**: Operators can manage workspaces, API keys, and review audit logs through a server-rendered admin panel built on Echo + Templ + HTMX
**Mode**: mvp
**Depends on**: Phase 1
**Requirements**: ADMIN-01, ADMIN-02, ADMIN-05, AUDIT-04
**Success Criteria** (what must be TRUE):

  1. Operator can create, view, and manage multi-tenant workspaces via the admin panel (Echo + Templ + HTMX with HTMX fragment detection — interactions return fragments without full-page reloads)
  2. Operator can generate new API keys per workspace and view/revoke existing keys from the admin panel
  3. Operator can search, filter (by workspace, trace_id, time range), and export audit logs from the admin panel; audit log access is also available via API

**Plans:** 3 plans
Plans:

- [ ] 02-01-PLAN.md — Admin shell foundation: templ, session auth, base layout, sidebar, dashboard, HTMX, CSS
- [ ] 02-02-PLAN.md — Workspace and API key management: CRUD handlers, templates, modal confirmations
- [ ] 02-03-PLAN.md — Audit log review and CSV export: filtering, pagination, CSV download

**UI hint**: yes

### Phase 3: Ingest API & Queue

**Goal**: The unified `POST /messages` endpoint accepts, validates, and durably enqueues messages with backpressure, dedup, rate limiting, and formal status/error contracts — the first half of the send path (worker dispatches via a stub until Phase 4 wires a real channel)
**Mode**: mvp
**Depends on**: Phase 1
**Requirements**: API-01, API-02, API-03, API-04, API-05, QUEUE-01, QUEUE-02, QUEUE-03, QUEUE-04, QUEUE-05
**Success Criteria** (what must be TRUE):

  1. `POST /messages` accepts a standard JSON payload, validates fields (returning structured 400 with `{code, message, more_info}` error body and field-level details on invalid input), generates a Trace-ID, and returns `202 Accepted` with the trace header
  2. Messages flow through a formal status enum (queued → sent → delivered → read → failed) with defined state transitions (terminal vs retriable vs fallback-triggering); a NATS JetStream `WorkQueuePolicy` stream provides durable at-least-once delivery with automatic retries and exponential backoff (`MaxDeliver` + `AckWait`/`MaxBackoff`, NAK-with-delay)
  3. When a session exceeds 1,000 queued messages, `POST /messages` returns 429/422 with a `Retry-After` header (backpressure enforced before enqueue)
  4. Duplicate publishes (same `trace_id`) are deduplicated via `Nats-Msg-Id`; a `dispatched_messages` dedup set prevents duplicate delivery on redelivery
  5. Per-session rate limiting applies staggered dispatch (1-3s random delay) for unofficial WhatsApp channels via `golang.org/x/time/rate`; message TTL (`ttl_seconds`) causes expired messages to be dropped instead of sent late

**Plans:** 3/3 plans complete
Plans:

- [x] 03-01-PLAN.md — Domain types, validation, and POST /messages handler (202 + trace + structured errors)
- [x] 03-02-PLAN.md — JetStream durability: WorkQueue stream, publisher with dedup, worker stub, lifecycle wiring
- [x] 03-03-PLAN.md — Rate limiting, backpressure, retry with backoff, TTL enforcement, delivery dedup

### Phase 4: WhatsApp Web & QR Pairing

**Goal**: Messages dispatch end-to-end through WhatsApp Web (unofficial via whatsmeow) with multi-session management, QR pairing, and ban-risk resilience — completing the first real send path
**Mode**: mvp
**Depends on**: Phase 2, Phase 3
**Requirements**: WAWEB-01, WAWEB-02, WAWEB-03, WAWEB-04, WAWEB-05, WAWEB-06, WAWEB-07, SEC-04, INFRA-07, ADMIN-03, ADMIN-04, ADMIN-06
**Success Criteria** (what must be TRUE):

  1. Operator can pair a WhatsApp Web device by scanning a QR code displayed in the admin panel (dynamically refreshed via HTMX/SSE); the UI prominently displays a ban-risk warning before pairing so operators do not pair business-critical numbers unknowingly
  2. Messages queued in Phase 3 are dispatched through the WhatsApp Web adapter (whatsmeow) implementing the `Dispatcher` interface; multi-session connection manager maintains per-device goroutines with in-memory registry; device sessions persist across server restarts with automatic reconnection
  3. On startup, the session manager reconnects all known devices with backoff and storm protection (semaphore cap + jittered backoff over `GetAllDevices`); on "client outdated" events, the WA Web version auto-refreshes (`SetWAVersion`) and retries
  4. When WhatsApp forces logout (`LoggedOut` / 403), the session is marked terminal, the operator is alerted, and no retry loop occurs; the message triggers fallback (if configured) rather than hanging
  5. whatsmeow device keys are encrypted at rest (custom store wrapper or `pgcrypto` bridging the plaintext storage gap); whatsmeow is pinned to a dated pseudo-version (not `@latest`) with a documented upgrade ritual; operators can view real-time session status, queue depths, and channel health on the admin telemetry panel

**Plans:** 3/3 plans complete
Plans:

- [x] 04-01-PLAN.md — Dispatcher interface, Registry, WhatsAppAdapter with staggered dispatch, Worker wiring
- [x] 04-02-PLAN.md — Session manager, device repository, startup reconnect, lifecycle events
- [x] 04-03-PLAN.md — QR pairing UI, device management page, telemetry dashboard

**UI hint**: yes

### Phase 5: Official Channels & Smart Fallback

**Goal**: Messages can be sent through WABA and Telegram (official, stateless REST channels), and the smart fallback pipeline routes through an ordered channel array with terminal-error classification and fallback-aware dedup
**Mode**: mvp
**Depends on**: Phase 4
**Requirements**: WABA-01, WABA-02, WABA-03, WABA-04, TGRAM-01, TGRAM-02, FALL-01, FALL-02, FALL-03
**Success Criteria** (what must be TRUE):

  1. WABA adapter sends messages via the WhatsApp Cloud REST API implementing the `Dispatcher` interface, including template-based messaging (`template_name`, `language`, `components` fields) with 24-hour customer service window awareness (template-only outside window, fallback triggered when window expires)
  2. Operator can manage WABA templates (create, list, submit for Meta approval, track approval status) via the admin panel or API
  3. Telegram adapter sends messages via the Telegram Bot HTTP API implementing the `Dispatcher` interface and sets up inbound webhooks (`setWebhook` with `secret_token` authentication)
  4. Smart fallback iterates through an ordered `fallback_channels` array: on primary channel failure, the next channel is attempted sequentially (not parallel); terminal errors (`ErrTerminal` typed) advance fallback immediately without retry
  5. Fallback-aware dedup prevents redelivery via a fallback channel if the message was already delivered by the primary channel (records which channel succeeded so a redelivery does not re-attempt the next fallback channel)

**Plans**: TBD
**UI hint**: yes

### Phase 6: Webhook Delivery & DLQ

**Goal**: Status events are delivered to consumer applications via durable, HMAC-signed webhooks with retries and a dead-letter queue for permanent failures — the production-blocking safety gaps (unsigned webhooks, no payload schema) are closed
**Mode**: mvp
**Depends on**: Phase 5
**Requirements**: WHOOK-01, WHOOK-02, WHOOK-03, WHOOK-04, WHOOK-05
**Success Criteria** (what must be TRUE):

  1. Webhook events are delivered via a dedicated JetStream consumer (separate stream with `LimitsPolicy`, durable, retried with exponential backoff `MaxDeliver: 10`, `AckWait` scaling 1s to 10m) — not fire-and-forget
  2. Webhook requests are HMAC-signed so consumers can verify sender identity; the formal payload schema includes status events, `trace_id`, `message_id`, `channel`, and `timestamp`
  3. Permanently failed webhook deliveries (exhausted retries) are moved to a dead-letter queue (`webhooks_dlq`) and surfaced on the admin console for operator inspection

**Plans**: TBD
**UI hint**: yes

### Phase 7: Media & Inbound

**Goal**: Messages with media (image, document, audio) are delivered across channels with channel-agnostic abstraction, and inbound messages from all providers are ingested and forwarded to consumer applications with audit correlation
**Mode**: mvp
**Depends on**: Phase 6
**Requirements**: MEDIA-01, MEDIA-02, MEDIA-03, INBD-01, INBD-02, INBD-03
**Success Criteria** (what must be TRUE):

  1. `POST /messages` accepts a unified media field (image, document, audio) with channel-agnostic abstraction; media is delivered via per-channel upload/download paths (WhatsApp media upload ID, Telegram `sendPhoto`/`sendDocument`, WABA media URL)
  2. Media storage policy is enforced (URL-pass-through or local blob storage with size limits) per configured policy
  3. Inbound messages from all providers (WhatsApp Web via whatsmeow event handlers, WABA inbound webhooks, Telegram `getUpdates`/webhook) are ingested and forwarded to the consumer application via durable, retried webhooks
  4. Inbound messages are audit-logged with Trace-ID correlation (same end-to-end traceability as outbound)

**Plans**: TBD

## Progress

**Execution Order:**
Phases execute in numeric order: 1 → 2 → 3 → 4 → 5 → 6 → 7

Note: Phase 2 and Phase 3 are independent after Phase 1 and may execute in parallel. Phase 4 depends on both.

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Foundation | 0/4 | Not started | - |
| 2. Admin Shell | 0/3 | Not started | - |
| 3. Ingest API & Queue | 3/3 | Complete    | 2026-06-26 |
| 4. WhatsApp Web & QR Pairing | 3/3 | Complete    | 2026-06-26 |
| 5. Official Channels & Smart Fallback | 4/4 | Complete    | 2026-06-26 |
| 6. Webhook Delivery & DLQ | 1/1 | Complete    | 2026-06-27 |
| 7. Media & Inbound | 4/4 | Complete    | 2026-06-27 |

---
*Roadmap created: 2026-06-25*
*Granularity: standard | Mode: mvp | Phase convention: sequential*

### Phase 07.1: Close gap: v1.0 audit gaps (INSERTED)

**Goal:** [Urgent work - to be planned]
**Requirements**: TBD
**Depends on:** Phase 7
**Plans:** 0 plans

Plans:

- [ ] TBD (run /gsd-plan-phase 07.1 to break down)
