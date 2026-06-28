# 6. Core Code Example

Illustrative snippets — not the full implementation, but the load-bearing
shapes: trace propagation, ingest handler, the routing/fallback engine,
the per-session worker with a rate limiter, and the audit batch writer.
All names are short, errors wrap, `context.Context` is the first arg.

## 6.1 Trace-ID propagation (`internal/platform/trace`)

```go
package trace

import (
    "context"

    "github.com/gofrs/uuid/v5"
)

type ctxKey struct{}

const Header = "Trace-Id"

func New() string {
    id, _ := uuid.NewV4()
    return id.String()
}

func With(ctx context.Context, id string) context.Context {
    return context.WithValue(ctx, ctxKey{}, id)
}

func From(ctx context.Context) string {
    if id, ok := ctx.Value(ctxKey{}).(string); ok {
        return id
    }
    return ""
}
```

## 6.2 Ingest handler (`internal/messaging/ingest.go`)

Hot path = two I/O calls: cached auth (middleware) + JetStream publish.

```go
package messaging

import (
    "errors"
    "net/http"

    "github.com/labstack/echo/v4"

    "pergo/internal/platform/trace"
)

type IngestHandler struct {
    queue   *Queue
    backpr  *Backpressure
}

func NewIngestHandler(q *Queue, b *Backpressure) *IngestHandler {
    return &IngestHandler{queue: q, backpr: b}
}

func (h *IngestHandler) Post(c echo.Context) error {
    ctx := c.Request().Context()

    var msg MessagePayload
    if err := c.Bind(&msg); err != nil {
        return echo.NewHTTPError(http.StatusBadRequest, ErrInvalidPayload.Error())
    }
    if err := msg.Validate(); err != nil {
        return echo.NewHTTPError(http.StatusBadRequest, err.Error())
    }

    // Backpressure: per-session pending depth.
    if h.backpr.Reject(msg.WorkspaceID, msg.PrimarySession()) {
        c.Response().Header().Set("Retry-After", "5")
        return echo.NewHTTPError(http.StatusTooManyRequests, ErrQueueFull.Error())
    }

    tid := trace.New()
    ctx = trace.With(ctx, tid)
    msg.TraceID = tid

    if err := h.queue.Publish(ctx, &msg); err != nil {
        if errors.Is(err, ErrQueueFull) {
            c.Response().Header().Set("Retry-After", "5")
            return echo.NewHTTPError(http.StatusTooManyRequests)
        }
        return echo.NewHTTPError(http.StatusServiceUnavailable, err.Error())
    }

    c.Response().Header().Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
    return c.JSON(http.StatusAccepted, echo.Map{"trace_id": tid})
}
```

## 6.3 JetStream publish with Trace-ID header (`internal/messaging/queue.go`)

```go
package messaging

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "github.com/nats-io/nats.go"
    "github.com/nats-io/nats.go/jetstream"

    "pergo/internal/platform/trace"
)

type Queue struct {
    js  jetstream.JetStream
    cfg QueueConfig
}

type QueueConfig struct {
    Stream      string
    SubjectFmt  string // e.g. "messages.%s.%s" (workspace, channel)
    PubTimeout  time.Duration
}

func NewQueue(js jetstream.JetStream, cfg QueueConfig) *Queue {
    return &Queue{js: js, cfg: cfg}
}

func (q *Queue) Publish(ctx context.Context, m *MessagePayload) error {
    subject := fmt.Sprintf(q.cfg.SubjectFmt, m.WorkspaceID, m.PrimaryChannel())

    b, err := json.Marshal(m)
    if err != nil {
        return fmt.Errorf("marshal message: %w", err)
    }

    ctx, cancel := context.WithTimeout(ctx, q.cfg.PubTimeout)
    defer cancel()

    hdr := nats.Header{}
    hdr.Set(trace.Header, m.TraceID)
    hdr.Set("Workspace", m.WorkspaceID)

    _, err = q.js.Publish(ctx, subject, b, jetstream.WithHeaders(hdr))
    if err != nil {
        return fmt.Errorf("jetstream publish %s: %w", subject, err)
    }
    return nil
}
```

## 6.4 Channel dispatcher interface — earned (3 real impls)

Defined **consumer-side**, in the channel package, not in `messaging`.

```go
// internal/channel/dispatcher.go
package channel

import "context"

import "pergo/internal/messaging"

// Dispatcher is implemented by whatsappweb, whatsappcloud, and telegram.
type Dispatcher interface {
    Dispatch(ctx context.Context, m *messaging.MessagePayload) error
}

// Terminal marks an error as non-retryable (e.g. template window expired,
// invalid recipient). The routing engine uses errors.As to advance
// fallback instead of redelivering.
type Terminal interface {
    Terminal() bool
}
```

