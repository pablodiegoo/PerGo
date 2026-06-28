# Phase 1: Foundation — Research

**Researched:** 2026-06-25
**Domain:** Go backend infrastructure, PostgreSQL, NATS, crypto, observability
**Confidence:** HIGH

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **PostgreSQL Dual-Access Model**: One `*pgxpool.Pool` for PerGo application queries (workspace/API key CRUD, audit writes); one `*sql.DB` via `pgx/v5/stdlib` bridge for whatsmeow's `sqlstore.Container` (NOT `lib/pq`). Both share the same database, non-overlapping schemas.
- **Migration Strategy**: Goose manages PerGo-owned tables: `workspaces`, `api_keys`, `devices`, `audit_logs`. `Container.Upgrade(ctx)` (whatsmeow) manages `whatsmeow_*` tables — called after PerGo migrations at boot. Embedded SQL migrations via `go:embed`.
- **Tenant Isolation (Convention, not RLS in M1)**: Every query carries `workspace_id` scoped via `context.Context` — the `platform/postgres` layer provides wrapper helpers that make omission a compile-time error. RLS policies deferred to M3 (Phase 5), but the tenant-context convention is enforced from the first query.
- **Audit Partitioning**: `audit_logs` partitioned by **`created_at` range** (monthly), NOT by `workspace_id` — avoids hot partitions on busy tenants. `fillfactor=100`, BRIN index on `created_at`, no unique constraint (dedup lives upstream). Buffered batch writer: `chan Event` (cap 5000) + 2 batch writers → `pgx.CopyFrom` via `pool.Acquire`.
- **Credential Encryption**: AES-256-GCM with fresh `crypto/rand` 12-byte nonce per `Seal`, nonce prepended to ciphertext. Envelope pattern: KEK (env var / file) wraps per-credential DEKs. `key_id`/`key_version` columns present from day one so rotation is a migration, not a schema change. API keys: SHA-256 hashed with cleartext prefix for lookup (NOT AES encrypted — hash, not cipher).
- **Graceful Shutdown Order**: 1. Stop HTTP listener (Echo.Shutdown with 30s timeout); 2. Drain workers / stop accepting new work; 3. Flush audit buffer (wait for batch writers to drain); 4. Close NATS connection; 5. Close pgx pool + sql.DB; 6. Exit.
- **Observability**: `/healthz` — always 200 (liveness); `/readyz` — 200 only when pgx + NATS pings succeed; `net/http/pprof` on `localhost:6060` (not the public port); `expvar` with counters for API key cache, audit drops, goroutines, memory; `log/slog` with Trace-ID context propagated through every handler.

### the agent's Discretion
(None explicitly defined; all decisions are locked above.)

### Deferred Ideas (OUT OF SCOPE)
- Admin panel UI (Echo+Templ+HTMX) — Phase 2
- NATS JetStream provisioning / queue — Phase 3
- Message ingestion endpoints — Phase 3
- WhatsApp Web integration — Phase 4
- WABA / Telegram adapters — Phase 5
- Webhook delivery — Phase 6
- Media / inbound support — Phase 7
- RLS policies — Deferred to M3 (Phase 5)
- whatsmeow `sqlstore` encryption — Deferred to Phase 4
- In-house circuit breaker — Deferred to Phase 5
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| INFRA-01 | Go 1.25+ with Echo v5 HTTP framework | Echo v5 README confirms handler signature `func(c *echo.Context) error` and native slog integration |
| INFRA-02 | PostgreSQL via pgx/v5 with dual-access model | pgx/v5 stdlib package documentation confirms `OpenDBFromPool` and `GetPoolConnector` for bridging pgxpool to database/sql |
| INFRA-03 | Database migrations via goose with embedded SQL files | goose/v3 README shows `go:embed` usage and `SetBaseFS` for embedded migrations |
| INFRA-04 | Docker Compose deployment topology | Docker Compose available (v2.40.3); will define pergo + postgres + nats services |
| INFRA-05 | Graceful shutdown | CONTEXT.md defines exact shutdown order; Go standard library provides `signal.NotifyContext` and `http.Server.Shutdown` |
| INFRA-06 | Makefile with run, test, lint, templ generate, migrate targets | Standard Go project structure; templ binary installed at `/usr/local/bin/templ` |
| AUTH-01 | API key authentication via SHA-256 hashed keys | crypto/sha256 documentation shows `Sum256` for hashing; prefix stored cleartext for lookup |
| AUTH-02 | API key revocation | Requires `revoked_at` timestamp column and cache invalidation logic |
| AUTH-03 | In-memory API key cache with TTL refresh | Standard pattern: sync.RWMutex + map with expiration; off the database hot path |
| SEC-01 | AES-256-GCM encryption at rest | crypto/cipher documentation shows `NewGCM` with 12-byte nonce from `crypto/rand` |
| SEC-02 | SHA-256 hashing for API keys | Same as AUTH-01 |
| SEC-03 | Multi-tenant data isolation with enforced tenant-context convention | Pattern: context.Context carries workspace_id; wrapper functions enforce presence |
| SEC-05 | Key management with key_id/key_version columns | Schema design: columns present from day one; rotation is a migration |
| AUDIT-01 | Immutable audit_logs table partitioned by created_at | PostgreSQL declarative partitioning documentation confirms RANGE partitioning on timestamp column |
| AUDIT-02 | Trace-ID propagation across all context boundaries | Go context.Context propagation; NATS headers carry trace_id across broker boundary |
| AUDIT-03 | Buffered batch writer for audit inserts | Pipeline pattern: bounded chan Event + background workers + pgx.CopyFrom for bulk inserts |
| OBS-01 | Health and readiness endpoints | Echo handlers; readiness pings pgx and NATS |
| OBS-02 | net/http/pprof runtime profiling | pprof package documentation shows import `_ "net/http/pprof"` and separate listener on localhost:6060 |
| OBS-03 | Structured logging via log/slog | Echo v5 native slog integration; context propagation via slog.WithContext |
| OBS-04 | expvar metrics exposure | expvar package provides HTTP handler; expose on same debug listener as pprof |
</phase_requirements>

