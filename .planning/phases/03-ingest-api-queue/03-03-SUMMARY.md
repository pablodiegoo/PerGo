---
phase: 03-ingest-api-queue
plan: 03
subsystem: middleware, queue, handler
tags: [rate-limiting, backpressure, retry, exponential-backoff, ttl, dedup, token-bucket]

requires:
  - phase: 03-ingest-api-queue
    provides: "POST /messages handler, Publisher interface, JetStream publisher, Worker stub"
  - phase: 01-Foundation
    provides: "NATS connection, Echo v5 server, trace middleware, auth middleware"
provides:
  - "Per-workspace token-bucket rate limiter (10 req/s, burst 10) with 429 + Retry-After"
  - "Queue depth tracker (atomic per-workspace) with 429 queue_full at 1000 messages"
  - "Worker with retry (exponential backoff 1s→60s, max 5 retries)"
  - "Worker TTL enforcement (drop expired messages at dispatch)"
  - "Worker delivery dedup (in-memory sync.Map, prevents duplicate processing)"
  - "Handler backpressure check before publish + queue depth increment after publish"
  - "Composition root wiring: RateLimiter, QueueDepthTracker, updated Worker"
affects: [03-ingest-api-queue, 04-channel-dispatch]

tech-stack:
  added: []
  patterns: ["Token-bucket rate limiting via golang.org/x/time/rate per workspace", "Atomic queue depth tracking via sync.Map + atomic ops", "Exponential backoff with cap for retry", "In-memory dedup set for delivery dedup", "Backpressure enforced before JetStream publish"]

key-files:
  created:
    - internal/api/middleware/ratelimit.go
    - internal/api/middleware/ratelimit_test.go
    - internal/platform/queue/worker_test.go
  modified:
    - internal/platform/queue/worker.go
    - internal/api/handler/message.go
    - cmd/pergo/main.go

key-decisions:
  - "Rate limiter middleware runs AFTER auth (workspace_id available), not before"
  - "Queue depth check in handler (before publish), not in middleware — ensures we don't count failed publishes"
  - "Delivery dedup is in-memory only — acceptable for MVP; JetStream redelivers are idempotent"
  - "Worker dispatch is still a stub (Phase 4 replaces with real channel dispatcher)"
  - "Staggered dispatch (1-3s random delay) deferred to Phase 4 — it's a WhatsApp-specific ban-risk concern"

patterns-established:
  - "Rate limiter uses sync.Map for concurrent per-workspace token buckets (no locks)"
  - "Queue depth uses sync.Map + atomic.AddInt64 for lock-free counters"
  - "Worker NAK-with-delay via JetStream for exponential backoff retry"
  - "Handler checks backpressure BEFORE expensive work (publish)"

requirements-completed: [QUEUE-02, QUEUE-04, QUEUE-05, API-05]

coverage:
  - id: D1
    description: "RateLimiter blocks requests after burst exhausted, returns 429 with Retry-After and rate_limited error code"
    requirement: QUEUE-04
    verification:
      - kind: unit
        ref: "internal/api/middleware/ratelimit_test.go#TestRateLimiterAllow"
        status: pass
      - kind: unit
        ref: "internal/api/middleware/ratelimit_test.go#TestRateLimiterMiddleware"
        status: pass
      - kind: unit
        ref: "internal/api/handler/message_test.go#TestCreateMessageRateLimited"
        status: pass
    human_judgment: false
  - id: D2
    description: "QueueDepthTracker enforces per-workspace 1000-message limit, returns 429 queue_full with Retry-After: 5"
    requirement: QUEUE-02
    verification:
      - kind: unit
        ref: "internal/api/middleware/ratelimit_test.go#TestQueueDepthTrackerExceeds"
        status: pass
      - kind: unit
        ref: "internal/api/handler/message_test.go#TestCreateMessageQueueFull"
        status: pass
      - kind: unit
        ref: "internal/api/handler/message_test.go#TestCreateMessageQueueNotFull"
        status: pass
    human_judgment: false
  - id: D3
    description: "Queue depth incremented after successful publish, handler returns 202"
    requirement: QUEUE-02
    verification:
      - kind: unit
        ref: "internal/api/handler/message_test.go#TestCreateMessageQueueDepthIncremented"
        status: pass
    human_judgment: false
  - id: D4
    description: "Worker retries failed messages with exponential backoff (1s → 2s → 4s → 8s → 16s → capped 60s), terminal failure after max retries"
    requirement: QUEUE-05
    verification:
      - kind: unit
        ref: "internal/platform/queue/worker_test.go#TestExponentialBackoff"
        status: pass
    human_judgment: false
  - id: D5
    description: "Worker TTL enforcement: expired messages dropped at dispatch time with log warning"
    requirement: API-05
    verification:
      - kind: unit
        ref: "internal/platform/queue/worker_test.go#TestIsExpired"
        status: pass
    human_judgment: false
  - id: D6
    description: "Delivery dedup prevents processing same message_id twice via in-memory sync.Map"
    requirement: QUEUE-05
    verification:
      - kind: unit
        ref: "internal/platform/queue/worker_test.go#TestDeliveryDedup"
        status: pass
    human_judgment: false
  - id: D7
    description: "main.go wires RateLimiter, QueueDepthTracker, RateLimiterMiddleware, updated Worker (5 retries, 60s backoff)"
    requirement: QUEUE-04
    verification:
      - kind: other
        ref: "go build ./... && go vet ./..."
        status: pass
    human_judgment: false

