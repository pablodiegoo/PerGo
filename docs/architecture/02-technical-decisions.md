# 2. Technical Decisions

## Libraries

| Concern | Choice | Justification |
|---------|--------|---------------|
| HTTP router | **`net/http` (Go 1.22+)** + `chi` *only if* middleware ergonomics demand it | Go 1.22 added method-pattern routing to the std lib. The PRD names Echo; we honour that as the *baseline* but note `net/http`+`chi` removes a dependency and a middleware model we do not need. **Decision: Echo**, per PRD, for handler ergonomics and middleware parity with the admin stack — but keep handlers thin and std-`http.Handler`-compatible so the router is swappable. |
| Templating (admin) | **`a-h/templ`** | Compile-time type-safe HTML → Go code, zero runtime parse cost. Matches PRD and the HTMX fragment model. |
| Client interactivity | **HTMX** | No JS bundle, server-driven fragments. Fits the "operator console, not SPA" scope. |
| PostgreSQL driver | **`jackc/pgx/v5`** (`pgxpool`) | Binary protocol, prepared-statement cache, native `COPY`, batch pipeline. `database/sql` + `sqlx` is acceptable but pays a `interface{}`-scan tax and a stdlib abstraction we do not use. **pgx is the right call** for the audit batch writer. |
| Migrations | **`pressly/goose`** or embed SQL + `golang-migrate` | One file per migration, versioned, no ORM. |
| Messaging broker | **`nats-io/nats.go`** + JetStream | Work-queue streams, durable consumers, headers (for Trace-ID), Pull consumers with `MaxDeliver` for retry. Official client, no abstraction layer on top. |
| WhatsApp Web | **`whatsmeow`** | Only viable Go library for multi-device WhatsApp Web; required, not a preference. |
| Rate limiting | **`golang.org/x/time/rate`** | Token-bucket, `Limiter.Wait(ctx)` yields to the scheduler — exactly the per-session staggered-dispatch requirement. |
| Logging | **`log/slog`** (std lib, Go 1.21+) | Structured, leveled, context-aware. No zerolog/zap unless a benchmark proves the need. |
| Tracing/metrics | **`expvar` + `net/http/pprof`** for diagnostics; **Prometheus exporter via `prometheus/client_golang`** *only if* a scraping infra exists | pprof is in the std lib and mandatory per PRD. Prometheus is the one external observability dep worth taking. |
| UUID | **`gofrs/uuid` v5 or `google/uuid`** | Trace-ID generation. std `crypto/rand` + custom RFC4122 formatter is possible but not worth the LoC. |
| Config | **`caarlos0/env`** or plain `os.Getenv` + struct tags | No viper, no YAML daemon. 12-factor env vars. |
| Crypto | **`crypto/aes`, `crypto/cipher` (GCM)** — std lib | AES-256-GCM for credential encryption at rest. `crypto/sha256` for API-key hashing. No external crypto. |
| Backoff | **`cenkalti/backoff/v4`** *or* a 15-line exponential backoff in `internal/platform/backoff` | Prefer the in-house version until a second consumer appears. |
| Circuit breaker | **in-house minimal** (count window, 3 states) in `internal/platform/breaker` | `sony/gobreaker` is fine, but the state machine we need is small and we avoid an external dependency's semantics mismatch. Revisit if requirements grow. |

**Principle applied:** every external package must answer "what does this
do that the std lib cannot, and is that delta worth a dependency?" The
three dependencies that clearly earn their place: `pgx`, `nats.go`,
`whatsmeow`. Everything else is on probation.

## Communication

- **Public API: REST/JSON over HTTPS.** The consumer is a backend
  developer integrating against a CRM/ERP; REST is the lowest-friction
  contract. gRPC would impose protobuf tooling on the caller for a
  single endpoint (`POST /messages`) — unjustified.
- **Internal dispatch: NATS JetStream.** This is *not* "messaging for
  messaging's sake." JetStream earns its place because:
  1. The ingest path must return in <50ms while the actual send takes
     1–3s (staggered) — async decoupling is a hard requirement, not a
     style choice.
  2. Work-queue semantics give at-least-once durability with a single
     consumer per message — exactly the dispatch model the PRD
     specifies.
  3. Horizontal worker scaling is a NATS queue-group primitive, not
     something we build.
  A local in-process channel would lose work on crash; JetStream is the
  durability boundary.
- **Webhooks: outbound HTTPS POST**, driven by a dedicated JetStream
  consumer (so webhook delivery itself is durable and retried, not
  fire-and-forget on the dispatch goroutine).
- **Admin: server-rendered HTML over HTTPS** (Echo + Templ + HTMX). No
  separate API for the console; HTMX fragments *are* the API.

## Persistence

- **PostgreSQL** is the only datastore. No Redis for MVP — the load
  envelope (500 req/s) does not require a cache layer, and API-key
  auth can be served from an in-memory `map` refreshed on a TTL or
  via LISTEN/NOTIFY. **Add Redis only if a measurement shows auth or
  session lookup on the hot path.**
- **Driver: `pgx/v5`** with a single shared `*pgxpool.Pool`.
- **Schema highlights:**
  - `workspaces(id, name, created_at)`
  - `api_keys(id, workspace_id, key_hash, label, created_at, revoked_at)`
    — `key_hash` is SHA-256; the prefix (e.g. `pergo_<8 chars>`) is
    stored cleartext for lookup, the secret is hashed.
  - `devices(jid, workspace_id, platform, encrypted_session, created_at)`
    — whatsmeow `store.Device` row, `encrypted_session` is AES-256-GCM.
  - `audit_logs(trace_id, workspace_id, event, channel, status, payload,
    created_at)` — **partitioned by `workspace_id`** (or by
    `created_at` day if workspace cardinality is low) to keep partition
    pruning effective and inserts append-only.
  - `queue_depth` is **not** stored — it is read from JetStream stream
    info (`nats.StreamInfo.State.Msgs`) at ingest time for backpressure.
- **No ORM.** Queries are hand-written SQL in `internal/platform/postgres`
  with `pgx`'s `CollectRows`/`ForEachRow` helpers. Query builder
  (`squirrel`) is *not* introduced; the query count is small and known.
- **Audit writes go through a batch pipeline**, not per-request
  `INSERT`. See `06-core-code-example.md`.
- **Transactions** are short and targeted: the audit batch writer opens
  one `BEGIN` per batch; the device-store path uses whatsmeow's
  `sqlstore.Container`, which manages its own tx.

## What we are *not* doing

- No Redis / memcached (unmeasured need).
- No Kafka (JetStream covers durability + queue groups with far less
  operational weight at this scale).
- No gRPC internal mesh (one binary, one process boundary — REST +
  JetStream suffice).
- No ORM, no query builder, no DI framework, no config daemon.
- No distributed tracing SDK (OpenTelemetry) in MVP — Trace-ID is
  propagated explicitly via `context` + NATS headers and emitted in
  `slog`. Add OTel only if a tracing backend is introduced.
