# 4. Concurrency & Performance Strategy

## Load envelope (the budget we design against)

- 500 req/s sustained, ingest p99 <= 50ms, <512MB RAM, 2 vCPU.
- Per-request hot path: TLS terminate → JSON decode → API-key auth
  (cached) → validate → JetStream publish → `202`. **Two I/O ops max.**
- Workers: 1–3s staggered send per WhatsApp Web session → a single
  session is throughput-limited to ~0.3–1 msg/s by design. To hit 500
  msg/s we need **many concurrent sessions**, not faster sessions.

## Where goroutines & channels live

| Location | Primitive | Lifetime | Purpose |
|----------|-----------|----------|---------|
| `messaging/ingest` | Echo handler goroutine (1 per request) | request-scoped | validate + publish, return 202 |
| `messaging/worker` | N pull-consumer goroutines (configurable, default `runtime.NumCPU()*2`) | process lifetime | fetch from JetStream, hand to `RoutingEngine`, `Ack()` on success |
| `session/manager` | 1 goroutine per linked WhatsApp Web device | session-scoped | runs `whatsmeow.Client.Connect()` + event loop |
| `session/registry` | `sync.RWMutex` + `map[JID]*Session` | process lifetime | thread-safe lookup, no goroutine of its own |
| `audit/buffer` | 1 bounded `chan AuditEvent` + M batch-writer goroutines | process lifetime | decouple audit writes from hot path |
| `webhook/worker` | K pull-consumer goroutines on a `webhooks` JetStream stream | process lifetime | durable outbound webhook delivery |

## Patterns applied

### Fan-out at the worker pool, not at the request
A single `POST /api/v1/messages` is **one** JetStream publish. Parallelism
happens at the consumer side: N worker goroutines pull from the same
queue group, so throughput scales by adding workers (or processes).
There is no per-request `errgroup` — that would be premature fan-out
for a single-channel dispatch.

### Pipeline: ingest → broker → worker → channel → audit
Each stage communicates through a **durable boundary** (JetStream) or a
**bounded channel** (audit), never through unbuffered hand-offs. This
makes backpressure explicit:

```
HTTP ──publish──► JetStream (durable) ──pull──► worker goroutine
                                                       │
                                                       ▼
                                                  Dispatcher
                                                       │
                                              ┌────────┴────────┐
                                              ▼                 ▼
                                          provider         audit chan (cap 5000)
                                                              │
                                                              ▼
                                                         batch writer
                                                              │
                                                              ▼
                                                          PostgreSQL
```

### Per-session rate limiter (staggered dispatch)
Each `Session` owns a `*rate.Limiter` sized to the unofficial-channel
policy (`rate.Every(1..3s jittered), burst 1`). `limiter.Wait(ctx)`
**blocks the worker goroutine** but releases the P to the scheduler, so
thousands of independent session queues progress concurrently on 2
vCPUs. The limiter is created in `session.New`, stored on the struct,
and read by the adapter — no global limiter, no shared mutex.

### Audit batch writer (classic bounded-buffer fan-in)
```
buf := make(chan audit.Event, 5000)   // 5000 * ~256B ≈ 1.3MB ceiling
for i := 0; i < M; i++ { go batchWriter(buf, db) }
```
Each writer drains up to `N` events (or a 50ms timer) and issues a
single `pgx.CopyFrom` / batch `INSERT`. This collapses 500 inserts/s
into ~10 batched writes/s — the single most important DB-protecting
pattern in the system.

## Preventing races & leaks

| Risk | Mitigation |
|------|------------|
| **Map race in `session.Registry`** | `sync.RWMutex`; `Get` takes RLock, `Put/Remove` take Lock. Never hold the lock across a network call. |
| **Goroutine leak on session drop** | Each session goroutine selects on `<-ctx.Done()` and `whatsmeow` disconnect; `manager.Stop(jid)` cancels the context and `wg.Wait()`s on the per-session goroutine before removing from the registry. |
| **Worker leak on shutdown** | Pull subscription uses `nats.PullOpts{MaxWaiting:...}`; `main` cancels a root context, workers stop fetching, finish in-flight `Ack`, then return. |
| **Audit channel full → blocked hot path** | `Record` does a **non-blocking** `select { case buf <- e: default: drop + slog.Error + expvar counter }`. Audit is best-effort on the hot path; a dropped event is loud and counted, never a 50ms stall. (Compliance SLO is tracked via the counter; if it ever increments, the buffer/writer count is tuned.) |
| **Pprof goroutine cost** | `net/http/pprof` mounted on a **separate listener** (`localhost:6060`), not on the public Echo mux. |
| `context` cancellation ignored | Every `Dispatch`, `Publish`, `INSERT` takes `ctx` as first arg and respects cancellation. `errgroup` is *not* used; we pass ctx explicitly. |

## Performance knobs (sized from the envelope, not guessed)

| Knob | Default | Rationale |
|------|---------|-----------|
| JetStream worker goroutines | `2 * runtime.NumCPU()` | 2 vCPU → 4 workers; each spends most time in `rate.Wait` or network I/O, so >CPU count is correct. |
| Audit buffer cap | 5000 | ~1.3MB worst case; sized above the 500 req/s × 1s drain rate. |
| Audit batch writers | 2 | One CopyFrom at a time per writer; 2 keeps the pipe full without contending. |
| Audit batch size / flush | 500 events or 50ms | Bounds latency of an audit row to <=50ms while amortising round-trips. |
| JetStream `MaxDeliver` | 5 | At-least-once retry ceiling before DLQ. |
| `pgxpool.MaxConns` | `2 * CPU` + queue | Small pool forces reuse; large pools add latency and memory. |
| Session limiter | `rate.Every(2s)` ± jitter, burst 1 | Matches PRD "1–3s staggered". |

## What we are *not* doing

- No `errgroup.WithContext` for the fallback pipeline: channels are
  tried **sequentially** by design (fallback is ordered, not parallel).
  Parallel dispatch would send the message N times.
- No `sync.Map`: the session registry has well-known keys (JIDs) and
  benefits from `RWMutex`'s read-heavy fast path with typed values.
- No worker-pool framework: `for i := range N { go loop() }` is the pool.
- No shared global `rate.Limiter` for the API: API-level rate limiting
  is an Echo middleware concern (token bucket per API key), not part of
  the dispatch concurrency model.