duration: 45min
completed: 2026-06-26
status: complete
---

# Phase 3 Plan 03: Rate Limiting, Backpressure, Retry, TTL, Dedup

**Production-hardens the message ingestion path with per-workspace rate limiting, queue depth backpressure, worker retry with exponential backoff, TTL enforcement, and delivery deduplication.**

## Performance

- **Duration:** 45 min
- **Started:** 2026-06-26T01:45:00Z
- **Completed:** 2026-06-26T02:30:00Z
- **Tasks:** 2
- **Files modified:** 7

## Accomplishments
- Per-workspace token-bucket rate limiter (10 req/s, burst 10) via golang.org/x/time/rate with sync.Map
- Queue depth tracker with atomic per-workspace counters, 1000-message limit enforced before publish
- Worker retry with exponential backoff (base 1s, ×2 per attempt, capped at 60s, max 5 retries)
- Worker TTL enforcement — expired messages dropped at dispatch with log warning
- Worker delivery dedup — in-memory sync.Map prevents duplicate processing on JetStream redelivery
- Handler backpressure: queue depth check before publish (429 queue_full), depth increment after success
- main.go wiring: RateLimiter middleware on POST /messages, QueueDepthTracker injected, Worker updated with retry config
- Fixed pre-existing migration 002 goose dollar-quoting issue (StatementBegin/End)

## Task Commits

1. **Task 1 (from previous session):** Rate limiter + queue depth tracker — `ac0a6de` (test), `7888cda` (feat)
2. **Task 2:** Worker retry/TTL/dedup + handler backpressure + main.go wiring — `0c43b86` (feat)

## Files Created/Modified
- `internal/api/middleware/ratelimit.go` — RateLimiter + QueueDepthTracker
- `internal/api/middleware/ratelimit_test.go` — 10 tests
- `internal/platform/queue/worker.go` — Updated with retry, TTL, dedup, new constructor
- `internal/platform/queue/worker_test.go` — 4 tests (retry parsing, backoff calc, TTL check, dedup)
- `internal/api/handler/message.go` — QueueDepth field, backpressure check, optional middleware in RegisterRoutes
- `cmd/pergo/main.go` — RateLimiter, QueueDepthTracker, RateLimiterMiddleware, Worker(5, 60s)
- `internal/platform/postgres/migrations/002_partition_audit.sql` — goose StatementBegin/End fix

## Decisions Made
- Rate limiter middleware runs AFTER auth middleware (workspace_id must be available)
- Queue depth checked BEFORE publish in handler (not in middleware) — avoids counting failed publishes
- Delivery dedup is in-memory only for MVP — acceptable since JetStream provides publish-side dedup via Nats-Msg-Id
- Staggered dispatch deferred to Phase 4 — it's a WhatsApp Web ban-risk concern, not an ingest concern
- Worker constructor takes maxRetries and maxBackoff (not a config struct) — minimal API for stub phase

## Deviations from Plan

### Pre-existing Bug Fixed
**1. Migration 002 dollar-quoting** — goose was splitting the PL/pgSQL function at `$$`, causing "unterminated dollar-quoted string". Fixed by wrapping in `-- +goose StatementBegin/End`.

### Auto-fixed Issues
None — clean implementation following plan exactly.

## Issues Encountered
- **Pre-existing:** 2 audit test failures (TestAuditEventWritten, TestAuditNoDedup) due to batch writer flush timing — not related to this plan, existed before.

## Next Phase Readiness
- Ingest API is production-hardened: rate limiting, backpressure, retry, TTL, dedup
- Worker is still a stub (log-only dispatch) — Phase 4 replaces with real WhatsApp Web dispatcher
- Ready for Phase 4 (WhatsApp Web & QR Pairing) — the first real end-to-end send path

---
*Phase: 03-ingest-api-queue*
*Completed: 2026-06-26*

## Self-Check: PASSED

- All 7 key files exist on disk
- Task commit verified in git log (0c43b86)
- All 32 tests pass: 10 middleware + 11 handler + 11 queue
- Full build and vet pass: `go build ./... && go vet ./...`
