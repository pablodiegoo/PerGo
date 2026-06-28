# Phase 3: Ingest API & Queue - Research

**Researched:** 2026-06-25
**Domain:** HTTP API design, message queue architecture, backpressure, rate limiting
**Confidence:** HIGH

## Summary

Phase 3 builds the first half of the send path: a unified `POST /messages` endpoint that validates payloads, enqueues messages durably into NATS JetStream, and returns formal status/error contracts. The existing codebase (Phase 1) provides Echo v5 server, pgxpool, auth middleware, trace middleware, and audit batch writer — all ready to integrate.

The JetStream WorkQueuePolicy is the natural fit for the durable work-queue requirement: it enforces FIFO, single-consumer-per-subject semantics with automatic message deletion on Ack. Backpressure is achieved via `MaxMsgs` + `DiscardNew` on the stream config, which causes the JetStream publish call to return an error when the queue is full — translating directly to HTTP 429/422 responses. Rate limiting uses the existing `golang.org/x/time/rate` dependency (already in go.mod as indirect) with one `Limiter` per workspace in a `sync.Map`.

**Primary recommendation:** Use the `jetstream` package (not the legacy `nats` JetStream API) for stream creation, publishing, and consumer management. The `jetstream` package is the recommended modern API with clearer interfaces and better pull-consumer support.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Payload validation | API / Backend | — | Echo handler validates before enqueue |
| Trace-ID generation | Middleware (existing) | API / Backend | Trace middleware already generates UUID |
| Message status enum | API / Backend | — | Status is a domain concept, returned in response |
| Structured error responses | API / Backend | — | Echo handler formats errors per API-04 |
| JetStream stream provisioning | Infrastructure | API / Backend | Stream created at boot, used by handler |
| Message enqueue (publish) | API / Backend | Queue | Handler publishes to JetStream |
| Backpressure enforcement | Queue (JetStream) | API / Backend | Stream limits reject at NATS level; handler translates |
| Dedup (publish-side) | Queue (JetStream) | API / Backend | Nats-Msg-Id header on publish |
| Dedup (delivery-side) | Queue (worker) | — | dispatched_messages set checked at dispatch |
| Rate limiting | API / Backend | — | Per-session limiter checked before enqueue |
| Per-session queue depth | API / Backend | — | Tracked in-memory, checked before publish |
| TTL checking | Queue (worker) | — | Checked at dispatch time, not enqueue |
| Worker stub | Queue (worker) | — | Logs dispatched messages, implements Dispatcher interface |

## Standard Stack

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/nats-io/nats.go` | v1.52.0 | NATS client with JetStream | Already in go.mod; canonical Go NATS client |
| `github.com/nats-io/nats.go/jetstream` | (subpackage) | JetStream simplified API | Modern API recommended over legacy `nats.JetStream()` |
| `github.com/labstack/echo/v5` | v5.2.1 | HTTP router + handler framework | Already in go.mod; project standard |
| `golang.org/x/time/rate` | v0.15.0 | Token-bucket rate limiter | Already in go.mod (indirect); stdlib-adjacent |
| `github.com/google/uuid` | v1.6.0 | Trace-ID generation | Already in go.mod; used by trace middleware |

### Supporting

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| (none new) | — | — | All dependencies already present |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `jetstream` package | Legacy `nats.JetStream()` | Legacy API has convoluted Subscribe patterns; jetstream is recommended |
| `rate.NewLimiter` per-session | Fixed counter with TTL | Rate limiter handles burst smoothing naturally; counter doesn't |
| `WorkQueuePolicy` | `LimitsPolicy` + manual ack tracking | WorkQueuePolicy gives delete-on-ack for free; LimitsPolicy requires manual cleanup |

**Installation:** No new packages needed — all dependencies already in `go.mod`.

## Package Legitimacy Audit

| Package | Registry | Age | Downloads | Source Repo | Verdict | Disposition |
|---------|----------|-----|-----------|-------------|---------|-------------|
| `github.com/nats-io/nats.go` | Go module proxy | 7+ yrs | 14k+ importers | github.com/nats-io/nats.go | OK | Approved — verified via proxy.golang.org |
| `golang.org/x/time` | Go module proxy | 12+ yrs | 14k+ importers | go.googlesource.com/time | OK | Approved — verified via proxy.golang.org |
| `github.com/labstack/echo/v5` | Go module proxy | 2+ yrs | 792 importers | github.com/labstack/echo | OK | Approved — already in go.mod |
| `github.com/google/uuid` | Go module proxy | 5+ yrs | widely used | github.com/google/uuid | OK | Approved — already in go.mod |

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

## Architecture Patterns

### System Architecture Diagram

```
HTTP Client
    │
    ▼