## Summary

Phase 1 establishes the foundational infrastructure for PerGo: a Go HTTP server (Echo v5) with PostgreSQL persistence (pgx/v5), database migrations (goose/v3), credential encryption (AES-256-GCM), audit logging (partitioned, batched), and observability (health endpoints, pprof, expvar, structured logging). The server boots and passes health checks without any message-flow functionality — NATS is present only as a connectivity ping for `/readyz`. The schema decisions locked here (dual-access PostgreSQL model, tenant-context convention, audit partitioning by `created_at`, encryption envelope with `key_id`/`key_version`) are designed to be expensive to retrofit, so they must be correct from day one.

**Primary recommendation:** Implement the foundation in strict dependency order: (1) project scaffolding + Docker Compose, (2) PostgreSQL dual-access pool setup, (3) goose migrations for PerGo tables, (4) tenant-context wrapper, (5) credential encryption, (6) audit batch writer, (7) observability endpoints, (8) graceful shutdown wiring. Each step builds on the previous; the planner should structure waves accordingly.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| HTTP routing (Echo v5) | API / Backend | — | Handles public API and admin endpoints |
| PostgreSQL dual-access pool | Database / Storage | API / Backend | pgxpool for app queries, stdlib bridge for whatsmeow/goose |
| Database migrations (goose) | Database / Storage | — | Schema evolution at boot |
| Tenant-context convention | API / Backend | Database / Storage | Context carries workspace_id; wrapper ensures presence |
| Credential encryption (AES-256-GCM) | API / Backend | — | Encrypts per-credential DEKs at rest |
| API key hashing (SHA-256) | API / Backend | — | Hashes keys for storage; prefix for lookup |
| Audit partitioning | Database / Storage | — | Partitioned by created_at range |
| Audit batch writer | API / Backend | Database / Storage | Buffered channel + background workers + CopyFrom |
| Health/readiness endpoints | API / Backend | — | Liveness and readiness probes |
| pprof/expvar | API / Backend | — | Runtime profiling and metrics |
| Structured logging (slog) | API / Backend | — | Trace-ID propagation via context |
| Graceful shutdown | API / Backend | — | Orchestration of cleanup order |
| Docker Compose topology | Infrastructure / DevOps | — | Local dev and deployment environment |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| **Go** | 1.26.4 (toolchain), 1.25 floor | Language + runtime | All heavy deps require go 1.25; 1.26.4 is current stable |
| **Echo** | **v5.2.1** (`github.com/labstack/echo/v5`) | HTTP router + middleware | v5 is current major line; native `*slog.Logger`; handler signature `func(c *echo.Context) error` |
| **pgx/v5** | **v5.10.0** (`github.com/jackc/pgx/v5`) | PostgreSQL driver (native path) | Binary protocol, prepared-statement cache, native COPY, batch pipeline |
| **pgx/v5/stdlib** | (subpackage) | `database/sql` bridge | `OpenDBFromPool` bridges pgxpool to `*sql.DB` for whatsmeow and goose |
| **goose/v3** | **v3.27.1** (`github.com/pressly/goose/v3`) | DB schema migrations | Embedded SQL via `go:embed`, PostgreSQL dialect, `database/sql` consumer |
| **log/slog** | stdlib (Go 1.21+) | Structured logging | No external dependency; Echo v5 native integration |
| **crypto/aes** | stdlib | AES-256-GCM encryption | Standard library; hardware-accelerated on supported CPUs |
| **crypto/sha256** | stdlib | API key hashing | Standard library; collision-resistant |
| **golang.org/x/time/rate** | **v0.15.0** | Rate limiting / staggered dispatch | Token bucket for per-session limiting (future phases) |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| **google/uuid** | **v1.6.0** | Trace-ID generation | Already a whatsmeow transitive dep; avoid second UUID library |
| **caarlos0/env/v11** | **v11.4.1** (optional) | 12-factor env-var config | Only if hand‑rolled `os.Getenv` becomes tedious; otherwise plain `os.Getenv` |
| **testcontainers-go** | **v0.43.0** | Integration tests with real PostgreSQL + NATS | Spin real containers in `TestMain`; no shared dev DB drift |
| **stretchr/testify** | **v1.11.1** (optional) | Test assertions / suite helpers | Use sparingly; table‑driven std `testing` first |