```go
// internal/channel/registry.go
package channel

type Registry struct {
    dispatchers map[string]Dispatcher
}

func NewRegistry(ds map[string]Dispatcher) *Registry {
    return &Registry{dispatchers: ds}
}

func (r *Registry) Get(name string) (Dispatcher, bool) {
    d, ok := r.dispatchers[name]
    return d, ok
}
```

## 6.5 Routing engine — sequential fallback pipeline

Sequential by design: parallel dispatch would send the message N times.

```go
// internal/messaging/routing.go
package messaging

import (
    "context"
    "errors"
    "fmt"

    "pergo/internal/audit"
    "pergo/internal/channel"
)

type RoutingEngine struct {
    channels *channel.Registry
    audit    audit.Sink
}

func NewRoutingEngine(r *channel.Registry, a audit.Sink) *RoutingEngine {
    return &RoutingEngine{channels: r, audit: a}
}

func (e *RoutingEngine) ResolveDelivery(ctx context.Context, m *MessagePayload) error {
    var lastErr error
    attempted := append([]string{m.PrimaryChannel()}, m.FallbackChannels...)

    for _, ch := range attempted {
        d, ok := e.channels.Get(ch)
        if !ok {
            lastErr = fmt.Errorf("channel %q: %w", ch, ErrNoChannel)
            e.record(ctx, m, ch, StatusSkipped, lastErr)
            continue
        }

        err := d.Dispatch(ctx, m)
        if err == nil {
            e.record(ctx, m, ch, StatusSent, nil)
            return nil
        }

        lastErr = fmt.Errorf("channel %q dispatch: %w", ch, err)
        e.record(ctx, m, ch, StatusFailed, err)

        // Terminal error -> do not redeliver via JetStream; advance now.
        // Non-terminal: still try the next fallback; redelivery of the
        // *original* message is governed by the worker's ack/nak in 6.6.
        var term channel.Terminal
        if errors.As(err, &term) && term.Terminal() {
            continue
        }
    }
    return fmt.Errorf("%w: last=%v", ErrAllFallbackFail, lastErr)
}

func (e *RoutingEngine) record(ctx context.Context, m *MessagePayload, ch string, st Status, err error) {
    ev := audit.Event{
        TraceID:     m.TraceID,
        WorkspaceID: m.WorkspaceID,
        Channel:     ch,
        Status:      string(st),
    }
    if err != nil {
        ev.Error = err.Error()
    }
    _ = e.audit.Record(ctx, ev) // non-blocking; see 6.7
}
```

## 6.6 Worker loop — JetStream pull consumer

```go
// internal/messaging/worker.go
package messaging

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "log/slog"
    "runtime"
    "time"

    "github.com/nats-io/nats.go/jetstream"

    "pergo/internal/platform/trace"
)

type WorkerPool struct {
    js       jetstream.JetStream
    consumer string
    route    *RoutingEngine
    log      *slog.Logger
    n        int
}

func NewWorkerPool(js jetstream.JetStream, consumer string, r *RoutingEngine, log *slog.Logger) *WorkerPool {
    return &WorkerPool{
        js: js, consumer: consumer, route: r, log: log,
        n: runtime.NumCPU() * 2,
    }
}

func (p *WorkerPool) Run(ctx context.Context) error {
    c, err := p.js.PullSubscribe("", p.consumer, jetstream.PullMaxWaiting(p.n))
    if err != nil {
        return fmt.Errorf("pull subscribe %s: %w", p.consumer, err)
    }

    for i := 0; i < p.n; i++ {
        go p.loop(ctx, c, i)
    }
    <-ctx.Done()
    return ctx.Err()
}

func (p *WorkerPool) loop(ctx context.Context, c jetstream.Consumer, id int) {
    log := p.log.With("worker", id)
    for {
        if ctx.Err() != nil {
            return
        }
        batch, err := c.Fetch(10, jetstream.FetchMaxWait(5*time.Second))
        if err != nil {
            if errors.Is(err, jetstream.ErrTimeout) {
                continue
            }
            log.Error("fetch", "err", err)
            continue
        }
        for msg := range batch.Messages() {
            p.handle(ctx, msg, log)
        }
    }
}

func (p *WorkerPool) handle(ctx context.Context, msg jetstream.Msg, log *slog.Logger) {
    tid := msg.Headers().Get(trace.Header)
    ctx = trace.With(ctx, tid)

    var m MessagePayload
    if err := json.Unmarshal(msg.Data(), &m); err != nil {
        log.Error("unmarshal", "trace_id", tid, "err", err)
        _ = msg.Term() // poison message; do not redeliver forever
        return
    }
    m.TraceID = tid

    if err := p.route.ResolveDelivery(ctx, &m); err != nil {
        log.Error("resolve", "trace_id", tid, "err", err)
        // Non-terminal failure -> NAK with delay for JetStream redelivery.
        _ = msg.Nak(jetstream.NakDelay(2 * time.Second))
        return
    }
    _ = msg.Ack()
}
```