POST /messages
    │
    ├─► Trace Middleware (generates/extracts trace_id)
    │
    ├─► Auth Middleware (validates API key, injects workspace_id)
    │
    ├─► Rate Limiter (per-workspace token bucket)
    │   └─► HTTP 429 + Retry-After if exceeded
    │
    ├─► Queue Depth Check (per-session 1,000 msg limit)
    │   └─► HTTP 429 + Retry-After if exceeded
    │
    ├─► Payload Validation
    │   └─► HTTP 400 + structured errors if invalid
    │
    ├─► TTL Check (reject if ttl_seconds = 0 or negative)
    │
    ├─► JetStream Publish (Nats-Msg-Id = trace_id)
    │   └─► HTTP 429 if DiscardNew (backpressure)
    │
    ├─► Audit Event (message.queued)
    │
    └─► HTTP 202 Accepted + X-Trace-Id header
            │
            ▼
    NATS JetStream (WorkQueuePolicy)
            │
            ▼
    Worker Stub (logs dispatched message)
            │
            ▼
    Phase 4: Real Channel Dispatcher
```

### Recommended Project Structure

```
internal/
├── api/
│   ├── handler/
│   │   └── message.go          # POST /messages handler
│   │   └── message_test.go     # Handler tests
│   └── middleware/
│       └── ratelimit.go        # Per-session rate limiting middleware
│       └── ratelimit_test.go   # Rate limiter tests
├── platform/
│   └── queue/
│       ├── jetstream.go        # JetStream stream setup + publisher
│       ├── jetstream_test.go   # Queue tests
│       ├── consumer.go         # Worker stub + Dispatcher interface
│       └── consumer_test.go    # Worker tests
├── domain/
│   └── message.go              # Message types, status enum, validation
cmd/pergo/
    └── ingest_test.go          # Integration tests
```

### Pattern 1: Echo v5 Handler with Structured Errors

**What:** Handler function that validates input, publishes to JetStream, returns structured JSON responses
**When to use:** All API endpoints in PerGo
**Example:**

```go
// Source: Echo v5 docs (pkg.go.dev/github.com/labstack/echo/v5)
func (h *MessageHandler) Create(c *echo.Context) error {
    var req CreateMessageRequest
    if err := c.Bind(&req); err != nil {
        return c.JSON(http.StatusBadRequest, ErrorResponse{
            Code:    "invalid_payload",
            Message: "request body validation failed",
            MoreInfo: "https://docs.pergo.dev/errors/invalid_payload",
            Details: []FieldError{{Field: "body", Message: "invalid JSON"}},
        })
    }
    // ... validate, publish, return 202
}
```

### Pattern 2: JetStream WorkQueue Stream Setup

**What:** Create a WorkQueuePolicy stream with backpressure limits at server boot
**When to use:** Once during server initialization
**Example:**

```go
// Source: nats-byexample.com/examples/jetstream/workqueue-stream/go + docs.nats.io
func EnsureStream(ctx context.Context, js jetstream.JetStream) (jetstream.Stream, error) {
    return js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
        Name:      "MESSAGES",
        Subjects:  []string{"messages.>"},
        Retention: jetstream.WorkQueuePolicy,
        MaxMsgs:   1000,             // backpressure limit
        Discard:   jetstream.DiscardNew,
        Storage:   jetstream.FileStorage,
        MaxAge:    24 * time.Hour,   // message expiry
    })
}
```

### Pattern 3: Per-Session Rate Limiter with sync.Map

**What:** One token-bucket limiter per workspace, stored in a concurrent map
**When to use:** Rate limiting middleware
**Example:**

```go
// Source: golang.org/x/time/rate docs (pkg.go.dev)
type RateLimiter struct {
    limiters sync.Map // workspace_id → *rate.Limiter
}