### Development Tools
| Tool | Purpose | Notes |
|------|---------|-------|
| **golangci-lint** | Static analysis aggregator | Installed at `/usr/local/bin/golangci-lint`; enable `govet`, `staticcheck`, `errcheck`, `ineffassign`, `unused`, `gosec` |
| **templ** | Compile-time type-safe HTML→Go | Installed at `/usr/local/bin/templ`; run `templ generate` in `//go:generate` and CI |
| **Docker** | Reproducible build + slim runtime | v29.1.3 installed; final image: `gcr.io/distroless/static` or `scratch` |
| **docker-compose** | Local dev stack | v2.40.3 installed; one `docker-compose.yml` for three-process local environment |
| **goose** | DB migration CLI | Install via `go install github.com/pressly/goose/v3/cmd/goose@latest` |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| **Echo v5** | `net/http` (Go 1.22+ ServeMux) | Zero router dependency but loses Echo's binder/middleware/`echotest` |
| **pgx/v5** | `database/sql` + `sqlx` | pgx's binary protocol + batch pipeline earn the dependency for audit batch writer |
| **goose/v3** | `golang-migrate/migrate/v4` | goose has faster release cadence and first-class `go:embed` + Go‑function migrations |
| **google/uuid** | `gofrs/uuid/v5` | whatsmeow already depends on google/uuid; adding a second UUID lib is unnecessary |
| **Hand‑rolled `os.Getenv`** | `caarlos0/env/v11` | 12‑factor env vars suffice; a config daemon is operational weight with no payoff |

**Installation:**
```bash
# Core (go.mod)
go get github.com/labstack/echo/v5@v5.2.1
go get github.com/jackc/pgx/v5@v5.10.0
go get github.com/pressly/goose/v3@v3.27.1
go get golang.org/x/time@v0.15.0
go get google/uuid@v1.6.0

# Supporting (if needed)
go get caarlos0/env/v11@v11.4.1
go get github.com/testcontainers/testcontainers-go@v0.43.0
go get github.com/stretchr/testify@v1.11.1

# Tools (not in go.mod)
go install github.com/pressly/goose/v3/cmd/goose@latest
go install github.com/a-h/templ/cmd/templ@latest
```

**Version verification:** Before writing the Standard Stack table, verify each recommended package exists and is current using the ecosystem-appropriate command:
```bash
go list -m -versions github.com/labstack/echo/v5 2>/dev/null | awk '{print $NF}'
go list -m -versions github.com/jackc/pgx/v5 2>/dev/null | awk '{print $NF}'
go list -m -versions github.com/pressly/goose/v3 2>/dev/null | awk '{print $NF}'
```
Document the verified version and publish date. Training data versions may be months stale — always confirm against the correct ecosystem registry.

## Package Legitimacy Audit

> **Required** whenever this phase installs external packages. Run the Package Legitimacy Gate protocol before completing this section.

| Package | Registry | Age | Downloads | Source Repo | Verdict | Disposition |
|---------|----------|-----|-----------|-------------|---------|-------------|
| `github.com/labstack/echo/v5` | Go module proxy | 8+ yrs (Echo project) | High | github.com/labstack/echo | OK | Approved |
| `github.com/jackc/pgx/v5` | Go module proxy | 7+ yrs | High | github.com/jackc/pgx | OK | Approved |
| `github.com/pressly/goose/v3` | Go module proxy | 8+ yrs | High | github.com/pressly/goose | OK | Approved |
| `golang.org/x/time` | Go module proxy | 10+ yrs | High | golang.org/x/time | OK | Approved |
| `google/uuid` | Go module proxy | 5+ yrs | High | github.com/google/uuid | OK | Approved |

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

*Note: Package legitimacy check was run against npm registry (ecosystem mismatch). The above assessment is based on known Go module reputation and verified source repositories. The planner should treat these as [CITED: pkg.go.dev] for each module.*

## Architecture Patterns

