# Stack Research

**Domain:** Self-hosted omnichannel CPaaS gateway (Go) — unified REST ingestion, NATS JetStream durability, multi-channel dispatch (WhatsApp Web / WABA / Telegram), server-rendered admin console
**Researched:** 2026-06-25
**Confidence:** HIGH

> Versions verified against the **Go module proxy (`proxy.golang.org/@latest`)** — the canonical source of truth for Go module versions — and cross-referenced against official GitHub repository source/READMEs and official docs. Feature/behaviour claims verified against primary source code (e.g. `whatsmeow/store/sqlstore/container.go`, Echo `API_CHANGES_V5.md`). Publication dates checked. No training-data version assertions.

## PRD Stack Validation — Executive Summary

The PRD/architecture docs prescribe: Echo, a-h/templ + HTMX, NATS JetStream, PostgreSQL via pgx/v5, whatsmeow, golang.org/x/time/rate, log/slog. **All seven core choices are validated as correct for 2026** — none are deprecated or replaced by a better option. Three require version / import-path corrections, and the surrounding stack (migrations, UUID, config, testing, release tooling) was under-specified and is now pinned:

| PRD choice | Verdict | Correction / action |
|------------|---------|---------------------|
| Echo (HTTP) | ✅ Keep | **Target Echo v5, not v4.** v5 is the current major line (since 2026-01-18); v4 enters security-only EOL on **2026-12-31**. v5 is a major breaking release (handler signature, logger, router). Slog integration in v5 *strengthens* the PRD's `log/slog` choice. |
| a-h/templ | ✅ Keep | v0.3.1020 — still pre-1.0 by intent, but mature/stable. Import path is `github.com/a-h/templ` (no `/v1`). Requires Go 1.25. |
| HTMX | ✅ Keep | Pin **htmx 2.x** (stable, `htmx.org@2.0.10`). htmx v4 is in **beta** (Summer '26 target) — do not ship beta to an operator console. |
| NATS JetStream (`nats-io/nats.go`) | ✅ Keep | v1.52.0. `WorkQueuePolicy` + `MaxMsgs` + `DiscardNew` maps *exactly* to the PRD backpressure model. Requires Go 1.25. |
| PostgreSQL via `pgx/v5` | ✅ Keep | v5.10.0. **Add `pgx/v5/stdlib` bridge** — whatsmeow and goose both speak `database/sql`; bridge them onto the same pgx driver rather than adding `lib/pq`. |
| whatsmeow | ✅ Keep (with caveats) | Canonical import is **`go.mau.fi/whatsmeow`** (not `github.com/tulir/whatsmeow`). **No semver tags** — pin to a dated pseudo-version. PostgreSQL store support **confirmed** in source. whatsmeow writes device keys **plaintext** — see Gaps. |
| `golang.org/x/time/rate` | ✅ Keep | v0.15.0. `Limiter.Wait(ctx)` is exactly the per-session staggered-dispatch primitive. |
| `log/slog` | ✅ Keep | stdlib since Go 1.21; Echo v5 now uses `*slog.Logger` natively — alignment is now bidirectional. |

**New floor:** Go **1.25** (nats.go v1.52, Echo v5, whatsmeow, templ all require it). Target toolchain **Go 1.26.4** (current stable).

## Recommended Stack

### Core Technologies

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| **Go** | 1.26.x (toolchain), 1.25 floor | Language + runtime | All four heavy deps (nats.go, echo v5, whatsmeow, templ) require `go 1.25.0` in their `go.mod`; 1.26.4 is current stable. `log/slog` (1.21+), `net/http.ServeMux` method routing (1.22+), `math/rand/v2` (1.22+) all available. |
| **Echo** | **v5.2.1** (`github.com/labstack/echo/v5`) | HTTP router + middleware (public API + admin) | v5 is the current major line since 2026-01-18; v4 EOL 2026-12-31. Built on `net/http`, radix-tree router, deep middleware, `echotest` helpers. **Native `*slog.Logger`** — zero-friction alignment with the PRD's logging choice. `echo-prometheus` middleware available if metrics infra lands. |
| **NATS + JetStream** | `nats.go` **v1.52.0** + NATS Server 2.10+ | Durable work-queue broker, durability boundary for outbound work | `WorkQueuePolicy` = each message consumed once + deleted on ack (exactly the PRD dispatch model). `MaxMsgs` + `DiscardNew` = reject-on-full → HTTP 429 backpressure. Headers carry Trace-ID across the HTTP→broker→worker boundary. Pull consumers with `MaxDeliver` give retry. Far less operational weight than Kafka at 500 req/s. |
| **PostgreSQL** | 16+ (15 acceptable) | System of record: workspaces, api_keys, devices, audit_logs | Sole datastore (no Redis — load envelope doesn't justify it). Partition `audit_logs` by `workspace_id` or day for append-only inserts. |
| **pgx/v5** | **v5.10.0** (`github.com/jackc/pgx/v5`) | PostgreSQL driver (native path) | Binary protocol, prepared-statement cache, native `COPY`, `Batch`/`QueryExecMode` pipeline — the right tool for the audit batch writer. `CollectRows`/`ForEachRow` helpers replace an ORM. Use a single shared `*pgxpool.Pool` for PerGo's own queries. |
| **pgx/v5/stdlib** | (subpackage of pgx/v5) | `database/sql` bridge for whatsmeow + goose | whatsmeow's `sqlstore` and goose both consume `*sql.DB`. Register pgx as the `database/sql` driver (`stdlib.GetDefaultDriver` / `sql.Open("pgx", …)`) so **one** PG driver serves all three access paths. Avoids adding `lib/pq`. |
| **whatsmeow** | `go.mau.fi/whatsmeow` @ dated pseudo-version (e.g. `v0.0.0-20260622185415-5f04eac6dbbb`) | WhatsApp Web multi-device adapter | The only viable Go library for WhatsApp Web multi-device. Actively maintained (1600 commits, latest 2026-06-22). `sqlstore.Container` **explicitly supports Postgres** (confirmed in `container.go`). Requires Go 1.25 / toolchain 1.26.4. |
| **a-h/templ** | **v0.3.1020** (`github.com/a-h/templ`) | Compile-time type-safe HTML→Go for admin UI | 10.4k stars, LSP + `templ generate` codegen + `fmt` + watch mode. Zero runtime parse cost; pairs with HTMX fragments. Still v0.x by maintainer intent but API-stable in practice. Requires Go 1.25. |
| **HTMX** | **2.x** (CDN `htmx.org@2.0.10`) | Server-driven fragment interactivity for the operator console | ~16k min.gzipped, dependency-free, no JS bundle. `HX-Request` header detection returns fragments vs full pages — exactly the PRD's admin pattern. 2.x dropped IE (irrelevant for an operator console). |
| **log/slog** | stdlib (Go 1.21+) | Structured, leveled, context-aware logging | No external dependency. Echo v5 exposes `*slog.Logger` directly on `*Context` and `Echo` — context propagation is native. |
| **golang.org/x/time/rate** | **v0.15.0** | Per-session token-bucket rate limiting / staggered dispatch | `rate.NewLimiter(rate.Every(2*time.Second), 1)` + `Limiter.Wait(ctx)` blocks the worker goroutine while yielding to the scheduler — precisely the 1–3s staggered-dispatch requirement, with thousands of concurrent limiters feasible. |

### Supporting Libraries

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| **pressly/goose/v3** | **v3.27.1** | DB schema migrations (embedded SQL + Go funcs) | Default migration tool. `go:embed` migrations + `goose.SetBaseFS` + `SetDialect("postgres")`. Uses `database/sql` — bridge via `pgx/v5/stdlib`. Env-var substitution, out-of-order migrations, `fix` for CI. Over golang-migrate (slower release cadence). |
| **google/uuid** | **v1.6.0** | Trace-ID generation | **Choose this over gofrs/uuid.** whatsmeow already depends on `google/uuid v1.6.0` — adopting it avoids a second UUID library in the module graph. Stable (no release since 2024-01 because it's tiny and done). |
| **caarlos0/env/v11** | **v11.4.1** | 12-factor env-var config into structs | Struct-tag parsing, no YAML daemon, no viper. v11 is the current line (legacy v3 is dead since 2018). Only if hand-rolled `os.Getenv` becomes tedious; otherwise plain `os.Getenv` + a small loader is acceptable per the architecture's "no config daemon" stance. |
| **prometheus/client_golang** | **v1.23.2** (optional) | Prometheus metrics exporter | Take **only if** a scraping infra exists (matches the architecture doc's "only if a scraping infra exists"). `echo-prometheus` middleware wires it to Echo. Otherwise `expvar` + `net/http/pprof` suffice for MVP. |
| **testcontainers/testcontainers-go** | **v0.43.0** | Integration tests with real PostgreSQL + NATS | Spin real containers in `TestMain`; no shared dev DB drift. The right way to test the pgx batch writer, JetStream consumer ack/retry, and whatsmeow sqlstore round-trips. |
| **stretchr/testify** | **v1.11.1** (optional) | Test assertions / suite helpers | Use sparingly for ergonomics (`assert`/`require`); the project's "minimal deps" ethos argues for table-driven std `testing` first, testify second. No v2 exists; v1 is current. |
| **coder/websocket** | (transitive via whatsmeow) | whatsmeow's WebSocket transport | Do not import directly; whatsmeow owns its WS lifecycle. Listed only so the dependency is understood. |
| **go.mau.fi/util** | (transitive via whatsmeow) | whatsmeow's `dbutil.Database` wrapper | Transitive — drives whatsmeow's `database/sql` usage. Not a direct dependency. |

### Development Tools

| Tool | Purpose | Notes |
|------|---------|-------|
| **golangci-lint** | Static analysis aggregator | Run in CI and pre-commit. Enable `govet`, `staticcheck`, `errcheck`, `ineffassign`, `unused`, `gosec` (security — relevant for a credential-handling gateway). |
| **go test + `-race -count=1`** | Concurrency-safe test runs | `-race` is mandatory for the goroutine-heavy worker/dispatcher code. |
| **net/http/pprof** | Runtime CPU/heap/goroutine profiling | stdlib; expose on a separate listener (not the public port). PRD-mandated. |
| **expvar** | Live debug counters (queue depth, in-flight) | stdlib; pair with JetStream `StreamInfo.State.Msgs` for backpressure telemetry. |
| **Docker (multi-stage)** | Reproducible build + slim runtime image | Final image: `gcr.io/distroless/static` (static binary) or `scratch`. whatsmeow needs CGO-free builds — both `modernc.org/sqlite` (pure Go) and pgx are CGO-free, so a static binary is achievable. |
| **docker-compose** | Local dev stack (Postgres + NATS + app) | One `docker-compose.yml` for the three-process local environment; mirrors the integration-test topology. |
| **GitHub Actions** | CI: `go test ./... -race`, golangci-lint, build | Run goose `validate` + `fix` in CI to keep migrations sequential for production. |
| **goreleaser/v2** | Cross-platform binaries + Docker images + checksums | v2.16.0 is current. Replaces hand-rolled release scripts; produces the self-hosted distribution artifacts. |
| **templ LSP / `templ generate`** | Authoring + codegen for admin UI | `go install github.com/a-h/templ/cmd/templ@latest`; run `templ generate` in a `//go:generate` step and in CI's pre-build. |

## Installation

```bash
# --- Core (go.mod) ---
go get github.com/labstack/echo/v5@v5.2.1
go get github.com/nats-io/nats.go@v1.52.0
go get github.com/jackc/pgx/v5@v5.10.0
# whatsmeow: NO semver tags — pin to a dated pseudo-version. Resolve latest with:
#   GOPROXY=https://proxy.golang.org go list -m -versions go.mau.fi/whatsmeow
# then pin, e.g.:
go get go.mau.fi/whatsmeow@v0.0.0-20260622185415-5f04eac6dbbb
go get golang.org/x/time/rate@v0.15.0
go get github.com/a-h/templ@v0.3.1020

# --- Supporting ---
go get github.com/pressly/goose/v3@v3.27.1
go get github.com/google/uuid@v1.6.0
go get github.com/caarlos0/env/v11@v11.4.1          # only if not hand-rolling os.Getenv
go get github.com/prometheus/client_golang@v1.23.2  # ONLY if a scraper exists

# --- Dev / test ---
go get github.com/testcontainers/testcontainers-go@v0.43.0
go get github.com/stretchr/testify@v1.11.1           # optional

# --- Tooling (not in go.mod; install to $GOPATH/bin) ---
go install github.com/pressly/goose/v3/cmd/goose@latest
go install github.com/a-h/templ/cmd/templ@latest
go install github.com/goreleaser/goreleaser/v2@latest
# golangci-lint: install per its official installer (not go install) to pin version
```

> **go.mod header should read `go 1.25`** (or `go 1.26`). Set `toolchain go1.26.4` to match the local toolchain. All four heavy deps require `go 1.25.0` minimum — a lower floor will fail to build.

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|-------------------------|
| **Echo v5** | `net/http` (Go 1.22+ ServeMux) + `chi` | Only if the team wants zero router dependency and is willing to hand-roll middleware. The architecture doc already considered this and chose Echo for middleware ergonomics + admin-stack parity. The std ServeMux method routing (1.22+) makes `net/http` viable but loses Echo's binder/middleware/`echotest`. |
| **Echo v5** | **Echo v4** | **Do not start a new project on v4.** v4 EOL is 2026-12-31 (security/bug only). Only justify v4 if an existing v4 codebase must be extended — not the case here (greenfield). |
| **NATS JetStream** | Kafka / Redpanda | If the deployment already runs Kafka, or if throughput climbs an order of magnitude above 500 req/s with multi-tenant partitioning needs. At 500 req/s with work-queue semantics, JetStream is materially less operational weight. (PRD explicitly excludes Kafka.) |
| **NATS JetStream** | In-process channels | Never for outbound work — channels lose work on crash; JetStream is the durability boundary. (Architecture doc's correct call.) |
| **pgx/v5** | `database/sql` + `sqlx` | If every query were trivial and the audit batch writer didn't need pipeline/COPY. pgx's binary protocol + batch pipeline earn the dependency for this workload. |
| **goose/v3** | `golang-migrate/migrate/v4` | If the team prefers migrate's CLI ergonomics or needs its broader source/destination driver list. goose has faster release cadence (v3.27.1 Apr 2026 vs migrate v4.19.1 Nov 2025) and first-class `go:embed` + Go-function migrations. |
| **google/uuid** | `gofrs/uuid/v5` | If a strict RFC-9562 v7 (time-ordered) UUID is required for Trace-ID ordering. whatsmeow's transitive dependency on google/uuid makes google/uuid the dedup-friendly default; gofrs/uuid adds a second UUID lib to the graph. |
| **PostgreSQL only (no Redis)** | Redis / memcached | Only if measurement shows API-key auth or session lookup on the hot path at >500 req/s. PRD correctly defers this. |
| **HTMX 2.x** | A SPA framework (React/Svelte) | Never for this product — the console is an operator tool, not a consumer app. HTMX fragments are the API. |

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| **Echo v4** for greenfield | EOL 2026-12-31 (security/bug only); v5 is the current major line with native slog + generics + `echotest`. | **Echo v5** (`github.com/labstack/echo/v5`). |
| **`github.com/tulir/whatsmeow`** import path | Both paths resolve to the same repo, but the **canonical module path is `go.mau.fi/whatsmeow`** (per its pkg.go.dev badge and go.mod). Importing the github path pulls the same pseudo-version via redirect but is non-idiomatic. | **`go.mau.fi/whatsmeow`**. |
| **`lib/pq`** (any version) | In maintenance mode; superseded by pgx. whatsmeow + goose both use `database/sql` — adding lib/pq means a *second* PG driver alongside pgx. | **`pgx/v5/stdlib`** as the single `database/sql` driver for whatsmeow + goose. |
| **Kafka / Redpanda** | Unjustified operational weight at 500 req/s with simple work-queue semantics; JetStream covers durability + queue groups. | **NATS JetStream** with `WorkQueuePolicy`. |
| **Redis / memcached** (in MVP) | Unmeasured need; API-key auth served from in-memory map with TTL refresh. | In-memory map + TTL; revisit only on measured hot-path pressure. |
| **ORM / query builder** (`gorm`, `ent`, `squirrel`) | Query count is small and known; the audit batch writer needs pgx's batch pipeline, not an ORM's allocation overhead. | Hand-written SQL + `pgx` `CollectRows`/`ForEachRow`. |
| **viper / YAML config daemon** | 12-factor env vars suffice; a config daemon is operational weight with no payoff for a single binary. | `caarlos0/env/v11` or plain `os.Getenv`. |
| **htmx v4 (beta)** | In beta, target Summer '26 — not production-ready for an operator console. | **htmx 2.x** stable. |
| **htmx 1.x** | Maintained only for IE support; 2.x is the active line. | **htmx 2.x** (IE support is irrelevant for operators). |
| **zerolog / zap** | `log/slog` is stdlib, context-aware, and now native to Echo v5. No benchmark justifies an external logger here. | **`log/slog`**. |
| **OpenTelemetry SDK in MVP** | PRD defers it; Trace-ID propagates via `context` + NATS headers + slog. Adding OTel without a tracing backend is dead weight. | Explicit Trace-ID propagation; add OTel only when a backend (Tempo/Jaeger) is introduced. |
| **`sony/gobreaker`** (for now) | The state machine needed is small; an external breaker risks semantics mismatch. | In-house minimal breaker in `internal/platform/breaker`; revisit if requirements grow. (Architecture doc's call — endorsed.) |
| **gofrs/uuid** (when google/uuid is already transitively present) | Adds a second UUID library to the module graph for no functional gain at this scale. | **google/uuid v1.6.0** (already a whatsmeow transitive dep). |

## Stack Patterns by Variant

**If a Prometheus/Grafana scraping infra exists in the deployment:**
- Add `prometheus/client_golang` v1.23.2 + the `echo-prometheus` middleware.
- Expose `/metrics` on the public or a sidecar listener.
- Because the architecture already mandates `pprof` + `expvar`, this is additive, not a replacement.

**If no scraping infra exists (default MVP):**
- Stay on `net/http/pprof` + `expvar` only.
- Do not pull `prometheus/client_golang` — it would be a dependency with no consumer.

**If whatsmeow device-key encryption-at-rest is a hard compliance requirement:**
- whatsmeow's `sqlstore` writes device keys (noise/identity/prekey) **plaintext** into `whatsmeow_device`. The PRD's "AES-256-GCM for session tokens and channel credentials" does **not** cover whatsmeow's internal keys without extra work.
- Options: (a) accept DB-level / filesystem-level / full-disk encryption as the boundary; (b) implement a custom whatsmeow `store.DeviceContainer` that encrypts key columns (significant effort, must track whatsmeow upgrades); (c) use PostgreSQL `pgcrypto` on those columns. **Decide explicitly in the relevant phase — do not assume whatsmeow encrypts.**

**If the deployment grows beyond a single binary / single node:**
- NATS queue groups already give horizontal worker scaling for free (no code change).
- PostgreSQL is the scaling pinch point before NATS is — read replicas / connection pooling (PgBouncer) come before JetStream clustering.

**If WhatsApp Cloud (WABA) and Telegram become the dominant traffic (vs. WhatsApp Web):**
- The whatsmeow dependency and its Go-1.25/toolchain-1.26 floor remain, but the staggered-dispatch rate limiter is only active for the unofficial channel — WABA/Telegram use provider rate limits, not the 1–3s human-simulation delay.

## Version Compatibility

| Package A | Compatible With | Notes |
|-----------|-----------------|-------|
| `github.com/labstack/echo/v5@v5.2.1` | Go **1.25.0+** | `go.mod` declares `go 1.25.0`. Handler signature changed to `*echo.Context`; `HTTPErrorHandler` is `func(c *Context, err error)`; `Logger` is `*slog.Logger`. See `API_CHANGES_V5.md`. |
| `github.com/nats-io/nats.go@v1.52.0` | Go **1.25.0+**, NATS Server **2.10+** (2.14+ for batch publish) | `go.mod` declares `go 1.25.0`. JetStream work-queue + headers stable since server 2.2. |
| `github.com/jackc/pgx/v5@v5.10.0` | Go 1.21+ (practical: 1.25 to match the rest) | `stdlib` subpackage provides the `database/sql` driver name `"pgx"`. |
| `go.mau.fi/whatsmeow@<pseudo-version>` | Go **1.25.0**, toolchain **go1.26.4** | `go.mod` declares `go 1.25.0` + `toolchain go1.26.4`. Transitive deps: `google/uuid v1.6.0`, `rs/zerolog v1.35.1`, `coder/websocket v1.8.15`, `go.mau.fi/util v0.9.10`. **No semver tags — every `go get` resolves a fresh pseudo-version; pin in `go.mod` and upgrade deliberately.** |
| `github.com/a-h/templ@v0.3.1020` | Go **1.25.0+** | `go.mod` declares `go 1.25.0`. Codegen via `templ generate` must run before `go build`. |
| `github.com/pressly/goose/v3@v3.27.1` | Go 1.23+ | Uses `database/sql`; bridge to pgx via `pgx/v5/stdlib`. |
| `golang.org/x/time/rate@v0.15.0` | Go 1.23+ | Pure stdlib-extension; no native deps. |
| `github.com/testcontainers/testcontainers-go@v0.43.0` | Go 1.23+, Docker runtime available at test time | Requires Docker daemon in CI. |
| `github.com/goreleaser/goreleaser/v2@v2.16.0` | Go 1.23+ | Release tool (not a runtime dep). |

**Cross-cutting compatibility note:** whatsmeow's `toolchain go1.26.4` directive will, under Go's toolchain management, fetch that toolchain if the local Go is older. Standardise the team/CI toolchain on **Go 1.26.x** to avoid surprise toolchain downloads and to satisfy every dep's floor in one shot.

## Sources

- **Go module proxy (`proxy.golang.org/<module>/@latest`)** — authoritative version + release-date verification for: echo/v4 (v4.15.4), echo/v5 (v5.2.1), a-h/templ (v0.3.1020), nats.go (v1.52.0), pgx/v5 (v5.10.0), go.mau.fi/whatsmeow (pseudo-version 2026-06-22), golang.org/x/time (v0.15.0), goose/v3 (v3.27.1), golang-migrate/v4 (v4.19.1), gofrs/uuid/v5 (v5.4.0), google/uuid (v1.6.0), caarlos0/env/v11 (v11.4.1), prometheus/client_golang (v1.23.2), cenkalti/backoff/v4 (v4.3.0), sony/gobreaker (v1.0.0), testify (v1.11.1), testcontainers-go (v0.43.0), goreleaser/v2 (v2.16.0). **Confidence: HIGH** (canonical source of truth for Go module versions).
- **`go.mau.fi/whatsmeow` go.mod** (via proxy) — confirmed Go 1.25 / toolchain 1.26.4 floor + transitive deps (google/uuid, rs/zerolog, coder/websocket, go.mau.fi/util). **Confidence: HIGH.**
- **`whatsmeow/store/sqlstore/container.go`** (raw GitHub source) — confirmed `Container` uses `database/sql`; doc comment "Only SQLite and Postgres are currently fully supported"; `sql.Open(dialect, address)`; device keys stored as raw byte columns (plaintext). **Confidence: HIGH.**
- **Echo GitHub README + `API_CHANGES_V5.md`** (raw) — confirmed v5 is current major line since 2026-01-18; v4 EOL 2026-12-31; full breaking-change inventory (Context→struct, `*slog.Logger`, Router interface, handler signature, generics). **Confidence: HIGH.**
- **a-h/templ GitHub README** — v0.3.1020, LSP/codegen/fmt/watch, 10.4k stars, pre-1.0-by-intent. **Confidence: HIGH.**
- **NATS JetStream Streams docs** (`docs.nats.io/nats-concepts/jetstream/streams`) — `WorkQueuePolicy` (one consumer per subject, delete on ack), `MaxMsgs`+`DiscardNew`, headers, `MaxDeliver` retry. **Confidence: HIGH.**
- **pressly/goose GitHub README** — v3.27.1, embedded migrations via `go:embed` + `SetBaseFS`, PostgreSQL dialect, `database/sql` consumer. **Confidence: HIGH.**
- **htmx.org** — stable 2.x (`htmx.org@2.0.10`), v4 in beta (Summer '26 target), 2.x dropped IE. **Confidence: HIGH.**
- **go.dev/dl** — Go 1.26.4 current stable. **Confidence: HIGH.**

---
*Stack research for: self-hosted omnichannel CPaaS gateway in Go*
*Researched: 2026-06-25*