func (rl *RateLimiter) GetLimiter(workspaceID uuid.UUID) *rate.Limiter {
    if limiter, ok := rl.limiters.Load(workspaceID); ok {
        return limiter.(*rate.Limiter)
    }
    limiter := rate.NewLimiter(rate.Every(100*time.Millisecond), 10) // 10 req/s burst 10
    actual, _ := rl.limiters.LoadOrStore(workspaceID, limiter)
    return actual.(*rate.Limiter)
}
```

### Pattern 4: JetStream Publish with Idempotency

**What:** Publish message with Nats-Msg-Id header for dedup
**When to use:** Every message publish
**Example:**

```go
// Source: nats.go jetstream README (GitHub)
ack, err := js.Publish(ctx, "messages.outbound", payload,
    jetstream.WithMsgID(traceID),
)
if err != nil {
    if errors.Is(err, jetstream.ErrMaxMsgsExceeded) {
        // Backpressure: queue full
        return c.JSON(http.StatusTooManyRequests, backpressureResponse)
    }
    return err
}
```

### Anti-Patterns to Avoid

- **Using legacy `nats.JetStream()` API:** The `jetstream` package is the recommended modern API with clearer interfaces. The legacy API's `Subscribe()` patterns are convoluted for work-queue use cases.
- **Storing rate limiters in a struct field:** Use `sync.Map` for concurrent workspace-keyed access. A plain `map` would race under concurrent requests.
- **Publishing without Nats-Msg-Id:** Without the dedup header, JetStream cannot detect duplicate publishes from retried HTTP requests.
- **Checking TTL at enqueue time:** TTL must be checked at dispatch time (in the worker) because messages may sit in the queue before dispatch.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Message queue durability | In-memory channel | JetStream WorkQueuePolicy | Channels lose work on crash; JetStream persists |
| Rate limiting | Manual counter + mutex | `golang.org/x/time/rate` | Token bucket handles burst smoothing, concurrent safety |
| Payload validation | Manual field checks | Echo v5 `c.Bind()` + custom validation | Binding handles JSON parsing, type coercion, error formatting |
| Dedup tracking | Manual map + TTL | JetStream `Nats-Msg-Id` header | Server-side dedup with configurable window |
| Backpressure signaling | Custom queue with limits | JetStream `MaxMsgs` + `DiscardNew` | Server rejects at the protocol level, no custom counting |

**Key insight:** JetStream's WorkQueuePolicy + DiscardNew gives backpressure for free at the protocol level. The HTTP handler only needs to catch the publish error and translate it to an HTTP 429.

## Common Pitfalls

### Pitfall 1: WorkQueuePolicy Single-Consumer Restriction
**What goes wrong:** Attempting to create multiple consumers with overlapping subject filters on a WorkQueuePolicy stream returns error `10099: multiple non-filtered consumers not allowed on workqueue stream`
**Why it happens:** WorkQueuePolicy enforces single-consumer-per-subject semantics
**How to avoid:** Use a single consumer for the outbound stream. If multiple worker types are needed later, use filtered subjects (e.g., `messages.whatsapp.>` vs `messages.telegram.>`)
**Warning signs:** NATS error code 10099 during consumer creation

### Pitfall 2: DiscardNew Error Translation
**What goes wrong:** JetStream returns a NATS error when the stream is full, but the error message doesn't directly map to HTTP 429
**Why it happens:** NATS error codes are protocol-level, not HTTP-level
**How to avoid:** Check for `jetstream.ErrMaxMsgsExceeded` or NATS error code `10077` and translate to HTTP 429 with `Retry-After` header
**Warning signs:** Clients receiving 500 instead of 429 when queue is full

### Pitfall 3: Trace-ID Propagation Across Boundaries
**What goes wrong:** Trace-ID is lost when publishing to JetStream because it's not included in the message headers
**Why it happens:** Trace-ID lives in request context but JetStream messages need explicit headers
**How to avoid:** Extract trace-ID from context and set as `Nats-Msg-Id` header AND as a custom `X-Trace-Id` header on the NATS message
**Warning signs:** Audit logs for message.queued and message.dispatched have different trace_ids

### Pitfall 4: Rate Limiter Memory Growth
**What goes wrong:** `sync.Map` of rate limiters grows unbounded as new workspaces make requests
**Why it happens:** No eviction policy on the map
**How to avoid:** Acceptable for MVP with <10K workspaces. Add TTL-based eviction in a future phase if needed. Monitor via `expvar` counter of active limiters.
**Warning signs:** RSS growth proportional to workspace count

### Pitfall 5: Echo v5 Handler Signature Confusion
**What goes wrong:** Using `func(c echo.Context) error` (value receiver) instead of `func(c *echo.Context) error` (pointer)
**Why it happens:** Echo v4 used value receivers; v5 changed to pointers
**How to avoid:** Always use `*echo.Context` pointer in handler signatures. The middleware signature `echo.MiddlewareFunc` expects `func(echo.HandlerFunc) echo.HandlerFunc`.
**Warning signs:** Compile errors or runtime panics when accessing context methods

## Code Examples

### Message Domain Types

```go
// Source: REQUIREMENTS.md API-03, API-04
package domain

