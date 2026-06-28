# Architecture Research

**Domain:** Self-hosted omnichannel CPaaS / messaging gateway (Go)
**Researched:** 2026-06-25
**Confidence:** HIGH (validated against official NATS JetStream, whatsmeow store/sqlstore, and pgx v5 documentation, plus a production whatsmeow-based reference implementation)

## Purpose of this document

The PRD (`docs/PRD PerGo.md`) and the six-part `docs/architecture/` set already prescribe a
coherent, unusually mature architecture. This research **validates that prescription against
external evidence** and surfaces the structural gaps and refinements the roadmap must absorb.
It is opinionated: where the PRD is right, it says so with the evidence; where a stronger or
simpler mechanism exists, it names it.

---

## Standard Architecture

### System Overview

The validated flow is a **durable work-queue pipeline** with a single ingestion endpoint, a
broker as the durability boundary, stateless channel workers behind a plugin interface, an
async audit fan-in, and a separate durable stream for outbound webhooks. PostgreSQL holds
identity and audit only — never hot-path queue state.

```
                        ┌─────────────────────────────────────────────────────────┐
                        │                    Ingestion (Echo)                      │
                        │  POST /messages → API-key auth (cached) → validate        │
                        │  → backpressure check → Trace-ID → JetStream publish      │
                        │  → 202 Accepted { trace_id }                              │
                        └───────────────────────────┬─────────────────────────────┘
                                                    │ Publish (Trace-ID + Workspace headers,
                                                    │  Nats-Msg-Id = trace_id  [RECOMMENDED])
                                                    ▼
                        ┌─────────────────────────────────────────────────────────┐
                        │        NATS JetStream — WorkQueuePolicy stream           │
                        │  (messages.<workspace>.<channel> subjects)               │
                        │  At-least-once · single consumer per subject · MaxDeliver│
                        │  MaxMsgsPerSubject=1000 + DiscardNew  [RECOMMENDED]      │
                        └───────────────────────────┬─────────────────────────────┘
                                                    │ Pull consumer (Fetch batch, AckExplicit)
                                                    ▼
                        ┌─────────────────────────────────────────────────────────┐
                        │            Channel Worker Pool (N goroutines)            │
                        │  unmarshal → RoutingEngine.ResolveDelivery (SEQUENTIAL)  │
                        │  → channel.Dispatcher → provider send → Ack / Nak / Term │
                        └──────┬───────────────┬───────────────┬───────────┬───────┘
                               │               │               │           │
                               ▼               ▼               ▼           ▼
                        ┌──────────┐    ┌──────────┐    ┌──────────┐  ┌──────────┐
                        │whatsapp- │    │whatsapp- │    │telegram  │  │ audit     │
                        │web       │    │cloud(WABA│    │(REST)    │  │ buffer    │
                        │(whatsmeow│    │ REST)    │    │          │  │ (chan     │
                        │ WS+sess.)│    │+breaker  │    │+breaker  │  │  cap5000) │
                        └─────┬────┘    └──────────┘    └──────────┘  └─────┬─────┘
                              │  per-session rate.Limiter (1-3s stagger)    │ batch
                              │  Session registry (RWMutex map[JID])        │ CopyFrom
                              ▼                                             ▼
                        ┌─────────────────────────────────────────────────────────┐
                        │   PostgreSQL  (system of record — identity + audit)      │
                        │  workspaces · api_keys · devices · audit_logs(partitioned)│
                        │  + whatsmeow's OWN tables (prekeys/sessions/identities)   │
                        └─────────────────────────────────────────────────────────┘
                                                    │
                        ┌───────────────────────────┴─────────────────────────────┐
                        │   Webhook delivery — separate JetStream stream + consumer │
                        │   outbound HTTPS POST · MaxDeliver=10 · exp backoff · DLQ  │
                        └─────────────────────────────────────────────────────────┘
```

### Component Responsibilities

