# Requirements: OmniGo

**Defined:** 2026-06-25
**Core Value:** A single API request delivers a message through any configured channel with automatic fallback — without per-message markup, without vendor lock-in, and with full custody of transaction data on infrastructure you control.

## v1 Requirements

Requirements for initial release. Each maps to roadmap phases.

### API

- [x] **API-01**: Unified `POST /messages` endpoint accepts standard JSON payload, validates fields, generates Trace-ID, returns `202 Accepted` with trace header
- [x] **API-02**: Payload validation returns structured `400` error responses with field-level error details
- [x] **API-03**: Formal message status enum (queued, sent, delivered, read, failed) with defined state transitions (terminal vs retriable vs fallback-triggering)
- [x] **API-04**: Public REST error response format with numeric error code catalog (`code`, `message`, `more_info`) for programmatic consumer branching
- [x] **API-05**: Message TTL / validity period via `ttl_seconds` field — queued messages expire instead of sending late

### Authentication

- [ ] **AUTH-01**: API key authentication via SHA-256 hashed keys with cleartext prefix for lookup
- [ ] **AUTH-02**: API key revocation (revoked_at timestamp, immediate cache invalidation)
- [ ] **AUTH-03**: In-memory API key cache with TTL refresh to keep ingest path off the database

### Queue

- [x] **QUEUE-01**: NATS JetStream work-queue stream for outbound message durability (at-least-once, single consumer per subject)
- [x] **QUEUE-02**: Backpressure: 1,000-message per-session limit enforced before enqueue with HTTP 429/422 + `Retry-After` header
- [x] **QUEUE-03**: Dedup mechanism: `Nats-Msg-Id = trace_id` for publish-side idempotency + `dispatched_messages` dedup set to prevent duplicate delivery on redelivery
- [x] **QUEUE-04**: Per-session rate limiting with staggered dispatch (1-3s random delay) for unofficial WhatsApp channels via `golang.org/x/time/rate`
- [x] **QUEUE-05**: Automatic retries with exponential backoff (JetStream `MaxDeliver` + `AckWait`/`MaxBackoff`, NAK-with-delay)

### WhatsApp Web

- [x] **WAWEB-01**: WhatsApp Web adapter via whatsmeow implementing the `Dispatcher` interface
- [x] **WAWEB-02**: Multi-session connection manager (in-memory `sync.RWMutex` registry, per-device goroutines)
- [x] **WAWEB-03**: QR code pairing via admin panel with dynamically refreshed QR display
- [x] **WAWEB-04**: Persistent session store in PostgreSQL (device identity survives server restart, auto-reconnect)
- [x] **WAWEB-05**: Reconnect on restart with backoff and storm protection (semaphore + jitter over `GetAllDevices`)
- [x] **WAWEB-06**: WhatsApp Web version auto-refresh (`SetWAVersion` on "client outdated" events)
- [x] **WAWEB-07**: Terminal session handling for `LoggedOut` / forced `403` events (mark session dead, alert operator, stop retry)

### WhatsApp Cloud (WABA)

- [x] **WABA-01**: WhatsApp Cloud API (WABA) REST adapter implementing the `Dispatcher` interface
- [x] **WABA-02**: Template management CRUD (create, list, submit for Meta approval, track approval status)
- [x] **WABA-03**: Template-based message sending (`template_name`, `language`, `components` fields in payload)
- [x] **WABA-04**: 24-hour customer service window awareness (track window per recipient, enforce template-only outside window, trigger fallback when window expired)

### Telegram

- [x] **TGRAM-01**: Telegram Bot API adapter implementing the `Dispatcher` interface
- [x] **TGRAM-02**: Telegram inbound webhook setup (`setWebhook` with `secret_token` authentication)

### Media

- [ ] **MEDIA-01**: Unified media field in message payload (image, document, audio) with channel-agnostic abstraction
- [ ] **MEDIA-02**: Per-channel media upload/download paths (WhatsApp media upload ID, Telegram `sendPhoto`/`sendDocument`, WABA media URL)
- [ ] **MEDIA-03**: Media storage policy (URL-pass-through or local blob storage with size limits)