import (
    "time"
    "github.com/google/uuid"
)

// MessageStatus represents the lifecycle state of a message.
type MessageStatus string

const (
    StatusQueued     MessageStatus = "queued"
    StatusSent       MessageStatus = "sent"
    StatusDelivered  MessageStatus = "delivered"
    StatusRead       MessageStatus = "read"
    StatusFailed     MessageStatus = "failed"
)

// CreateMessageRequest is the JSON payload for POST /messages.
type CreateMessageRequest struct {
    To       string            `json:"to" validate:"required"`
    Channel  string            `json:"channel" validate:"required,oneof=whatsapp whatsapp_cloud telegram"`
    Body     string            `json:"body"`
    Metadata map[string]string `json:"metadata,omitempty"`
    TTLSeconds *int            `json:"ttl_seconds,omitempty"`
}

// CreateMessageResponse is returned on successful enqueue.
type CreateMessageResponse struct {
    MessageID uuid.UUID       `json:"message_id"`
    Status    MessageStatus   `json:"status"`
    QueuedAt  time.Time       `json:"queued_at"`
}

// ErrorResponse is the structured error format per API-04.
type ErrorResponse struct {
    Code     string       `json:"code"`
    Message  string       `json:"message"`
    MoreInfo string       `json:"more_info,omitempty"`
    Details  []FieldError `json:"details,omitempty"`
}

// FieldError provides field-level validation error details.
type FieldError struct {
    Field   string `json:"field"`
    Message string `json:"message"`
}
```

### JetStream Stream Initialization

```go
// Source: docs.nats.io/nats-concepts/jetstream/streams + nats-byexample.com
package queue

import (
    "context"
    "github.com/nats-io/nats.go"
    "github.com/nats-io/nats.go/jetstream"
)

const (
    StreamName    = "MESSAGES"
    StreamSubject = "messages.>"
    MaxQueueDepth = 1000
)

func EnsureStream(ctx context.Context, nc *nats.Conn) (jetstream.Stream, error) {
    js, err := jetstream.New(nc)
    if err != nil {
        return nil, err
    }

    stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
        Name:      StreamName,
        Subjects:  []string{StreamSubject},
        Retention: jetstream.WorkQueuePolicy,
        MaxMsgs:   MaxQueueDepth,
        Discard:   jetstream.DiscardNew,
        Storage:   jetstream.FileStorage,
        MaxAge:    24 * time.Hour,
    })
    if err != nil {
        return nil, err
    }
    return stream, nil
}
```

### Message Handler

```go
// Source: Echo v5 docs + nats.go jetstream docs
package handler

import (
    "net/http"
    "time"
    "github.com/google/uuid"
    "github.com/labstack/echo/v5"
    "github.com/nats-io/nats.go/jetstream"
    "github.com/pablojhp.pergo/internal/domain"
    "github.com/pablojhp.pergo/internal/platform/postgres/tenant"
)

type MessageHandler struct {
    JS   jetstream.JetStream
    Rate *RateLimiter
    QueueDepth *QueueDepthTracker
}