| Component | Responsibility | Validated implementation |
|-----------|----------------|--------------------------|
| **Ingestion gateway** | Parse, validate, attach Trace-ID, enforce backpressure, publish to broker, return 202 | Echo handler; **two I/O ops max** on hot path (cached auth + publish). Validated by `01-architectural-summary.md` and load envelope. |
| **NATS JetStream stream** | Durability boundary for outbound work; at-least-once; single consumer per subject | `WorkQueuePolicy` stream. **Validated**: official docs confirm one consumer per subject, ack-deletes, MaxDeliver cap, no auto-DLQ. |
| **Channel worker pool** | Pull messages, route via fallback pipeline, dispatch to provider, ack/nak/term | N pull-consumer goroutines (`Fetch(10)` + `FetchMaxWait`). **Validated** as the correct pull-consumer batch pattern. |
| **Routing engine** | Resolve ordered `fallback_channels` sequentially; advance on terminal error | **Validated**: sequential is correct — parallel dispatch would send the message N times. |
| **Channel adapters** | Implement consumer-side `Dispatcher` interface; encapsulate provider SDKs | whatsappweb (whatsmeow), whatsappcloud (REST + breaker), telegram (REST + breaker). **Validated** as a plugin boundary. |
| **Session manager** | Lifecycle of stateful WhatsApp Web WebSocket sessions; reconnect on restart | `sync.RWMutex` map[JID]*Session, one goroutine per device, device identity in PostgreSQL via whatsmeow `sqlstore.Container`. **Validated** with caveats (see gaps). |
| **Audit engine** | Record every state transition; non-blocking on hot path; batched writes | Bounded `chan Event` (cap 5000) + M batch-writer goroutines → `pgx.CopyFrom`. **Validated** as the canonical DB-protecting pattern. |
| **Webhook delivery** | Durable outbound POST to consumer URLs with retries and DLQ | Dedicated JetStream stream + consumer. **Validated** by reference impl (86 event types, per-device quotas). |
| **Admin panel** | Server-rendered console: workspaces, QR pairing, telemetry, audit review | Echo + Templ + HTMX. **Validated** as the right scope (operator console, not SPA). |
| **API-key auth** | SHA-256 hashed keys, prefix lookup, in-memory cache with TTL | Middleware + pgx repo. **Validated** — cache removes DB from hot path. |

---

## Recommended Project Structure

The PRD's domain-oriented package layout (`docs/architecture/03-directory-structure.md`) is
**correct and validated**. Domain packages own their types; `internal/platform/` is the only
place that knows about pgx/nats/whatsmeow plumbing; adapters are siblings sharing an interface,
not a hierarchy; `cmd/pergo` is the sole composition root. **Two refinements emerge from
research:**

1. **`internal/platform/postgres/` must host TWO pool constructors** — `pgxpool.Pool` for
   PerGo (auth, audit, devices metadata) AND a `*sql.DB` for whatsmeow's `sqlstore.Container`.
   whatsmeow speaks `database/sql`, not pgx native (confirmed: `sqlstore.NewWithDB(*sql.DB,
   ...)`). They share the PostgreSQL *database*, not the driver stack or connection pool.
