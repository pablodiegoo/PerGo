# Phase 1: Foundation — Phase Context

## Summary

Phase 1 locks in the expensive-to-retrofit schema decisions before any message flows. By the end of this phase, the PerGo server boots with identity management (workspaces + API keys), AES-256-GCM credential encryption with key rotation support, immutable partitioned audit logging with buffered batch writes, and full observability (health endpoints, pprof, structured tracing, expvar). NATS is present only as a connectivity ping for /readyz — JetStream provisioning and message queuing are deferred to Phase 3.

## Key Architectural Decisions

### PostgreSQL Dual-Access Model
- One `*pgxpool.Pool` for PerGo application queries (workspace/API key CRUD, audit writes)
- One `*sql.DB` via `pgx/v5/stdlib` bridge for whatsmeow's `sqlstore.Container` (NOT `lib/pq`)
- Both share the same database, non-overlapping schemas

### Migration Strategy
- **Goose** manages PerGo-owned tables: `workspaces`, `api_keys`, `devices`, `audit_logs`
- `Container.Upgrade(ctx)` (whatsmeow) manages `whatsmeow_*` tables — called after PerGo migrations at boot
- Embedded SQL migrations via `go:embed`

### Tenant Isolation (Convention, not RLS in M1)
- Every query carries `workspace_id` scoped via `context.Context` — the `platform/postgres` layer provides wrapper helpers that make omission a compile-time error
- RLS policies deferred to M3 (Phase 5), but the tenant-context convention is enforced from the first query

### Audit Partitioning
- `audit_logs` partitioned by **`created_at` range** (monthly), NOT by `workspace_id` — avoids hot partitions on busy tenants
- `fillfactor=100`, BRIN index on `created_at`, no unique constraint (dedup lives upstream)
- Buffered batch writer: `chan Event` (cap 5000) + 2 batch writers → `pgx.CopyFrom` via `pool.Acquire`

### Credential Encryption
- AES-256-GCM with fresh `crypto/rand` 12-byte nonce per `Seal`, nonce prepended to ciphertext
- Envelope pattern: KEK (env var / file) wraps per-credential DEKs
- `key_id`/`key_version` columns present from day one so rotation is a migration, not a schema change
- API keys: SHA-256 hashed with cleartext prefix for lookup (NOT AES encrypted — hash, not cipher)

### Graceful Shutdown Order
1. Stop HTTP listener (Echo.Shutdown with 30s timeout)
2. Drain workers / stop accepting new work
3. Flush audit buffer (wait for batch writers to drain)
4. Close NATS connection
5. Close pgx pool + sql.DB
6. Exit

### Observability
- `/healthz` — always 200 (liveness)
- `/readyz` — 200 only when pgx + NATS pings succeed
- `net/http/pprof` on `localhost:6060` (not the public port)
- `expvar` with counters for API key cache, audit drops, goroutines, memory
- `log/slog` with Trace-ID context propagated through every handler

## Out of Scope for Phase 1

| Item | Reason |
|------|--------|
| Admin panel UI (Echo+Templ+HTMX) | Phase 2 — Admin Shell builds the visual interface |
| NATS JetStream provisioning / queue | Phase 3 — Ingest API & Queue |
| Message ingestion endpoints | Phase 3 |
| WhatsApp Web integration | Phase 4 |
| WABA / Telegram adapters | Phase 5 |
| Webhook delivery | Phase 6 |
| Media / inbound support | Phase 7 |
| RLS policies | Deferred to M3 (Phase 5) for defense-in-depth |
| whatsmeow `sqlstore` encryption | Deferred to Phase 4 (custom store wrapper or pgcrypto) |
| In-house circuit breaker | Deferred to Phase 5 (REST channel wrappers) |

## Research Outputs Consumed

- **STACK.md**: Echo v5 (not v4), pgx/v5 + stdlib bridge, goose/v3, google/uuid, caarlos0/env (optional)
- **ARCHITECTURE.md**: Dual pool constructors, goose + Container.Upgrade, CopyFrom via pool.Acquire, tenant-context pattern, audit partition strategy
- **FEATURES.md**: API key auth, audit logging, credential encryption confirmed as P1 table stakes
- **PITFALLS.md**: Partition by created_at (not workspace_id), key_id columns from M1, nonce-per-seal, tenant convention from first query, buffer Close() race avoidance
- **SUMMARY.md**: Full M1 scope validated — do NOT bring up NATS beyond a connectivity check; ingest handler returns 503 until Phase 3

## Security Posture

Trust boundaries and threat model decisions documented per-plan in PLAN.md `<threat_model>` sections. ASVS L1 (opportunistic) applies — `mitigate` critical/high-severity threats, document rationale for accepted risks.
