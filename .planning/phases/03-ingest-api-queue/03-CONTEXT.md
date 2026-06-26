# Phase 3: Ingest API & Queue — Phase Context

**Gathered:** 2026-06-25
**Status:** Ready for planning
**Mode:** Auto-generated (smart discuss — autonomous mode)

<domain>
## Phase Boundary

The unified `POST /messages` endpoint accepts, validates, and durably enqueues messages with backpressure, dedup, rate limiting, and formal status/error contracts — the first half of the send path (worker dispatches via a stub until Phase 4 wires a real channel).

</domain>

<decisions>
## Implementation Decisions

### API Design
- Single `POST /messages` endpoint accepting standardized JSON payload
- Structured error responses: `{code, message, more_info}` with field-level details
- Formal status enum: queued → sent → delivered → read → failed
- Trace-ID generated per request and returned in response header

### Queue Architecture
- NATS JetStream with `WorkQueuePolicy` for durable at-least-once delivery
- Stream config: `MaxDeliver` + `AckWait`/`MaxBackoff` for automatic retries
- `Nats-Msg-Id = trace_id` for publish-side idempotency
- `dispatched_messages` dedup set prevents duplicate delivery on redelivery

### Backpressure
- 1,000-message per-session limit enforced before enqueue
- HTTP 429/422 with `Retry-After` header when limit exceeded
- Per-session rate limiting via `golang.org/x/time/rate`

### Message TTL
- `ttl_seconds` field in payload — queued messages expire instead of sending late
- TTL checked at dispatch time, not enqueue time

### Worker Stub
- Simple worker that logs dispatched messages (no real channel until Phase 4)
- Implements the `Dispatcher` interface that Phase 4 will wire to WhatsApp Web

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- Phase 1: Echo v5 server, pgxpool, audit batch writer, tenant-context, auth middleware
- Phase 2: Admin panel for workspace/key management (not directly used here)

### Established Patterns
- Repository pattern for database operations
- Middleware pattern for auth and trace propagation
- Handler pattern with structured error responses

### Integration Points
- `POST /messages` mounts on existing Echo instance
- Auth middleware validates API keys before message processing
- Audit batch writer logs message ingestion events
- NATS connection from Phase 1 readiness check (now used for JetStream)

</code_context>

<specifics>
## Specific Ideas

- The worker stub should be clearly marked as temporary (Phase 4 replaces it)
- Status transitions should be well-defined to support Phase 5 fallback logic
- Dedup mechanism must work across server restarts (durable JetStream)

</specifics>

<deferred>
## Deferred Ideas

- Real channel dispatch (Phase 4 — WhatsApp Web)
- Official channel adapters (Phase 5 — WABA, Telegram)
- Smart fallback pipeline (Phase 5)
- Webhook delivery (Phase 6)

</deferred>