2. **`internal/platform/migrations/` must run TWO migration systems** at boot: goose (for
   PerGo's `workspaces`/`api_keys`/`devices`/`audit_logs`) AND `Container.Upgrade(ctx)` (for
   whatsmeow's own `whatsmeow_device`/`whatsmeow_prekey`/`whatsmeow_session`/... tables).
   These are non-overlapping schemas in the same DB.

```
internal/
├── platform/
│   ├── postgres/
│   │   ├── pool.go          # *pgxpool.Pool for PerGo
│   │   ├── sqlstore_pool.go # *sql.DB for whatsmeow (lib/pq or pgx/stdlib)  [GAP]
│   │   ├── migrations/      # goose *.sql for PerGo tables
│   │   └── audit_repo.go    # CopyFrom via pool.Acquire -> conn.CopyFrom
│   ├── nats/ …
│   ├── crypto/ …
│   ├── trace/ …
│   ├── backoff/ …
│   ├── breaker/ …
│   └── server/ …
├── messaging/ …   # ingest, routing, queue, worker
├── channel/ …     # dispatcher interface + whatsappweb/whatsappcloud/telegram
├── session/ …     # registry, session, manager, store  (+ reconnect-storm semaphore [GAP])
├── webhook/ …
├── audit/ …
├── apikey/ …
└── admin/ …
```

### Structure rationale

- **`platform/` imports nothing internal** — swap broker or DB driver by touching one subtree.
- **`channel` does not import `messaging/worker`** — dependency inverted via the consumer-side
  `Dispatcher` interface defined in `channel/`. This is the load-bearing plugin boundary that
  isolates unofficial-protocol breakage (whatsmeow) from core.
- **`session` depends on `channel/whatsappweb` concretely** — only WhatsApp Web is
  session-ful; WABA and Telegram are stateless REST and do not need the session manager.

---

## Architectural Patterns

### Pattern 1: Durable work-queue boundary (NATS JetStream `WorkQueuePolicy`)

**What:** A single JetStream stream with `WorkQueuePolicy` retention is the durability
boundary for all outbound work. Each message is consumed by exactly one consumer and deleted
on ack. Workers are stateless and crash-safe — anything not acked is the broker's
responsibility, not the process's.

**When to use:** Whenever async decoupling is required (ingest <50ms vs. 1–3s staggered send)
and at-least-once delivery is acceptable.

**Trade-offs:** At-least-once, **not** exactly-once — redelivery after a worker crash means
downstream side effects (a WhatsApp message to a human) must be idempotent at the provider
boundary or deduplicated by `trace_id` before dispatch. **Validated** by official NATS docs:
WorkQueuePolicy enforces "one consumer per subject — no overlapping consumer filters." This
constrains the worker topology (see Pattern 7).

**Validated refinement:** JetStream supports **publish-side idempotency** via the
`Nats-Msg-Id` header + `DuplicateWindow` (default 2 min). The PRD's `queue.go` does not set
it. **Recommendation: set `Nats-Msg-Id = trace_id` (or a client-supplied idempotency key) on
publish** to make client retries no-ops at the broker. This is the single most valuable
addition to the ingest path.

### Pattern 2: Two-I/O hot path

**What:** The request goroutine performs exactly two external operations — cached API-key
lookup + JetStream publish — then returns `202`. Validation, trace generation, backpressure
check, and audit are all moved off the request goroutine (audit is non-blocking; everything
else is in-memory or broker-side).

**When to use:** Any path with a latency budget (here 50ms p99). **Validated** — this is the
correct shape for the stated envelope; a synchronous DB write for auth or audit would blow
the budget.

### Pattern 3: Sequential fallback pipeline

**What:** `RoutingEngine.ResolveDelivery` iterates `fallback_channels` **in order**, calling
`Dispatch` on each until one succeeds. On a terminal error (`channel.Terminal`), it advances
immediately without redelivering; on a transient error, the next fallback is tried and the
original message's redelivery is governed by the worker's ack/nak.

**When to use:** Ordered fallback where delivering the message more than once is unacceptable.

**Trade-offs:** Sequential is slower than parallel but **parallel dispatch would send the
message N times** — a correctness violation. `errgroup` is explicitly wrong here. **Validated**
by `04-concurrency-performance.md` ("No errgroup for the fallback pipeline").

### Pattern 4: Bounded-buffer audit fan-in (non-blocking on hot path)

**What:** `audit.Buffer` is a `chan Event` (cap 5000) drained by M batch-writer goroutines.
Each writer accumulates up to 500 events or 50ms, then issues a single `pgx.CopyFrom`. The
hot-path `Record` call is **non-blocking** (`select { case ch <- e: default: drop+count }`) —
a full buffer drops + increments an `expvar` counter rather than stalling the 50ms budget.

**When to use:** High-throughput append-only writes where best-effort on the hot path is
acceptable and the SLO is tracked via the drop counter.

**Validated refinement:** `pgx.Conn.CopyFrom` is the fastest append path in pgx (COPY
protocol, faster than INSERT with as few as 5 rows). **It is a method on `*pgx.Conn`/`pgx.Tx`,
NOT on `*pgxpool.Pool`** — the batch writer must `pool.Acquire(ctx)` → `conn.CopyFrom(...)`
(or `pool.BeginTx` → `tx.Conn.CopyFrom`) per batch. The PRD's `sink.CopyFrom(ctx, batch)`
signature is correct; the implementation must acquire a connection per batch.

### Pattern 5: Per-session rate-limited goroutine (staggered dispatch)

**What:** Each WhatsApp Web `Session` owns a `*rate.Limiteriter` sized to the unofficial
policy (`rate.Every(1–3s jittered), burst 1`). `limiter.Wait(ctx)` blocks the worker goroutine
but releases the P to the scheduler, so thousands of independent session queues progress
concurrently on 2 vCPUs. No global limiter, no shared mutex on the dispatch path.

**When to use:** Unofficial channels that need human-like pacing to avoid suspension, plus
high fan-out across many sessions. **Validated** — this is the mechanism that makes the 500
msg/s target reachable on 2 vCPU (throughput comes from *many concurrent sessions*, not faster
sessions).

### Pattern 6: Consumer-side `Dispatcher` interface (plugin boundary)

**What:** `channel.Dispatcher` is defined in the `channel` package (consumer-side), with
three implementations (`whatsappweb`, `whatsappcloud`, `telegram`) as siblings. `messaging`
depends on the interface, not the implementations; `channel` does not import `messaging/worker`.

**When to use:** Whenever an unofficial protocol library (whatsmeow) may break on upstream
changes and must be upgradeable without touching core. **Validated** as the PRD's stated
risk-mitigation for unofficial protocol updates.

### Pattern 7: WorkQueuePolicy single-consumer topology decision

**What:** NATS `WorkQueuePolicy` permits only **one consumer per subject** (no overlapping
filters). The PRD's worker code (`06-core-code-example.md` §6.6) uses `PullSubscribe("",
consumer, ...)` — an empty filter binding the whole stream. **This is valid ONLY as a single
consumer** that dispatches to all channels via the in-process `RoutingEngine`.

**When to use / trade-off:** There are two viable topologies, and the architecture must pick
one explicitly:
- **(A) Single consumer + in-process RoutingEngine** (what the PRD code shows). Simpler;
  scales by adding goroutines to the one consumer's pool; channel selection is in-process.
  **This is the recommended default** — it matches the code and avoids the overlap constraint.
- **(B) Per-channel durable consumers on non-overlapping subjects**
  (`messages.*.whatsappweb.>`, `messages.*.waba.>`, `messages.*.telegram.>`). Scales by adding
  worker *processes* per channel; but requires per-channel consumers and breaks the unified
  fallback pipeline (fallback would need cross-subject requeue).

**Recommendation:** Keep topology (A). Document the single-consumer decision explicitly so a
future contributor doesn't try to add a second consumer on the same stream and hit JetStream's
"overlapping consumers" error. If horizontal worker *process* scaling is ever needed, scale
via a queue-group push consumer on a `LimitsPolicy` mirror stream instead of splitting the
work-queue stream.

---

## Data Flow

### Ingest → broker → worker → channel → audit

```
HTTP POST /messages
  │
  ├─ Echo middleware: API-key parse → SHA-256 hash → in-mem cache lookup (HIT) ─┐
  │                                                                             │
  ├─ Bind JSON → MessagePayload → Validate()                                   │
  ├─ Backpressure: per-session pending check (in-mem atomic OR                 │
  │                 JetStream MaxMsgsPerSubject, see gaps) ─────────────────┐  │
  │                                                                         ▼  ▼
  ├─ trace.New() → ctx = trace.With(ctx, tid); msg.TraceID = tid
  ├─ Queue.Publish(ctx, &msg):
  │     subject = messages.<workspace>.<channel>
  │     hdr.Set("Trace-Id", tid); hdr.Set("Workspace", ws)
  │     js.Publish(..., WithHeaders(hdr), WithMsgID(tid))   ← RECOMMENDED dedup
  │     → on ErrQueueFull → 429 + Retry-After:5
  │     → on other err    → 503
  └─ return 202 { trace_id }                                  ← ≤50ms p99 budget