## 6.7 Audit batch writer — bounded buffer, non-blocking on hot path

```go
// internal/audit/buffer.go
package audit

import (
    "context"
    "errors"
    "expvar"
    "log/slog"
    "time"
)

var droppedTotal expvar.Int

var ErrBufferFull = errors.New("audit buffer full")

type Buffer struct {
    ch     chan Event
    sink   PGSink
    log    *slog.Logger
    batchN int
    flush  time.Duration
}

func NewBuffer(sink PGSink, log *slog.Logger, cap, writers, batchN int, flush time.Duration) *Buffer {
    b := &Buffer{
        ch:     make(chan Event, cap),
        sink:   sink,
        log:    log,
        batchN: batchN,
        flush:  flush,
    }
    for i := 0; i < writers; i++ {
        go b.run()
    }
    return b
}

// Record is non-blocking: a full buffer drops + counts, never stalls
// the ingest/dispatch hot path.
func (b *Buffer) Record(ctx context.Context, e Event) error {
    select {
    case b.ch <- e:
        return nil
    default:
        droppedTotal.Add(1)
        b.log.Error("audit buffer full, dropping event", "trace_id", e.TraceID)
        return ErrBufferFull
    }
}

func (b *Buffer) run() {
    batch := make([]Event, 0, b.batchN)
    t := time.NewTimer(b.flush)
    defer t.Stop()

    flush := func() {
        if len(batch) == 0 {
            return
        }
        ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
        if err := b.sink.CopyFrom(ctx, batch); err != nil {
            b.log.Error("audit batch write", "n", len(batch), "err", err)
        }
        cancel()
        batch = batch[:0]
    }

    for {
        select {
        case e, ok := <-b.ch:
            if !ok {
                flush()
                return
            }
            batch = append(batch, e)
            if len(batch) >= b.batchN {
                flush()
                if !t.Stop() { <-t.C }
                t.Reset(b.flush)
            }
        case <-t.C:
            flush()
            t.Reset(b.flush)
        }
    }
}

func (b *Buffer) Close() {
    close(b.ch)
}
```

`PGSink.CopyFrom` uses `pgx.CopyFrom` on `audit_logs` — the fastest
append path in pgx, ideal for an append-only partitioned table.

## 6.8 Per-session worker with rate limiter (`internal/session`)

The part that makes "thousands of queues on 2 vCPUs" viable:
`limiter.Wait(ctx)` blocks the goroutine but yields the P.

```go
// internal/session/session.go
package session

import (
    "context"
    "sync"
    "time"

    "github.com/tulir/whatsmeow"
    "golang.org/x/time/rate"
)

type Session struct {
    JID      string
    client   *whatsmeow.Client
    limiter  *rate.Limiter
    cancel   context.CancelFunc
    done     chan struct{}
}

// Staggered limiter: 1 token every 1–3s, burst 1. The jitter is applied
// per-acquire by re-seeding the interval on each Wait via a small wrapper;
// a fixed limiter is shown for clarity.
func newSession(c *whatsmeow.Client, every time.Duration) *Session {
    return &Session{
        client:  c,
        limiter: rate.NewLimiter(rate.Every(every), 1),
        done:    make(chan struct{}),
    }
}

func (s *Session) Run(parent context.Context) {
    ctx, cancel := context.WithCancel(parent)
    s.cancel = cancel
    defer close(s.done)

    if err := s.client.Connect(); err != nil {
        return
    }
    <-ctx.Done()
    _ = s.client.Disconnect()
}

func (s *Session) Wait(ctx context.Context) error {
    return s.limiter.Wait(ctx)
}

func (s *Session) Stop() {
    s.cancel()
    <-s.done
}
```

```go
// internal/session/registry.go
package session

import "sync"

type Registry struct {
    mu    sync.RWMutex
    alive map[string]*Session
}

func NewRegistry() *Registry {
    return &Registry{alive: make(map[string]*Session)}
}

func (r *Registry) Get(jid string) (*Session, bool) {
    r.mu.RLock()
    s, ok := r.alive[jid]
    r.mu.RUnlock()
    return s, ok
}

func (r *Registry) Put(s *Session) {
    r.mu.Lock()
    r.alive[s.JID] = s
    r.mu.Unlock()
}

func (r *Registry) Remove(jid string) {
    r.mu.Lock()
    delete(r.alive, jid)
    r.mu.Unlock()
}
```

The `whatsappweb` adapter calls `s.Wait(ctx)` immediately before
`client.SendMessage`, gating the send on the session's limiter without
any global lock.

---

These six shapes — trace-aware context, two-I/O ingest, sequential
fallback routing, pull-consumer worker pool, non-blocking audit buffer,
per-session rate-limited goroutine — are the structural backbone. Every
other file in the tree is wiring around them.