### Inbound

- [ ] **INBD-01**: Inbound message ingestion from providers (WhatsApp Web via whatsmeow event handlers, WABA inbound webhooks, Telegram `getUpdates`/webhook)
- [ ] **INBD-02**: Forward inbound messages to consumer application via webhook (durable, retried)
- [ ] **INBD-03**: Inbound message audit logging with Trace-ID correlation

### Fallback

- [x] **FALL-01**: Ordered `fallback_channels` array with iterative dispatch (try primary, on failure try next)
- [x] **FALL-02**: Terminal-error classification (`ErrTerminal` typed errors advance fallback immediately without retry)
- [x] **FALL-03**: Fallback-aware dedup (prevent redelivery via fallback channel if already delivered by primary)

### Webhooks

- [x] **WHOOK-01**: Outbound webhook delivery via dedicated JetStream consumer (durable, retried, not fire-and-forget)
- [x] **WHOOK-02**: Webhook HMAC request signing (authenticated delivery — consumer can verify sender identity)
- [x] **WHOOK-03**: Formal webhook payload schema (status events, trace_id, message_id, channel, timestamp)
- [x] **WHOOK-04**: Webhook dead-letter queue for permanently failed deliveries (surfaced on admin console)
- [x] **WHOOK-05**: Webhook retry policy with exponential backoff (`MaxDeliver: 10`, `AckWait` 1s to 10m)

### Audit

- [ ] **AUDIT-01**: Immutable `audit_logs` table partitioned by `created_at` (range partitioning for append-only workload)
- [ ] **AUDIT-02**: Trace-ID propagation across all context boundaries (HTTP request → NATS message headers → worker `context.Context` → SQL transaction)
- [ ] **AUDIT-03**: Buffered batch writer for audit inserts (bounded channel + background workers, protects DB from write spikes)
- [ ] **AUDIT-04**: Audit log access via both API and admin dashboard (filterable by workspace, trace_id, time range)

### Admin

- [ ] **ADMIN-01**: Server-rendered admin panel (Echo + Templ + HTMX, HTMX fragment detection)
- [ ] **ADMIN-02**: Multi-tenant workspace management (create, isolate, manage scoped API keys)
- [x] **ADMIN-03**: Connection telemetry display (real-time session status, queue depths, channel health)
- [x] **ADMIN-04**: QR code pairing interface (dynamically refreshed QR for WhatsApp Web device linking)
- [ ] **ADMIN-05**: Audit log review interface (searchable, filterable, exportable)
- [x] **ADMIN-06**: Ban-risk warning displayed on QR pairing UI (operators must not pair business-critical numbers unknowingly)

### Security