NATS JetStream (durable)  ──pull──►  Worker goroutine
  c.Fetch(10, FetchMaxWait=5s)
  for msg := range batch.Messages():
    tid = msg.Headers.Get("Trace-Id")
    ctx = trace.With(ctx, tid)
    json.Unmarshal(msg.Data(), &m); m.TraceID = tid
    err = RoutingEngine.ResolveDelivery(ctx, &m)
      for ch in [primary]+fallback_channels:
        d = registry.Get(ch)
        err = d.Dispatch(ctx, &m)        ← whatsappweb: session.Wait(ctx) then client.SendMessage
        audit.Record(ctx, Event{tid, ws, ch, Status})   ← non-blocking
        if err == nil: break
        if errors.As(err, &terminal): continue          ← advance, do not redeliver
    if err == nil: msg.Ack()
    else:          msg.Nak(NakDelay(2s))                ← JetStream redelivers
    // poison message (unmarshal fail): msg.Term()     ← stop redelivery
```

### Trace-ID propagation (4 boundaries — the 100% correlation SLO)

| Boundary | Mechanism | Validated |
|----------|-----------|-----------|
| HTTP → handler | `trace.New()` → `context.WithValue` | yes |
| Handler → NATS | `nats.Header.Set("Trace-Id", tid)` on publish | yes |
| NATS → worker | `msg.Headers().Get("Trace-Id")` → `trace.With(ctx, tid)` | yes |
| Worker → SQL | `ctx` carried into `pgx` calls + `slog` logger field | yes |

**Pitfall:** Loss at *any* boundary breaks the SLO. The worker **must** re-inject the Trace-ID
from the NATS header into the context (the message body's `TraceID` is a fallback, not the
primary). **Validated** — the PRD's §6.6 does this correctly.

### Webhook delivery flow (separate durable stream)

Provider delivery receipts / status transitions are published to a **second** JetStream stream
(`webhooks`), consumed by a dedicated worker that POSTs to the consumer URL with
`MaxDeliver=10` and exponential `AckWait` (1s, 5s, 30s, 2m, 10m…). Permanently-4xx consumers
are NAK'd to `MaxDeliver`, then moved to a `webhooks_dlq` stream surfaced in the admin console.
**Validated** by the reference impl (86 webhook event types, retries, per-device quotas).

---

## Concurrency Model Validation

| Primitive | Location | PRD claim | Research verdict |
|-----------|----------|-----------|------------------|
| Echo handler goroutine (1/req) | `messaging/ingest` | request-scoped, two I/O | **Validated** — correct for 50ms budget |
| Pull-consumer goroutines (N=`2*NumCPU`) | `messaging/worker` | process lifetime, `Fetch(10)` | **Validated** — pull consumers recommended for new projects; `MaxWaiting`/`MaxAckPending` give flow control |
| 1 goroutine per WhatsApp device | `session/manager` | session-scoped, `<-ctx.Done()` + disconnect | **Validated** — matches whatsmeow `Client.Connect()` event-loop model |
| `sync.RWMutex` + `map[JID]*Session` | `session/registry` | read-heavy fast path | **Validated** — `RWMutex` is correct over `sync.Map` for well-known typed keys; never hold lock across a network call |
| Bounded `chan Event` (cap 5000) + M writers | `audit/buffer` | non-blocking `Record`, batch `CopyFrom` | **Validated** — canonical bounded-buffer fan-in; drop+count is the right hot-path policy |
| `*rate.Limiteriter` per session (1–3s, burst 1) | `session/session` | `Wait(ctx)` yields P | **Validated** — `golang.org/x/time/rate` is the std-extension token bucket; per-session isolation is correct |
| K webhook pull-consumer goroutines | `webhook/worker` | durable outbound delivery | **Validated** — separate stream + consumer is the right durability boundary |
| `errgroup` for fallback | — | **explicitly NOT used** | **Validated** — sequential fallback is a correctness requirement, not a parallelism opportunity |
| Worker-pool framework | — | **explicitly NOT used** | **Validated** — `for i:=0;i<N;i++ { go loop() }` is the pool |

**One concurrency gap surfaced:** startup reconnect of many paired devices is unguarded.
`Container.GetAllDevices()` → `for each: go connect()` with no limit can storm WhatsApp on
restart. **See Missing Components #5.**

---

## Suggested Build Order (milestone validation & refinement)

The PRD proposes three milestones. Research **validates the ordering** (each unlocks the next)
and **refines the contents** to absorb the gaps.

### Milestone 1 — Core Foundation (weeks 1–3) ✅ validated, minor refinement

**Build first because everything depends on identity, config, and the ingest contract.**

- Echo server + `cmd/pergo` composition root + `platform/server`
- PostgreSQL: **both** pool constructors (`pgxpool` + `*sql.DB` for whatsmeow) [GAP #4]
- **Both** migration runners at boot: goose (PerGo tables) + `Container.Upgrade` [GAP #4]
- Schemas: `workspaces`, `api_keys` (SHA-256 + prefix), `devices` (metadata), `audit_logs`
  (partitioned by `workspace_id` or `created_at` day)
- `platform/trace`, `platform/crypto` (AES-256-GCM + SHA-256 apikey), `platform/backoff`
- API-key auth middleware + in-mem cache
- `audit` engine (buffer + batch writer + `CopyFrom` via `pool.Acquire`) — build it here so
  M2/M3 can record from day one
- Templ + HTMX admin shell (workspaces, key gen) — QR pairing deferred to M2
- `/healthz` (liveness) + `/readyz` (pgx ping + nats ping) + pprof on `localhost:6060`
- Graceful shutdown scaffolding (root ctx, `Echo.Shutdown`, 30s ceiling) — establish the
  pattern now even though workers come in M2

**Refinement:** Do NOT bring up NATS in M1 beyond a connectivity check. The ingest handler
can return `503` until M2 wires the real publish. This keeps M1 focused on identity + audit.

### Milestone 2 — Queue & WhatsApp Web (weeks 4–6) ✅ validated, key refinements

**Build second because the broker is the durability boundary and WhatsApp Web is the
highest-risk channel — de-risk it early.**

- NATS JetStream: provision `WorkQueuePolicy` stream with subjects `messages.<ws>.<channel>`;
  **set `MaxMsgsPerSubject=1000` + `DiscardNew` for native backpressure** [GAP #2]
- `messaging/queue` publish with **`WithMsgID(trace_id)`** for publish-side dedup [GAP #1]
- `messaging/worker` pull consumer (topology A: single consumer + RoutingEngine) [Pattern 7]
- `session` package: registry, per-session `rate.Limiter`, manager with backoff reconnect
- **Startup reconnect-storm protection**: semaphore (e.g. 8 concurrent) + jittered backoff over
  `GetAllDevices()` [GAP #5]
- **WA Web version management**: `SetWAVersion` + auto-refresh on "client outdated" [GAP #6]
- whatsmeow `sqlstore.Container` wiring via `*sql.DB`; **AD-JID** for `GetDevice` [GAP #9]
- `channel/whatsappweb` adapter implementing `Dispatcher`
- Backpressure: in-memory atomic counter per session, reconciled against
  `StreamInfo` every 10s (or rely on `MaxMsgsPerSubject` broker rejection) [GAP #2]
- Ingest handler wired to real publish; 429 on `ErrQueueFull`
- QR pairing admin page (live refresh via HTMX/SSE)

**Refinement:** This is the right milestone to load-test the ingest path (202 throughput,
backpressure 429 behavior) *before* adding official channels.

### Milestone 3 — Official Channels & Fallback (weeks 7–8) ✅ validated

**Build last because official channels are stateless REST and lower-risk; fallback needs ≥2
channels to demonstrate.**

- `channel/whatsappcloud` (WABA REST) + `channel/telegram` (Bot REST), each behind
  `platform/breaker` (breaker open → fallback trigger, not retry)
- `RoutingEngine` sequential fallback with `channel.Terminal` error typing
- Webhook delivery: second JetStream stream + consumer, exp backoff, `webhooks_dlq` + admin
  view [GAP #3 — also add a MaxDeliver-exhausted sweep for the *messages* stream]
- End-to-end load test: 500 req/s sustained, 50ms p99 ingest, 99.5% delivery, <512MB RAM
- 30/60/90-day eval metrics instrumentation (`expvar` counters + pprof leak audit)

**Phase-ordering rationale:** M1 → M2 is a hard dependency (identity + audit must exist before
any message flows). M2 → M3 is a risk-ordering dependency (WhatsApp Web is the fragile,
unofficial channel; de-risk it before the easy official ones; fallback needs ≥2 channels).
Bringing official channels in M2 would split attention from the highest-risk integration.

---

## Missing Components & Structural Gaps

These are the components the PRD does not explicitly prescribe but research shows are required
or strongly recommended. Ordered by impact.

### GAP 1 — Publish-side idempotency via `Nats-Msg-Id` (HIGH impact, LOW effort)
The PRD flags idempotency as a challenge but prescribes no publish-side mechanism. JetStream
natively deduplicates on the `Nats-Msg-Id` header within `DuplicateWindow` (default 2 min).
**Action:** set `Nats-Msg-Id = trace_id` (or accept a client `Idempotency-Key` header) in
`Queue.Publish`. This makes client retries no-ops at the broker and is the cheapest
exactly-once-publish available. *(Official NATS docs, HIGH confidence.)*

### GAP 2 — Native per-subject backpressure via `MaxMsgsPerSubject` + `DiscardNewPerSubject` (HIGH impact, LOW effort)
The PRD's backpressure design (in-memory atomic counter, reconciled against `StreamInfo`
every 10s) works but is application-layer. JetStream has a **native** mechanism since server
2.9: `MaxMsgsPerSubject=1000` + `DiscardPolicy=DiscardNew` (+ `DiscardNewPerSubject=true`)
enforces the 1,000-message per-session limit *at the broker* — a publish that would exceed it
is rejected. **Action:** set these on the stream; treat the publish error as the 429 trigger.
Keep the in-memory counter only as a fast-path pre-check to avoid a broker round-trip.

### GAP 3 — No automatic dead-letter for the messages stream (MEDIUM impact, MEDIUM effort)
Official NATS docs: messages that hit `MaxDeliver` on a `WorkQueuePolicy` stream **stay in the
stream and must be manually deleted** — there is no auto-DLQ. The PRD describes a `webhooks_dlq`
but not the equivalent for the *messages* stream. **Action:** add a JetStream advisory
listener (`$JS.EVENT.ADVISORY.MAX_DELIVERIES.<stream>.<consumer>`) that, on fire, copies the
dead message to a `messages_dlq` stream and `purge`s it from the original. Surface DLQ depth
in the admin panel.

### GAP 4 — Two driver stacks and two migration systems (MEDIUM impact, LOW effort — documentation/structure)
whatsmeow's `sqlstore.Container` uses the `database/sql` interface (`NewWithDB(*sql.DB, ...)`,
dialect `"postgres"`), **not** pgx native. PerGo therefore runs **two** PostgreSQL driver
stacks (pgxpool for the app, `*sql.DB` for whatsmeow) and **two** migration systems (goose for
PerGo tables, `Container.Upgrade(ctx)` for whatsmeow's `whatsmeow_*` tables) against one
database. This is fine — they own non-overlapping schemas — but it must be **explicit** in
`platform/postgres/` and in the boot sequence. Also: set `whatsmeow.PostgresArrayWrapper =
pq.Array` (or the pgx/stdlib equivalent) if using `lib/pq`. *(Official whatsmeow sqlstore
godoc, HIGH confidence.)*

### GAP 5 — Startup reconnect-storm protection (HIGH impact, LOW effort)
`Container.GetAllDevices()` on boot returns every paired device. Naively `go connect()` for
each can reconnect hundreds of WebSocket sessions simultaneously, triggering WhatsApp
rate-limiting/bans. The reference production impl calls this out explicitly ("Startup
Reconnect Storm Protection: concurrency limit + jitter + retry/backoff for 100s of sessions").
**Action:** `session/manager` must gate startup reconnection with a semaphore (e.g.
`chan struct{}` of capacity 8) and jittered delay per device. *(Reference impl, MEDIUM
confidence — corroborated by whatsmeow's per-account fragility.)*

### GAP 6 — WhatsApp Web version auto-refresh (MEDIUM impact, LOW effort)
whatsmeow bundles a WhatsApp Web client version that goes stale; pairing then fails with
"client outdated." The reference impl ships "WA Web Version Auto-Refresh" using `SetWAVersion`
+ a version fetcher. **Action:** `platform/` (or `channel/whatsappweb`) should fetch the
current WA Web version on startup and on "client outdated" errors, then retry. *(Reference
impl, MEDIUM confidence.)*

### GAP 7 — WorkQueue single-consumer topology must be documented (LOW impact, documentation)
The worker code's `PullSubscribe("", consumer, ...)` (empty filter) is valid only as a single
consumer. A future contributor adding a per-channel consumer on the same `WorkQueuePolicy`
stream will hit "overlapping consumers" errors. **Action:** document topology (A) (single
consumer + in-process `RoutingEngine`) as the intentional choice in the worker package and
ADR. *(Official NATS docs, HIGH confidence.)*

### GAP 8 — `CopyFrom` is connection-level, not pool-level (LOW impact, implementation detail)
`pgx.Conn.CopyFrom` / `pgx.Tx.CopyFrom` exist; `*pgxpool.Pool` has no `CopyFrom`. The audit
batch writer must acquire a connection per batch. **Action:** implement
`audit_repo.CopyFrom(ctx, rows)` as `pool.Acquire(ctx)` → `conn.CopyFrom(...)`. *(Official pgx
v5 godoc, HIGH confidence.)*

### GAP 9 — `GetDevice` requires an AD-JID (LOW impact, implementation detail)
whatsmeow `Container.GetDevice(jid)` notes "the parameter usually must be an AD-JID"
(agent-owned device JID). The session manager must resolve/normalize JIDs to AD form before
lookup or device retrieval silently returns nil. *(Official whatsmeow sqlstore godoc, HIGH
confidence.)*

### GAP 10 — Idempotency at the provider boundary (MEDIUM impact, design decision)
Even with `Nats-Msg-Id` dedup at publish, a worker crash *after* the provider accepted a
message but *before* `msg.Ack()` causes JetStream redelivery → a second send to a human. The
PRD flags this but prescribes no mechanism. **Action:** for unofficial WhatsApp Web, accept
the small redelivery risk (rare, and WhatsApp dedups obvious replays within a short window);
for WABA, rely on Meta's message ID dedup; document the residual risk. A full
`trace_id`-keyed dedup table is over-engineering for MVP — revisit if a compliance
requirement demands it.

---

## Scaling Considerations

| Scale | Architecture adjustment |
|-------|--------------------------|
| ≤500 req/s, single node (MVP target) | **Single binary, single NATS, single PG.** No change — the prescribed architecture is correctly sized. |
| 1k–5k req/s | Add worker *goroutines* (cheap) and/or a second PerGo process joining the NATS queue group (topology A scales within one process; for multi-process, mirror the work-queue stream to a `LimitsPolicy` stream with a push queue-group consumer). Vertical-scale PostgreSQL first. |
| 10k+ req/s | Split `audit_logs` writes to a dedicated PG instance; introduce PgBouncer (then disable pgx prepared statements via `QueryExecMode`); consider Redis for API-key cache only if measurement shows hot path. |

### Scaling priorities (in order of what breaks first)

1. **Per-session WhatsApp Web throughput** — a single session is capped at ~0.3–1 msg/s *by
   design* (staggered). Throughput scales by **adding paired sessions**, not by speeding one
   up. This is a product/ops constraint, not a code bottleneck.
2. **Audit write contention** — already mitigated by the batched `CopyFrom`. The drop counter
   is the canary; if it increments, raise buffer cap or writer count.
3. **PostgreSQL connection saturation** — `pgxpool.MaxConns` sized to `2*CPU`; whatsmeow's
   `*sql.DB` is a second pool — set its `SetMaxOpenConns` deliberately, not the default.
4. **NATS stream depth** — `MaxMsgsPerSubject` enforces the 1000 cap; beyond that, scale
   consumers (see above).

---

## Anti-Patterns to Avoid

| Anti-pattern | Why it's wrong | Do this instead |
|--------------|----------------|-----------------|
| Parallel `errgroup` fallback dispatch | Sends the message N times to a human | Sequential `for` loop in `RoutingEngine` |
| Synchronous audit INSERT on the hot path | Violates the 50ms p99 budget | Non-blocking `Record` → bounded buffer → batch `CopyFrom` |
| Encrypting whatsmeow's internal signal-protocol rows | whatsmeow reads/writes its own blob columns directly via `*sql.DB`; AES-GCM wrapping breaks it | Encrypt only PerGo's *metadata* (workspace mapping, pairing tokens). whatsmeow's signal blobs are already opaque. |
| `sync.Map` for the session registry | Loses type safety; `RWMutex` read-heavy fast path is faster for well-known JID keys | `sync.RWMutex` + `map[JID]*Session` |
| Layering provider retries on top of JetStream retries | Doubles the delivery attempt count → sends to a human twice | `Dispatch` returns nil only on provider accept; transient errors → JetStream nak redelivers; terminal errors → advance fallback |
| A second durable consumer on the same `WorkQueuePolicy` stream | JetStream rejects overlapping consumers | Single consumer + in-process `RoutingEngine` (topology A) |
| `go connect()` for all devices on startup | Reconnect storm → WhatsApp bans | Semaphore (cap ~8) + jittered backoff |
| `context.Background()` on the request path | Breaks cancellation cascade and trace propagation | Derive every child from the request `ctx` |

---

## Integration Points

### External services

| Service | Integration pattern | Gotchas |
|---------|---------------------|---------|
| WhatsApp Web (whatsmeow) | WebSocket, one `*whatsmeow.Client` per paired device, `sqlstore.Container` over `*sql.DB` (postgres) | AD-JID for `GetDevice`; WA version staleness; reconnect-storm; per-session rate limit; unofficial protocol may break |
| WhatsApp Cloud (WABA) | REST/HTTPS, `net/http.Client{Timeout:10s}` behind `platform/breaker` | Template session windows expire (terminal error → fallback); Meta rate limits → breaker open |
| Telegram Bot | REST/HTTPS, `net/http.Client{Timeout:10s}` behind `platform/breaker` | Bot API rate limits per chat |
| NATS JetStream | `nats.go` client; `WorkQueuePolicy` messages stream + `LimitsPolicy` webhooks stream | Single consumer per subject; no auto-DLQ; `Backoff` overrides `AckWait`; `Nats-Msg-Id` for dedup |
| PostgreSQL | `pgx/v5` pool (app) + `*sql.DB` (whatsmeow); `pgx.CopyFrom` for audit | Two pools, two migrations; `CopyFrom` is conn-level; prepared stmts break under PgBouncer |
| Consumer webhooks | Outbound HTTPS POST, dedicated JetStream consumer, exp backoff, DLQ | Per-consumer 4xx → NAK to MaxDeliver → DLQ |

### Internal boundaries

| Boundary | Communication | Notes |
|----------|---------------|-------|
| ingest ↔ queue | in-process call (`Queue.Publish`) | Trace-ID in `context` + NATS header |
| queue ↔ worker | NATS JetStream (durable) | The durability boundary; at-least-once |
| worker ↔ channel | `channel.Dispatcher` interface (in-process) | Plugin boundary; unofficial breakage isolated here |
| channel ↔ session | `session.Registry.Get(jid)` (in-process) | Only `whatsappweb` adapter; RWMutex map |
| worker ↔ audit | `audit.Sink.Record` (in-process, non-blocking) | Bounded buffer; never blocks the 50ms path |
| audit ↔ postgres | `pgx.CopyFrom` (batched) | Conn-level; one batch per 500 events or 50ms |
| webhook worker ↔ consumer | outbound HTTPS | Separate JetStream stream; DLQ on permanent failure |

---

## Sources

- **NATS JetStream — Streams** (official docs, `docs.nats.io/nats-concepts/jetstream/streams`) — HIGH confidence. WorkQueuePolicy single-consumer-per-subject, MaxMsgsPerSubject, DiscardNewPerSubject, retention/discard semantics.
- **NATS JetStream — Consumers** (official docs) — HIGH confidence. Pull consumers recommended, AckExplicit-only for pull, MaxAckPending flow control, MaxDeliver, Backoff-overrides-AckWait, NAK with delay.
- **NATS JetStream — Model Deep Dive** (official docs) — HIGH confidence. `Nats-Msg-Id` dedup + DuplicateWindow (exactly-once publish), ack types (+ACK/-NAK/+TERM/+WPI/+NXT), MaxDeliver messages stay in stream (no auto-DLQ).
- **whatsmeow — store package** (pkg.go.dev `go.mau.fi/whatsmeow/store`) — HIGH confidence. `Device` struct, `DeviceContainer` interface, signal-protocol sub-stores.
- **whatsmeow — store/sqlstore package** (pkg.go.dev) — HIGH confidence. `Container.New/NewWithDB` (sqlite3+postgres dialects, `*sql.DB` interface), `Upgrade`, `GetDevice` (AD-JID), `GetAllDevices`, `PostgresArrayWrapper`.
- **pgx v5** (pkg.go.dev `github.com/jackc/pgx/v5`) — HIGH confidence. `Conn.CopyFrom` (COPY protocol, conn-level not pool-level), `CopyFromRows/Slice/Func`, `CollectRows/ForEachRow`, Go 1.25+/PG14+ on v5.10, prepared-stmt/PgBouncer caveat.
- **gdbrns/go-whatsapp-multi-session-rest-api** (GitHub reference impl) — MEDIUM confidence. Production whatsmeow+PostgreSQL patterns: startup reconnect-storm protection, WA Web version auto-refresh, 86 webhook event types with retries/quotas, JWT-vs-API-key scoping divergence.

---
*Architecture research for: self-hosted omnichannel CPaaS in Go*
*Researched: 2026-06-25*