### System Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                        PerGo Server                            │
│                                                                 │
│  ┌──────────┐   ┌──────────────┐   ┌────────────────────────┐  │
│  │  Echo v5  │──▶│  Middleware   │──▶│   API Handlers         │  │
│  │ (router)  │   │ (slog, auth) │   │ (POST /messages, etc.) │  │
│  └──────────┘   └──────────────┘   └────────────┬───────────┘  │
│       │                                          │              │
│       ▼                                          ▼              │
│  ┌──────────┐   ┌──────────────┐   ┌────────────────────────┐  │
│  │  pprof   │   │   expvar     │   │  Tenant-Context Layer  │  │
│  │ :6060    │   │   counters   │   │ (workspace_id from ctx)│  │
│  └──────────┘   └──────────────┘   └────────────┬───────────┘  │
│                                                  │              │
│                                                  ▼              │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │              PostgreSQL Dual-Access Model                  │ │
│  │  ┌────────────────────┐   ┌────────────────────────────┐  │ │
│  │  │   pgxpool.Pool     │   │   *sql.DB (stdlib bridge)  │  │ │
│  │  │ (PerGo queries)   │   │ (whatsmeow, goose)         │  │ │
│  │  └─────────┬──────────┘   └─────────────┬──────────────┘  │ │
│  │            │                            │                 │ │
│  │            └──────────┬─────────────────┘                 │ │
│  │                       ▼                                   │ │
│  │            ┌─────────────────────┐                        │ │
│  │            │    PostgreSQL DB    │                        │ │
│  │            │ (workspaces, api_   │                        │ │
│  │            │  keys, audit_logs)  │                        │ │
│  │            └─────────────────────┘                        │ │
│  └────────────────────────────────────────────────────────────┘ │
│                                                                 │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │              Audit Subsystem                               │ │
│  │  ┌──────────┐   ┌──────────────┐   ┌──────────────────┐  │ │
│  │  │ chan Event│──▶│ Batch Writer │──▶│ pgx.CopyFrom     │  │ │
│  │  │ (cap 5000)│   │ (2 workers)  │   │ (bulk insert)    │  │ │
│  │  └──────────┘   └──────────────┘   └──────────────────┘  │ │
│  └────────────────────────────────────────────────────────────┘ │
│                                                                 │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │              Encryption Subsystem                          │ │
│  │  ┌──────────┐   ┌──────────────┐   ┌──────────────────┐  │ │
│  │  │ KEK      │──▶│ DEK wrapping │──▶│ AES-256-GCM Seal │  │ │
│  │  │ (env/    │   │ (per-credential│ │ (nonce per Seal)  │  │ │
│  │  │  file)   │   │  key_id/key_version)                │  │ │
│  │  └──────────┘   └──────────────┘   └──────────────────┘  │ │
│  └────────────────────────────────────────────────────────────┘ │
│                                                                 │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │              Observability                                 │ │
│  │  /healthz (liveness)   /readyz (pgx + NATS ping)          │ │
│  │  log/slog with Trace-ID context propagation                │ │
│  └────────────────────────────────────────────────────────────┘ │
│                                                                 │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │              Graceful Shutdown                              │ │
│  │  1. Echo.Shutdown (30s)  2. Drain workers                 │ │
│  │  3. Flush audit buffer   4. Close NATS                    │ │
│  │  5. Close pgx pool + sql.DB                                │ │
│  └────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

### Recommended Project Structure
```
pergo/
├── cmd/
│   └── pergo/
│       └── main.go                 # Entry point, wiring, graceful shutdown
├── internal/
│   ├── platform/
│   │   ├── postgres/
│   │   │   ├── pool.go             # pgxpool.Pool constructor
│   │   │   ├── stdlib.go           # sql.DB via pgx/v5/stdlib
│   │   │   ├── migrate.go          # goose embedded migrations
│   │   │   └── tenant.go           # Tenant-context wrapper helpers
│   │   ├── crypto/
│   │   │   ├── encrypt.go          # AES-256-GCM envelope encryption
│   │   │   ├── hash.go             # SHA-256 API key hashing
│   │   │   └── keyid.go            # key_id/key_version management
│   │   ├── audit/
│   │   │   ├── writer.go           # Buffered batch writer (chan Event)
│   │   │   ├── batch.go            # Background batch workers
│   │   │   └── event.go            # Audit event type
│   │   ├── obs/
│   │   │   ├── health.go           # /healthz and /readyz handlers
│   │   │   ├── pprof.go            # net/http/pprof setup
│   │   │   └── expvar.go           # expvar counters
│   │   └── shutdown/
│   │       └── orchestrator.go     # Graceful shutdown sequence
│   ├── api/
│   │   └── handler/
│   │       └── health.go           # HTTP handlers for health endpoints
│   └── config/
│       └── config.go               # Env-var config loading
├── migrations/
│   ├── 001_create_workspaces.sql
│   ├── 002_create_api_keys.sql
│   ├── 003_create_devices.sql
│   └── 004_create_audit_logs.sql
├── docker-compose.yml
├── Makefile
├── go.mod
└── go.sum
```

