# 5. Resilience & Error Handling

## Error wrapping convention

All errors are wrapped at the package boundary with the operation name
and propagated up via `fmt.Errorf`. Callers use `errors.Is` / `errors.As`
against a small, explicitly exported set of sentinel errors.

```go
// internal/messaging/errors.go
package messaging

import "errors"

var (
    ErrQueueFull       = errors.New("queue full: backpressure")
    ErrNoChannel       = errors.New("no channel available")
    ErrAllFallbackFail = errors.New("delivery failed on all fallback channels")
    ErrInvalidPayload  = errors.New("invalid payload")
)
```

At an I/O boundary:

```go
if err := js.Publish(ctx, subject, b, nats.Header{trace.Hdr: tid}); err != nil {
    return fmt.Errorf("jetstream publish %s: %w", subject, err)
}
```

**Rules:**
- `%w` for the immediate cause; never `%v` for a wrapped error we want
  unwrappable.
- One wrap per layer; do not re-wrap an already-wrapped error with the
  same context.
- `errors.Is(err, messaging.ErrQueueFull)` decides the HTTP status
  (`429`), not string matching.
- No `errors.New` for flow control across packages — only sentinels.

## HTTP status mapping

| Error | Status |
|-------|--------|
| `ErrInvalidPayload` / decode error | `400` |
| Auth failure | `401` |
| Workspace mismatch | `403` |
| `ErrQueueFull` (per-session depth > 1000) | `429` + `Retry-After: 5` |
| Queue service unavailable | `503` + `Retry-After` |
| Successful enqueue | `202` + `Trace-Id` header + `{ "trace_id": ... }` |
| Successful dispatch (sync channels, if ever) | `200` |

## Timeouts (every I/O call has one)

| Call | Timeout | Mechanism |
|------|---------|-----------|
| JSON decode (Echo) | 2s | `http.Server.ReadTimeout` |
| JetStream publish (ingest) | 1s | `nats.PublishAsyncMaxWait` / ctx |
| JetStream pull fetch | 5s | `PullMaxWait` |
| Provider REST (WABA, Telegram) | 10s | `http.Client{Timeout: 10s}` per client |
| whatsmeow send | parent ctx, no extra | whatsmeow honours ctx |
| Webhook outbound POST | 5s | dedicated `http.Client` |
| pgx query (auth, audit) | 2s | `ctx, cancel := context.WithTimeout(...)` per call |
| Graceful shutdown | 30s | `main` root ctx; workers drain in-flight |

No `context.Background()` on the request path except the root in `main`.
Every child derives from the request `ctx` so cancellation cascades.

## Retries

- **JetStream** handles *transport-level* retry: `MaxDeliver: 5` with
  exponential backoff via `nats.Consumer` `AckWait` and `MaxBackoff`.
  A worker that fails `Dispatch` does **`nak`** (negative ack with
  delay) instead of `Ack`, returning the message to the queue.
- **Provider-level** retries are **not** layered on top of JetStream
  retries by default — that doubles the delivery attempt count and
  risks sending a message to a human twice. The contract is:
  - `Dispatch` returns `nil` only when the provider accepted the
    message. A transient HTTP 5xx from WABA returns an error → JetStream
    redelivers.
  - `Dispatch` returns a **terminal error** (e.g. `ErrTemplateWindowExpired`)
    → the routing engine catches it and advances to the next fallback
    channel *without* `nak`-ing. Terminal errors are typed so the worker
    can branch:
    ```go
    var term ErrTerminal
    if errors.As(err, &term) { /* advance fallback, ack original */ }
    ```
- **Webhook delivery** has its own JetStream stream with `MaxDeliver:
  10` and exponential `AckWait` (1s, 5s, 30s, 2m, 10m…). A consumer
  that 4xx's permanently is NAK'd until `MaxDeliver`, then moved to a
  `webhooks_dlq` stream and surfaced on the admin console.

## Circuit breakers

Used **per provider REST channel** (WABA, Telegram), not per JetStream
consumer. A breaker around `whatsappweb` is pointless — the WebSocket
either is connected or isn't, which the session manager already tracks.

```go
// internal/platform/breaker
type Breaker struct {
    mu          sync.Mutex
    failureTol  int        // e.g. 10
    openTime    time.Duration // e.g. 30s
    failures    int
    state       state // closed | open | halfOpen
    openedAt    time.Time
}

func (b *Breaker) Do(ctx context.Context, fn func(context.Context) error) error
```

- **closed** → call `fn`; on error, `failures++`; at threshold → **open**.
- **open** → return `breaker.ErrOpen` immediately for `openTime`; the
  routing engine treats `ErrOpen` as a fallback trigger (advance to
  next channel), not a retry trigger.
- **half-open** → allow one probe; success → closed, failure → open.

This is ~60 LoC and covers the requirement. `sony/gobreaker` is a
drop-in upgrade if we later need sliding windows or metrics hooks.

## Backpressure (concrete)

`messaging/ingest` reads the target session's pending count **before**
publish:

```go
info, err := js.StreamInfo(ctx, streamName,
    nats.MaxSubjects(nats.SubjectForSession(jid)))
pending := info.State.Msgs
if pending > 1000 { return messaging.ErrQueueFull }
```

If `StreamInfo` itself is slow (>5ms), fall back to an in-memory
`atomic.Int64` counter per session, incremented on publish and
decremented on worker `Ack`. Cheaper, eventually consistent with the
broker — acceptable because the 1000 limit has slack. **Default to the
in-memory counter**; use `StreamInfo` only as a periodic reconciler
(every 10s per session) to correct drift.

## Graceful shutdown

```
SIGTERM
  → root ctx cancel
  → Echo.Shutdown(ctx)         (stop accepting, finish in-flight reads)
  → workers stop pulling       (drain in-flight Dispatch, Ack or Nak)
  → session manager stops      (cancel per-session ctx, wg.Wait)
  → audit buffer flush         (close chan, writers drain remaining)
  → pgxpool / nats close
```

A 30s ceiling bounds the whole sequence. In-flight JetStream messages
not `Ack`'d are redelivered after `AckWait` — that is the durability
guarantee; we do **not** attempt to flush the broker.

## Observability (resilience-relevant)

- `expvar` counters: `ingest_total`, `ingest_429`, `audit_dropped`,
  `dispatch_fail_total{channel}`, `breaker_open_total{channel}`,
  `session_active`, `jetstream_redeliver_total`.
- `slog` with trace_id field on every log line; level configurable via env.
- `net/http/pprof` on `localhost:6060` for the 90-day leak audit.
- `/healthz` (liveness: process up) and `/readyz` (pgx ping + nats
  ping) for orchestrator probes.