- [ ] **SEC-01**: AES-256-GCM encryption at rest for session tokens and channel credentials (per-row nonce)
- [ ] **SEC-02**: SHA-256 hashing for API keys (prefix stored cleartext for lookup, secret hashed)
- [ ] **SEC-03**: Multi-tenant data isolation with enforced tenant-context convention (every query scoped to `workspace_id`, RLS or equivalent guard against cross-tenant leaks)
- [x] **SEC-04**: whatsmeow device key encryption (custom store wrapper or `pgcrypto` — bridge the plaintext storage gap in whatsmeow's internal `whatsmeow_device` table)
- [ ] **SEC-05**: Key management with `key_id`/`key_version` columns for encryption key rotation

### Observability

- [ ] **OBS-01**: Health and readiness endpoints (`/healthz` liveness, `/readyz` with pgx ping + nats ping)
- [ ] **OBS-02**: `net/http/pprof` runtime profiling integration (CPU, memory, goroutine diagnostics)
- [ ] **OBS-03**: Structured logging via `log/slog` with Trace-ID context propagation
- [ ] **OBS-04**: `expvar` metrics exposure (memory utilization, queue depths, execution latencies)

### Infrastructure

- [ ] **INFRA-01**: Go 1.25+ with Echo v5 HTTP framework (v5 native `*slog.Logger` integration, `*echo.Context` handler signature)
- [ ] **INFRA-02**: PostgreSQL via pgx/v5 with dual-access model (pgxpool for OmniGo queries + `pgx/v5/stdlib` bridge for whatsmeow `database/sql` and goose migrations)
- [ ] **INFRA-03**: Database migrations via goose with embedded SQL files (versioned, one file per migration)
- [ ] **INFRA-04**: Docker Compose deployment topology (omnigo + postgres + nats)
- [ ] **INFRA-05**: Graceful shutdown (drain JetStream consumers, flush audit buffer, close connections, stop HTTP listener)
- [ ] **INFRA-06**: Makefile with run, test, lint, `templ generate`, migrate targets
- [x] **INFRA-07**: whatsmeow pseudo-version pinning (dated commit-hash, not `@latest`) with documented upgrade ritual

## v2 Requirements

Deferred to future release. Tracked but not in current roadmap.

### Robustness

- **ROBUST-01**: Per-destination rate limiting (complement per-session limiting to prevent single-recipient blasting)
- **ROBUST-02**: Optional `Idempotency-Key` header for caller-supplied dedup (stricter exactly-once-publish semantics)
- **ROBUST-03**: Per-workspace sender routing policy (tenant A uses WABA, tenant B uses WhatsApp Web pool)

### Scheduling

- **SCHED-01**: Message scheduling via `send_at` field (timezone-aware, recipient-local delivery)

### Compliance

- **COMP-01**: Content redaction / PII retention controls
- **COMP-02**: Contact / consent management API

### Observability

- **OBSV-01**: OpenTelemetry distributed tracing export (add when tracing backend introduced)
- **OBSV-02**: Prometheus metrics exporter (add when scraping infrastructure exists)

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| Real-time Voice / WebRTC / SIP trunking | Different latency/protocol stack (Pion, RTP, SIP); orthogonal to transactional messaging — PRD §7 |
| Visual conversation flow builder / bot designer | OmniGo is a backend router, not a bot platform; flow builders become full apps — PRD §7 |
| Group/community management (create, permissions, announcements) | Different domain (membership CRUD, permissions); high API surface — PRD §7. Group JID targeting allowed, no admin features. |
| Kafka as broker | Operational weight unjustified at 500 req/s; NATS JetStream covers durability + queue groups — arch 02 |
| Redis / memcached cache layer | Unmeasured need at 500 req/s; API-key auth fits in-memory map with TTL — arch 02 |
| gRPC internal mesh | Single binary; REST + JetStream suffice; protobuf tooling burden unjustified — arch 02 |
| ORM / query builder / DI framework | Hand-written SQL with pgx is clearer for small known query set — arch 02 |
| OpenTelemetry SDK in MVP | Trace-ID via context + NATS headers + slog meets 100% correlation SLO; add OTel only with tracing backend — arch 02 |
| SMS / MMS / RCS channels | Different regulatory stack (A2P 10DLC, carrier registration); separate product domain |
| Phone number purchasing / porting | Telecom regulatory workflow; OmniGo has no carrier relationships |
| Per-message billing / metering | Self-hosted = no billing; adding metering couples to payments stack |
| AI / LLM message generation | OmniGo routes, doesn't compose; consumer app generates content |
| Link shortening + click tracking | Marketing feature; pulls in URL storage, redirect service — OmniGo is transactional |
| Built-in conversation state machine | Couples router to business logic; session windows belong to consumer app |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| API-01 | Phase 3 | Complete |
| API-02 | Phase 3 | Complete |
| API-03 | Phase 3 | Complete |
| API-04 | Phase 3 | Complete |
| API-05 | Phase 3 | Complete |
| AUTH-01 | Phase 1 | Pending |
| AUTH-02 | Phase 1 | Pending |
| AUTH-03 | Phase 1 | Pending |
| QUEUE-01 | Phase 3 | Complete |
| QUEUE-02 | Phase 3 | Complete |
| QUEUE-03 | Phase 3 | Complete |
| QUEUE-04 | Phase 3 | Complete |
| QUEUE-05 | Phase 3 | Complete |
| WAWEB-01 | Phase 4 | Complete |
| WAWEB-02 | Phase 4 | Complete |
| WAWEB-03 | Phase 4 | Complete |
| WAWEB-04 | Phase 4 | Complete |
| WAWEB-05 | Phase 4 | Complete |
| WAWEB-06 | Phase 4 | Complete |
| WAWEB-07 | Phase 4 | Complete |
| WABA-01 | Phase 5 | Complete |
| WABA-02 | Phase 5 | Complete |
| WABA-03 | Phase 5 | Complete |
| WABA-04 | Phase 5 | Complete |
| TGRAM-01 | Phase 5 | Complete |
| TGRAM-02 | Phase 5 | Complete |
| MEDIA-01 | Phase 7 | Pending |
| MEDIA-02 | Phase 7 | Pending |
| MEDIA-03 | Phase 7 | Pending |
| INBD-01 | Phase 7 | Pending |
| INBD-02 | Phase 7 | Pending |
| INBD-03 | Phase 7 | Pending |
| FALL-01 | Phase 5 | Complete |
| FALL-02 | Phase 5 | Complete |
| FALL-03 | Phase 5 | Complete |
| WHOOK-01 | Phase 6 | Complete |
| WHOOK-02 | Phase 6 | Complete |
| WHOOK-03 | Phase 6 | Complete |
| WHOOK-04 | Phase 6 | Complete |
| WHOOK-05 | Phase 6 | Complete |
| AUDIT-01 | Phase 1 | Pending |
| AUDIT-02 | Phase 1 | Pending |
| AUDIT-03 | Phase 1 | Pending |
| AUDIT-04 | Phase 2 | Pending |
| ADMIN-01 | Phase 2 | Pending |
| ADMIN-02 | Phase 2 | Pending |
| ADMIN-03 | Phase 4 | Complete |
| ADMIN-04 | Phase 4 | Complete |
| ADMIN-05 | Phase 2 | Pending |
| ADMIN-06 | Phase 4 | Complete |
| SEC-01 | Phase 1 | Pending |
| SEC-02 | Phase 1 | Pending |
| SEC-03 | Phase 1 | Pending |
| SEC-04 | Phase 4 | Complete |
| SEC-05 | Phase 1 | Pending |
| OBS-01 | Phase 1 | Pending |
| OBS-02 | Phase 1 | Pending |
| OBS-03 | Phase 1 | Pending |
| OBS-04 | Phase 1 | Pending |
| INFRA-01 | Phase 1 | Pending |
| INFRA-02 | Phase 1 | Pending |
| INFRA-03 | Phase 1 | Pending |
| INFRA-04 | Phase 1 | Pending |
| INFRA-05 | Phase 1 | Pending |
| INFRA-06 | Phase 1 | Pending |
| INFRA-07 | Phase 4 | Complete |

**Coverage:**

- v1 requirements: 66 total
- Mapped to phases: 66
- Unmapped: 0

**Per-phase summary:**

| Phase | Requirements | Count |
|-------|-------------|-------|
| 1. Foundation | INFRA-01–06, AUTH-01–03, SEC-01,02,03,05, AUDIT-01–03, OBS-01–04 | 20 |
| 2. Admin Shell | ADMIN-01,02,05, AUDIT-04 | 4 |
| 3. Ingest API & Queue | API-01–05, QUEUE-01–05 | 10 |
| 4. WhatsApp Web & QR Pairing | WAWEB-01–07, SEC-04, INFRA-07, ADMIN-03,04,06 | 12 |
| 5. Official Channels & Smart Fallback | WABA-01–04, TGRAM-01,02, FALL-01–03 | 9 |
| 6. Webhook Delivery & DLQ | WHOOK-01–05 | 5 |
| 7. Media & Inbound | MEDIA-01–03, INBD-01–03 | 6 |

---
*Requirements defined: 2026-06-25*
*Last updated: 2026-06-25 after roadmap creation*