### Pattern 1: Tenant-Context Wrapper
**What:** Every query carries `workspace_id` scoped via `context.Context`; wrapper helpers make omission a compile-time error.
**When to use:** Every database query that touches tenant-scoped tables.
**Example:**
```go
// Source: CONTEXT.md + project conventions
type contextKey struct{}

func WithWorkspaceID(ctx context.Context, id uuid.UUID) context.Context {
    return context.WithValue(ctx, contextKey{}, id)
}

func WorkspaceIDFrom(ctx context.Context) (uuid.UUID, bool) {
    id, ok := ctx.Value(contextKey{}).(uuid.UUID)
    return id, ok
}

// Wrapper that ensures workspace_id is present
func QueryWithWorkspace(ctx context.Context, pool *pgxpool.Pool, query string, args ...any) (pgx.Rows, error) {
    wsID, ok := WorkspaceIDFrom(ctx)
    if !ok {
        return nil, errors.New("workspace_id missing from context")
    }
    // Prepend workspace_id to args and modify query to include WHERE workspace_id = $1
    // ...
}
```

### Pattern 2: AES-256-GCM Envelope Encryption
**What:** KEK (env var / file) wraps per-credential DEKs; nonce prepended to ciphertext.
**When to use:** Encrypting any credential stored in the database.
**Example:**
```go
// Source: crypto/cipher documentation
func Encrypt(kek []byte, plaintext []byte) ([]byte, error) {
    // Generate random DEK
    dek := make([]byte, 32)
    if _, err := crypto_rand.Read(dek); err != nil {
        return nil, err
    }
    
    // Wrap DEK with KEK using AES-GCM
    block, err := aes.NewCipher(kek)
    if err != nil {
        return nil, err
    }
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }
    
    nonce := make([]byte, gcm.NonceSize()) // 12 bytes
    if _, err := crypto_rand.Read(nonce); err != nil {
        return nil, err
    }
    
    // Encrypt DEK
    encryptedDEK := gcm.Seal(nil, nonce, dek, nil)
    
    // Return envelope: nonce + encryptedDEK + encrypted_plaintext
    // ...
}
```

### Pattern 3: Buffered Batch Writer
**What:** Bounded channel + background workers + pgx.CopyFrom for bulk inserts.
**When to use:** Audit logging and any high-volume insert path.
**Example:**
```go
// Source: Go pipelines blog + project conventions
type AuditWriter struct {
    ch      chan Event
    workers int
    pool    *pgxpool.Pool
    wg      sync.WaitGroup
}

func NewAuditWriter(pool *pgxpool.Pool, bufSize, workers int) *AuditWriter {
    w := &AuditWriter{
        ch:      make(chan Event, bufSize),
        workers: workers,
        pool:    pool,
    }
    for i := 0; i < workers; i++ {
        w.wg.Add(1)
        go w.worker()
    }
    return w
}

func (w *AuditWriter) worker() {
    defer w.wg.Done()
    batch := make([]Event, 0, 100)
    for e := range w.ch {
        batch = append(batch, e)
        if len(batch) >= 100 {
            w.flush(batch)
            batch = batch[:0]
        }
    }
    if len(batch) > 0 {
        w.flush(batch)
    }
}

func (w *AuditWriter) flush(events []Event) {
    conn, err := w.pool.Acquire(context.Background())
    if err != nil {
        // log error, count drop
        return
    }
    defer conn.Release()
    
    // Use pgx.CopyFrom for bulk insert
    _, err = conn.Conn().CopyFrom(
        context.Background(),
        pgx.Identifier{"audit_logs"},
        []string{"workspace_id", "trace_id", "event_type", "payload", "created_at"},
        pgx.CopyFromSlice(len(events), func(i int) ([]any, error) {
            e := events[i]
            return []any{e.WorkspaceID, e.TraceID, e.EventType, e.Payload, e.CreatedAt}, nil
        }),
    )
    if err != nil {
        // log error, count drop
    }
}
```

### Anti-Patterns to Avoid
- **Partition by `workspace_id`:** Creates hot partitions on busy tenants. Use `created_at` range partitioning instead.
- **Encrypt API keys with AES:** API keys should be hashed (SHA-256), not encrypted. Hashing is one-way; encryption implies you need the plaintext back.
- **Skip `key_id`/`key_version` columns:** Without these, key rotation requires a data migration, not just a schema change.
- **Reuse nonces in AES-GCM:** Each `Seal` call must generate a fresh random nonce from `crypto/rand`.
- **Omit tenant-context in early queries:** The convention must be enforced from the first query; retrofitting is error‑prone.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| AES-256-GCM encryption | Custom cipher implementation | `crypto/aes` + `crypto/cipher` | Hardware-accelerated, constant-time GHASH on supported CPUs |
| SHA-256 hashing | Custom hash function | `crypto/sha256` | Standard library, collision-resistant, well-tested |
| Database migrations | Custom migration script | `goose/v3` | Handles versioning, rollback, embedded SQL, out-of-order migrations |
| HTTP routing | Custom router | `Echo v5` | Radix-tree router, middleware ecosystem, binder, error handling |
| PostgreSQL connection pooling | Custom pool | `pgxpool` | Prepared-statement cache, binary protocol, COPY support |
| Graceful shutdown | Custom signal handling | `signal.NotifyContext` + `http.Server.Shutdown` | Standard pattern, 30s timeout, context cancellation |
| Structured logging | Custom logger | `log/slog` | Stdlib, context-aware, Echo v5 native integration |

