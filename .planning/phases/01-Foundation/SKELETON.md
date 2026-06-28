# Walking Skeleton — PerGo

**Phase:** 1
**Generated:** 2026-06-25

## Capability Proven End-to-End

The server boots, connects to PostgreSQL and NATS, serves health checks, authenticates API requests via hashed API keys, logs audit events with trace correlation, exposes debug metrics, and shuts down gracefully on SIGTERM — the full foundation stack operational before any message flows.

## Architectural Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Framework | Echo v5.2.1 (`github.com/labstack/echo/v5`) | Current major line (v4 EOL 2026-12-31); native `*slog.Logger` integration; handler signature `func(c *echo.Context) error` |
| Data layer | PostgreSQL 16+ via pgx/v5 (pgxpool + stdlib bridge) | Binary protocol, prepared-statement cache, native COPY for audit batch writer; stdlib bridge for goose and future whatsmeow |
| Migrations | goose/v3 with embedded SQL via `go:embed` | One file per migration, versioned, PostgreSQL dialect, `database/sql` consumer via stdlib bridge |
| Auth | SHA-256 hashed API keys with cleartext prefix for lookup | Hash is one-way (no decryption needed for auth); prefix enables O(1) cache lookup without DB roundtrip |
| Encryption | AES-256-GCM envelope (KEK wraps DEK, nonce per Seal) | Hardware-accelerated; `key_id`/`key_version` columns from day one enable rotation via migration |
| Tenant isolation | Context convention (workspace_id in `context.Context`) | Enforced from first query via wrapper helpers; RLS deferred to Phase 5 |
| Audit | Partitioned by `created_at` range (monthly), buffered batch writer | Avoids hot partitions on busy tenants; `pgx.CopyFrom` for bulk inserts via bounded channel + 2 workers |
| Observability | `/healthz` + `/readyz`, pprof on :6060, expvar, slog with Trace-ID | Liveness/readiness probes; runtime profiling; structured logging with end-to-end trace correlation |
| Deployment | Docker Compose (pergo + postgres + nats) | Single-command local dev stack; mirrors integration-test topology |
| Directory layout | `cmd/pergo/` entry point, `internal/platform/` infrastructure, `internal/api/handler/` HTTP handlers | Standard Go project layout; platform layer isolates infrastructure concerns |

## Stack Touched in Phase 1

- [x] Project scaffold — Go module, Docker Compose, Makefile, directory structure
- [x] Routing — Echo v5 with health endpoints (`/healthz`, `/readyz`)
- [x] Database — pgxpool for app queries, stdlib bridge for goose; goose migrations for `workspaces`, `api_keys`, `devices`, `audit_logs`
- [x] Auth — SHA-256 API key hashing, in-memory cache with TTL, revocation support
- [x] Encryption — AES-256-GCM envelope with `key_id`/`key_version` for credential storage
- [x] Audit — Partitioned `audit_logs` table, buffered batch writer with `pgx.CopyFrom`
- [x] Observability — pprof, expvar, slog with Trace-ID context propagation
- [x] Shutdown — Graceful SIGTERM handling with ordered resource cleanup

## Out of Scope (Deferred to Later Slices)

- Admin panel UI (Echo + Templ + HTMX) — Phase 2
- NATS JetStream provisioning / work-queue stream — Phase 3
- Message ingestion endpoints (`POST /messages`) — Phase 3
- WhatsApp Web integration (whatsmeow) — Phase 4
- WABA / Telegram adapters — Phase 5
- Webhook delivery and DLQ — Phase 6
- Media and inbound support — Phase 7
- RLS policies — Deferred to Phase 5
- whatsmeow `sqlstore` encryption — Deferred to Phase 4
- In-house circuit breaker — Deferred to Phase 5
- OpenTelemetry SDK — Deferred until tracing backend introduced
- Prometheus metrics — Deferred until scraping infrastructure exists

## Subsequent Slice Plan

Each later phase adds one vertical slice on top of this skeleton without altering its architectural decisions:

- Phase 2: Server-rendered admin panel for workspace, key, and audit management (Echo + Templ + HTMX)
- Phase 3: Unified `POST /messages` endpoint with JetStream durability, backpressure, dedup, rate limiting
- Phase 4: WhatsApp Web channel via whatsmeow with multi-session, QR pairing, ban-risk resilience
- Phase 5: WABA and Telegram adapters with template management, smart fallback pipeline
- Phase 6: Durable, HMAC-signed webhook delivery with retries and dead-letter queue
- Phase 7: Channel-agnostic media support and inbound message ingestion