func (h *MessageHandler) Create(c *echo.Context) error {
    traceID, _ := middleware.TraceIDFrom(c.Request().Context())
    workspaceID, _ := tenant.WorkspaceIDFrom(c.Request().Context())

    // Rate limit check
    limiter := h.Rate.GetLimiter(workspaceID)
    if !limiter.Allow() {
        return c.JSON(http.StatusTooManyRequests, domain.ErrorResponse{
            Code:    "rate_limited",
            Message: "too many requests, slow down",
            MoreInfo: "https://docs.pergo.dev/errors/rate_limited",
        })
    }

    // Queue depth check
    if h.QueueDepth.Exceeds(workspaceID, 1000) {
        c.Response().Header().Set("Retry-After", "5")
        return c.JSON(http.StatusTooManyRequests, domain.ErrorResponse{
            Code:    "queue_full",
            Message: "per-session message queue limit exceeded",
            MoreInfo: "https://docs.pergo.dev/errors/queue_full",
        })
    }

    // Bind and validate
    var req domain.CreateMessageRequest
    if err := c.Bind(&req); err != nil {
        return c.JSON(http.StatusBadRequest, domain.ErrorResponse{
            Code:    "invalid_payload",
            Message: "request body validation failed",
            Details: []domain.FieldError{{Field: "body", Message: "invalid JSON"}},
        })
    }

    // Publish to JetStream
    payload, _ := json.Marshal(req)
    _, err := h.JS.Publish(c.Request().Context(), "messages.outbound", payload,
        jetstream.WithMsgID(traceID),
    )
    if err != nil {
        // Backpressure: queue full
        if isMaxMsgsError(err) {
            c.Response().Header().Set("Retry-After", "5")
            return c.JSON(http.StatusTooManyRequests, domain.ErrorResponse{
                Code:    "backpressure",
                Message: "message queue is full, retry later",
                MoreInfo: "https://docs.pergo.dev/errors/backpressure",
            })
        }
        return err
    }

    h.QueueDepth.Increment(workspaceID)

    return c.JSON(http.StatusAccepted, domain.CreateMessageResponse{
        MessageID: uuid.New(),
        Status:    domain.StatusQueued,
        QueuedAt:  time.Now().UTC(),
    })
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `nats.JetStream()` legacy API | `jetstream.New(nc)` simplified API | nats.go v1.19+ | Cleaner interfaces, better pull-consumer support |
| Echo v4 `echo.Context` value | Echo v5 `*echo.Context` pointer | Echo v5.0 (2026-01) | Handler signatures changed to pointer receivers |
| Manual rate limiting with maps | `golang.org/x/time/rate` token bucket | Standard since Go 1.x | Concurrent-safe, burst-aware rate limiting |

**Deprecated/outdated:**
- `nats.JetStream()` legacy API: use `jetstream.New(nc)` instead
- Echo v4 handler patterns: v5 uses `*echo.Context` pointers

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `jetstream.ErrMaxMsgsExceeded` is the correct error type for DiscardNew backpressure | Code Examples | Handler won't catch backpressure correctly — planner should verify error type |
| A2 | NATS error code 10077 corresponds to MaxMsgs exceeded | Common Pitfalls | Wrong error code in fallback check |
| A3 | Per-session queue depth can be tracked in-memory (sync.Map of atomics) | Architecture | If workspace count exceeds 10K, memory may be a concern |

## Open Questions

1. **JetStream stream name and subject pattern**
   - What we know: Stream name "MESSAGES" with subject "messages.>" is logical
   - What's unclear: Whether Phase 4 (WhatsApp Web) needs separate subject hierarchies
   - Recommendation: Use "messages.>" now; Phase 4 can add filtered consumers if needed

2. **Message payload schema completeness**
   - What we know: CREATE request needs `to`, `channel`, `body`, optional `metadata` and `ttl_seconds`
   - What's unclear: Whether `channel` field is needed now (Phase 3 has no real dispatch) or if it should be a placeholder
   - Recommendation: Include `channel` field in schema for forward compatibility; worker stub ignores it

3. **Queue depth tracking precision**
   - What we know: 1,000-message per-session limit is required
   - What's unclear: Whether to track via JetStream StreamInfo (accurate but slow) or in-memory atomic counter (fast but approximate)
   - Recommendation: In-memory atomic counter for the handler fast path; JetStream StreamInfo for monitoring/expvar

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| NATS Server | JetStream | ✓ (in docker-compose) | 2.10+ | — |
| Go 1.26.4 | Build | ✓ | 1.26.4 | — |
| Docker | Integration tests | ✓ | — | — |

**Missing dependencies with no fallback:** None
**Missing dependencies with fallback:** None

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `go test` |
| Config file | none — uses `go test ./...` |
| Quick run command | `go test ./internal/api/handler/... ./internal/platform/queue/... -v -count=1` |
| Full suite command | `go test ./... -race -count=1` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| API-01 | POST /messages accepts valid payload, returns 202 with trace header | integration | `go test ./cmd/pergo/ -run TestIngestAPI -v` | ❌ Wave 0 |
| API-02 | Invalid payload returns 400 with field-level errors | unit | `go test ./internal/api/handler/ -run TestCreateValidation -v` | ❌ Wave 0 |
| API-03 | Status enum values are correct and transitions documented | unit | `go test ./internal/domain/ -run TestMessageStatus -v` | ❌ Wave 0 |
| API-04 | Error responses match {code, message, more_info} format | unit | `go test ./internal/api/handler/ -run TestErrorResponseFormat -v` | ❌ Wave 0 |
| API-05 | TTL field rejected if zero/negative | unit | `go test ./internal/api/handler/ -run TestTTLValidation -v` | ❌ Wave 0 |
| QUEUE-01 | JetStream stream created with WorkQueuePolicy | integration | `go test ./internal/platform/queue/ -run TestStreamCreation -v` | ❌ Wave 0 |
| QUEUE-02 | Backpressure returns 429 when queue full | integration | `go test ./internal/platform/queue/ -run TestBackpressure -v` | ❌ Wave 0 |
| QUEUE-03 | Dedup: same trace_id published twice results in single message | integration | `go test ./internal/platform/queue/ -run TestDedup -v` | ❌ Wave 0 |
| QUEUE-04 | Rate limiter blocks after threshold exceeded | unit | `go test ./internal/api/middleware/ -run TestRateLimit -v` | ❌ Wave 0 |
| QUEUE-05 | Consumer retries with backoff on NAK | integration | `go test ./internal/platform/queue/ -run TestRetryBackoff -v` | ❌ Wave 0 |

### Sampling Rate

- **Per task commit:** `go test ./internal/api/handler/... ./internal/platform/queue/... -v -count=1`
- **Per wave merge:** `go test ./... -race -count=1`
- **Phase gate:** Full suite green before `/gsd-verify-work`

### Wave 0 Gaps

- [ ] `internal/domain/message.go` — Message types, status enum, request/response structs
- [ ] `internal/api/handler/message.go` — POST /messages handler
- [ ] `internal/api/handler/message_test.go` — Handler unit tests
- [ ] `internal/api/middleware/ratelimit.go` — Per-session rate limiting
- [ ] `internal/api/middleware/ratelimit_test.go` — Rate limiter tests
- [ ] `internal/platform/queue/jetstream.go` — Stream setup + publisher
- [ ] `internal/platform/queue/jetstream_test.go` — Queue integration tests
- [ ] `internal/platform/queue/consumer.go` — Worker stub + Dispatcher interface
- [ ] `internal/platform/queue/consumer_test.go` — Worker tests
- [ ] `cmd/pergo/ingest_test.go` — End-to-end integration test
- [ ] Migration `003_message_status.sql` — If message status needs DB tracking (likely not needed for MVP — status is in-memory/JetStream metadata)

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes | Existing auth middleware (Phase 1) validates API keys |
| V3 Session Management | no | No sessions — API key auth only |
| V4 Access Control | yes | workspace_id from auth context scopes all operations |
| V5 Input Validation | yes | Echo v5 `c.Bind()` + custom validation on message payload |
| V6 Cryptography | no | No new crypto in this phase |

### Known Threat Patterns for Go + NATS Stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Message injection via invalid payload | Tampering | Input validation + channel whitelist in handler |
| Rate limit bypass via workspace spoofing | Elevation of Privilege | workspace_id from auth context (trusted), not from payload |
| Queue flooding (DoS) | Denial of Service | MaxMsgs + DiscardNew backpressure + per-session depth limit |
| Trace-ID spoofing | Information Disclosure | Trace-ID for debugging only; spoofed IDs harm attacker only |

## Sources

### Primary (HIGH confidence)
- [docs.nats.io/nats-concepts/jetstream/streams] - WorkQueuePolicy, DiscardPolicy, MaxMsgs configuration
- [nats-byexample.com/examples/jetstream/workqueue-stream/go] - WorkQueuePolicy Go example with non-overlapping consumers
- [pkg.go.dev/github.com/nats-io/nats.go/jetstream] - jetstream package API: CreateStream, Publish, WithMsgID, ConsumerConfig
- [pkg.go.dev/github.com/labstack/echo/v5] - Echo v5 handler signature, c.Bind(), c.JSON(), MiddlewareFunc
- [pkg.go.dev/golang.org/x/time/rate] - rate.NewLimiter, Allow(), Wait(), token bucket semantics

### Secondary (MEDIUM confidence)
- [GitHub nats-io/nats.go README] - jetstream package overview, publish patterns, consumer patterns

### Tertiary (LOW confidence)
- None — all findings verified against official docs

## Metadata

**Confidence breakdown:**
- Standard Stack: HIGH — all packages already in go.mod, verified via proxy.golang.org
- Architecture: HIGH — patterns derived from official NATS and Echo documentation
- Pitfalls: HIGH — documented in NATS streams docs and Echo v5 API_CHANGES_V5.md

**Research date:** 2026-06-25
**Valid until:** 2026-07-25 (stable stack, no fast-moving dependencies)