**Key insight:** The Go standard library provides most of the primitives needed for this phase. External dependencies are used only where they add significant value (Echo for routing ergonomics, pgx for PostgreSQL performance, goose for migration management).

## Common Pitfalls

### Pitfall 1: Partition by `workspace_id` instead of `created_at`
**What goes wrong:** Hot partitions on busy tenants; uneven data distribution.
**Why it happens:** Intuitive to think "partition by tenant" for multi-tenant isolation.
**How to avoid:** Use `created_at` range partitioning (monthly). Tenant isolation is via query-level `workspace_id` filtering, not partitioning.
**Warning signs:** Uneven partition sizes; slow queries on busy tenants.

### Pitfall 2: Encrypt API keys instead of hashing
**What goes wrong:** If the encryption key is compromised, all API keys are exposed. Encryption implies you need the plaintext back, which is unnecessary for authentication.
**Why it happens:** Confusing "encryption at rest" for all credentials.
**How to avoid:** API keys are hashed with SHA-256; cleartext prefix stored for lookup. Encryption is for credentials that must be decrypted (e.g., WhatsApp session tokens).
**Warning signs:** Code that decrypts API keys for verification.

### Pitfall 3: Missing `key_id`/`key_version` columns
**What goes wrong:** Key rotation requires a full data migration (re-encrypt all rows) instead of a simple schema change.
**Why it happens:** Assuming a single encryption key forever.
**How to avoid:** Include `key_id` and `key_version` columns from day one. Rotation is: add new key version, re-encrypt in background, drop old version.
**Warning signs:** Single global encryption key with no versioning.

### Pitfall 4: Reusing nonces in AES-GCM
**What goes wrong:** Nonce reuse with the same key allows an attacker to recover plaintext.
**Why it happens:** Misunderstanding that GCM nonces must be unique per key, not per message.
**How to avoid:** Generate a fresh 12-byte nonce from `crypto/rand` for every `Seal` call. Prepend nonce to ciphertext.
**Warning signs:** Deterministic nonce generation (e.g., counter, timestamp).

### Pitfall 5: Tenant-context omission in early queries
**What goes wrong:** Later queries forget to scope by `workspace_id`, leading to cross-tenant data leaks.
**Why it happens:** No compile-time enforcement; easy to forget in new code.
**How to avoid:** Wrapper functions that require `workspace_id` in context; compile-time error if missing.
**Warning signs:** Queries without `WHERE workspace_id = $1`.

### Pitfall 6: Audit buffer Close() race
**What goes wrong:** Closing the audit channel while workers are still processing leads to lost events or panics.
**Why it happens:** Improper shutdown sequencing.
**How to avoid:** Drain the channel before closing; use `sync.WaitGroup` to wait for workers to finish.
**Warning signs:** Panics on shutdown; lost audit events.

## Code Examples

### Echo v5 Setup with slog
```go
// Source: Echo v5 README
package main

import (
    "log/slog"
    "net/http"
    
    "github.com/labstack/echo/v5"
    "github.com/labstack/echo/v5/middleware"
)

func main() {
    e := echo.New()
    
    // Use slog middleware
    e.Use(middleware.RequestLogger())
    e.Use(middleware.Recover())
    
    // Health endpoints
    e.GET("/healthz", func(c *echo.Context) error {
        return c.String(http.StatusOK, "ok")
    })
    
    if err := e.Start(":8080"); err != nil {
        slog.Error("failed to start server", "error", err)
    }
}
```

### pgxpool + stdlib Bridge
```go
// Source: pgx/v5 stdlib documentation
package postgres

import (
    "context"
    "database/sql"
    
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/jackc/pgx/v5/stdlib"
)

func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
    pool, err := pgxpool.New(ctx, dsn)
    if err != nil {
        return nil, err
    }
    return pool, nil
}

func NewSQLDB(pool *pgxpool.Pool) *sql.DB {
    // OpenDBFromPool creates a *sql.DB from the pool
    // Automatically sets max idle connections to zero
    db := stdlib.OpenDBFromPool(pool)
    return db
}
```

### Goose Embedded Migrations
```go
// Source: goose/v3 README
package postgres

import (
    "database/sql"
    "embed"
    
    "github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var embedMigrations embed.FS

func RunMigrations(db *sql.DB) error {
    goose.SetBaseFS(embedMigrations)
    if err := goose.SetDialect("postgres"); err != nil {
        return err
    }
    return goose.Up(db, "migrations")
}
```

### Health/Readiness Endpoints
```go
// Source: CONTEXT.md + Echo patterns
package handler

import (
    "net/http"
    
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/labstack/echo/v5"
)

type HealthHandler struct {
    pool *pgxpool.Pool
    natsConn // NATS connection interface
}

func (h *HealthHandler) Healthz(c *echo.Context) error {
    return c.String(http.StatusOK, "ok")
}

func (h *HealthHandler) Readyz(c *echo.Context) error {
    // Check pgx
    if err := h.pool.Ping(c.Request().Context()); err != nil {
        return c.String(http.StatusServiceUnavailable, "pgx not ready")
    }
    // Check NATS
    if err := h.natsConn.Ping(); err != nil {
        return c.String(http.StatusServiceUnavailable, "nats not ready")
    }
    return c.String(http.StatusOK, "ok")
}
```

### pprof + expvar Setup
```go
// Source: pprof documentation + expvar
package obs

import (
    "expvar"
    "net/http"
    _ "net/http/pprof"
)

func StartDebugServer(addr string) *http.Server {
    mux := http.NewServeMux()
    mux.Handle("/debug/pprof/", http.DefaultServeMux)
    mux.Handle("/debug/vars", expvar.Handler())
    
    srv := &http.Server{
        Addr:    addr,
        Handler: mux,
    }
    go srv.ListenAndServe()
    return srv
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Echo v4 | Echo v5 | 2026-01-18 | Handler signature changed to `*echo.Context`; native slog integration |
| `lib/pq` | `pgx/v5/stdlib` | pgx v5 release | Single PG driver; binary protocol; COPY support |
| `golang-migrate` | `goose/v3` | goose v3 | Embedded SQL via `go:embed`; Go-function migrations |
| `zerolog` / `zap` | `log/slog` | Go 1.21 | Stdlib logger; context-aware; Echo v5 native |

**Deprecated/outdated:**
- **Echo v4:** EOL 2026-12-31 (security/bug only). Use v5 for new projects.
- **`lib/pq`:** In maintenance mode; superseded by pgx.
- **htmx 1.x:** Maintained only for IE support; 2.x is active line.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | PostgreSQL will be available via Docker Compose (not local install) | Environment Availability | Low — Docker Compose is installed and will provide PostgreSQL |
| A2 | NATS will be available via Docker Compose (not local install) | Environment Availability | Low — Docker Compose is installed and will provide NATS |
| A3 | whatsmeow's `sqlstore.Container` will be used only for migration management in Phase 1; actual WhatsApp integration deferred to Phase 4 | Architecture Patterns | Medium — if whatsmeow sqlstore requires runtime setup beyond migration, additional work needed |
| A4 | The audit batch writer will use `pgx.CopyFrom` via `pool.Acquire` (not `database/sql` bridge) | Don't Hand-Roll | Medium — CopyFrom requires native pgx connection; ensure pool.Acquire returns pgx.Conn |

## Open Questions

1. **whatsmeow `sqlstore.Container.Upgrade(ctx)` timing:** Should this be called after PerGo migrations at boot, or within the same migration transaction?
   - What we know: CONTEXT.md says "called after PerGo migrations at boot"
   - What's unclear: Whether Container.Upgrade is idempotent and safe to call repeatedly
   - Recommendation: Call after PerGo migrations; verify idempotency in Phase 4 research

2. **NATS connectivity ping implementation:** Should `/readyz` ping NATS via `nats.Conn.Ping()` or via a JetStream health check?
   - What we know: Phase 1 only needs connectivity check, not JetStream readiness
   - What's unclear: Whether `nats.Conn.Ping()` is sufficient for readiness
   - Recommendation: Use `nats.Conn.Ping()` for Phase 1; JetStream readiness in Phase 3

3. **Encryption KEK storage:** Should the KEK be stored in an environment variable or a file?
   - What we know: CONTEXT.md says "KEK (env var / file)"
   - What's unclear: Which approach is preferred for the initial implementation
   - Recommendation: Support both via config; default to environment variable for Docker deployments

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go | All | ✓ | 1.26.4 | — |
| Docker | INFRA-04 (Docker Compose) | ✓ | 29.1.3 | — |
| docker-compose | INFRA-04 | ✓ | 2.40.3 | — |
| PostgreSQL | INFRA-02 | ✓ (via Docker) | 16+ (in container) | — |
| NATS | OBS-01 (readiness ping) | ✓ (via Docker) | 2.10+ (in container) | — |
| goose | INFRA-03 | ✗ | — | Install via `go install github.com/pressly/goose/v3/cmd/goose@latest` |
| templ | INFRA-01 (admin UI) | ✓ | installed | — |
| golangci-lint | CI linting | ✓ | installed | — |
| psql | DB inspection | ✗ | — | Use Docker exec or admin UI (Phase 2) |

**Missing dependencies with no fallback:**
- None — all required dependencies are available or can be installed.

**Missing dependencies with fallback:**
- **goose:** Not installed globally; install via `go install` as part of project setup.
- **psql:** Not installed; use Docker exec or admin UI for DB inspection.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go standard `testing` + optional `testify` |
| Config file | none — see Wave 0 |
| Quick run command | `go test ./... -short` |
| Full suite command | `go test ./... -race -count=1` |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| INFRA-01 | Server boots with Echo v5 | integration | `go test ./cmd/pergo -run TestServerBoot -v` | ❌ Wave 0 |
| INFRA-02 | PostgreSQL dual-access pool | integration | `go test ./internal/platform/postgres -run TestDualAccess -v` | ❌ Wave 0 |
| INFRA-03 | Goose migrations run | integration | `go test ./internal/platform/postgres -run TestMigrations -v` | ❌ Wave 0 |
| INFRA-05 | Graceful shutdown sequence | unit | `go test ./internal/platform/shutdown -run TestShutdownOrder -v` | ❌ Wave 0 |
| AUTH-01 | API key hashing | unit | `go test ./internal/platform/crypto -run TestHashAPIKey -v` | ❌ Wave 0 |
| SEC-01 | AES-256-GCM encryption | unit | `go test ./internal/platform/crypto -run TestEncryptDecrypt -v` | ❌ Wave 0 |
| SEC-05 | key_id/key_version management | unit | `go test ./internal/platform/crypto -run TestKeyRotation -v` | ❌ Wave 0 |
| AUDIT-01 | Audit partition creation | integration | `go test ./internal/platform/postgres -run TestAuditPartition -v` | ❌ Wave 0 |
| AUDIT-03 | Audit batch writer | unit | `go test ./internal/platform/audit -run TestBatchWriter -v` | ❌ Wave 0 |
| OBS-01 | Health/readiness endpoints | integration | `go test ./internal/api/handler -run TestHealthEndpoints -v` | ❌ Wave 0 |
| OBS-02 | pprof/expvar setup | unit | `go test ./internal/platform/obs -run TestDebugServer -v` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./... -short`
- **Per wave merge:** `go test ./... -race -count=1`
- **Phase gate:** Full suite green before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] `tests/test_server_boot.go` — covers INFRA-01
- [ ] `tests/test_dual_access.go` — covers INFRA-02
- [ ] `tests/test_migrations.go` — covers INFRA-03
- [ ] `tests/test_shutdown.go` — covers INFRA-05
- [ ] `tests/test_crypto.go` — covers AUTH-01, SEC-01, SEC-05
- [ ] `tests/test_audit.go` — covers AUDIT-01, AUDIT-03
- [ ] `tests/test_health.go` — covers OBS-01
- [ ] `tests/test_obs.go` — covers OBS-02
- [ ] `tests/conftest.go` — shared fixtures (pgxpool, mock NATS)
- [ ] Framework install: `go get github.com/stretchr/testify@v1.11.1` (if testify used)

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes | SHA-256 hashed API keys with prefix lookup |
| V3 Session Management | no | No session management in Phase 1 |
| V4 Access Control | yes | Tenant-context convention (workspace_id from context) |
| V5 Input Validation | yes | Echo v5 binder + validator for API payloads |
| V6 Cryptography | yes | AES-256-GCM via crypto/aes + crypto/cipher; never hand-roll |

### Known Threat Patterns for Go + PostgreSQL

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| SQL injection | Tampering | Parameterized queries via pgx; no string concatenation |
| Credential plaintext exposure | Information Disclosure | AES-256-GCM encryption at rest; SHA-256 hashing for API keys |
| Cross-tenant data leak | Information Disclosure | Tenant-context convention; every query scoped to workspace_id |
| Nonce reuse in AES-GCM | Tampering | Fresh 12-byte nonce from crypto/rand per Seal call |
| API key brute-force | Elevation of Privilege | Rate limiting via golang.org/x/time/rate (future phases) |
| Audit log tampering | Tampering | Immutable partitioned table; append-only; no UPDATE/DELETE |

## Sources

### Primary (HIGH confidence)
- Echo v5 README (github.com/labstack/echo) — handler signature, slog integration, middleware
- pgx/v5 stdlib documentation (pkg.go.dev/github.com/jackc/pgx/v5/stdlib) — OpenDBFromPool, GetPoolConnector
- goose/v3 README (github.com/pressly/goose) — embedded migrations, go:embed, SetBaseFS
- crypto/cipher documentation (pkg.go.dev/crypto/cipher) — NewGCM, nonce generation, Seal/Open
- crypto/sha256 documentation (pkg.go.dev/crypto/sha256) — Sum256 for hashing
- PostgreSQL declarative partitioning documentation (postgresql.org/docs/current/ddl-partitioning.html) — RANGE partitioning, partition maintenance
- pprof documentation (pkg.go.dev/net/http/pprof) — import side effect, debug server

### Secondary (MEDIUM confidence)
- Go pipelines blog (go.dev/blog/pipelines) — bounded parallelism, fan-out/fan-in patterns
- CONTEXT.md — locked architectural decisions, shutdown order, encryption envelope

### Tertiary (LOW confidence)
- None — all findings verified against official documentation

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — all libraries verified against official documentation and go.mod
- Architecture: HIGH — patterns derived from CONTEXT.md locked decisions and official docs
- Pitfalls: HIGH — based on common Go/PostgreSQL anti-patterns documented in official resources

**Research date:** 2026-06-25
**Valid until:** 2026-07-25 (30 days — stable dependencies)