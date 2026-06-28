---
phase: 03-ingest-api-queue
plan: 02
subsystem: queue
tags: [nats, jetstream, workqueue, durable-queue, worker]

requires:
  - phase: 03-ingest-api-queue
    provides: "POST /messages handler, Publisher interface, domain types"
  - phase: 01-Foundation
    provides: "NATS connection, Echo v5 server, trace middleware"
provides:
  - "JetStream WorkQueuePolicy stream 'MESSAGES' with MaxMsgs=1000 and DiscardNew"
  - "JetStreamPublisher with Nats-Msg-Id dedup header"
  - "Worker stub that reads and logs dispatched messages"
  - "Composition root wiring: stream, publisher, worker, shutdown lifecycle"
affects: [03-ingest-api-queue, 04-channel-dispatch]

tech-stack:
  added: []
  patterns: ["JetStream WorkQueuePolicy for durable at-least-once delivery", "Push-based consumer via Messages() channel"]

key-files:
  created:
    - internal/platform/queue/jetstream.go
    - internal/platform/queue/jetstream_test.go
    - internal/platform/queue/worker.go
  modified:
    - cmd/pergo/main.go
    - internal/api/handler/message.go

key-decisions:
  - "Publisher interface updated to accept traceID param for dedup header"
  - "EnsureStream takes *nats.Conn (not jetstream.JetStream) for simpler call-site ergonomics"
  - "Worker uses push-based Messages() channel for simplicity in stub phase"
  - "Stream cleanup in tests to work around WorkQueuePolicy single-consumer restriction"

patterns-established:
  - "JetStream integration tests use connectNATS helper with t.Skip when unavailable"
  - "Stream cleanup in test setup to avoid WorkQueuePolicy consumer conflicts"

requirements-completed: [QUEUE-01, QUEUE-03, API-01]

coverage:
  - id: D1
    description: "JetStream stream MESSAGES created with WorkQueuePolicy, MaxMsgs=1000, DiscardNew, FileStorage"
    requirement: QUEUE-01
    verification:
      - kind: integration
        ref: "internal/platform/queue/jetstream_test.go#TestEnsureStream"
        status: pass
    human_judgment: false
  - id: D2
    description: "EnsureStream is idempotent — calling twice does not error"
    requirement: QUEUE-01
    verification:
      - kind: integration
        ref: "internal/platform/queue/jetstream_test.go#TestEnsureStreamIdempotent"
        status: pass
    human_judgment: false
  - id: D3
    description: "Publisher sends messages with Nats-Msg-Id header set to trace_id"
    requirement: QUEUE-03
    verification:
      - kind: integration
        ref: "internal/platform/queue/jetstream_test.go#TestPublishAndConsume"
        status: pass
    human_judgment: false
  - id: D4
    description: "Dedup: publishing same trace_id twice results in single message in stream"
    requirement: QUEUE-03
    verification:
      - kind: integration
        ref: "internal/platform/queue/jetstream_test.go#TestPublishDedup"
        status: pass
    human_judgment: false
  - id: D5
    description: "Worker reads messages from JetStream consumer and logs them via slog"
    requirement: QUEUE-01
    verification:
      - kind: integration
        ref: "internal/platform/queue/jetstream_test.go#TestPublishAndConsume"
        status: pass
    human_judgment: false
  - id: D6
    description: "main.go wires JetStream stream, publisher into handler, worker goroutine, and graceful shutdown"
    requirement: API-01
    verification:
      - kind: other
        ref: "go build ./... && go vet ./..."
        status: pass
    human_judgment: false
  - id: D7
    description: "Handler publishes to JetStream after accepting the request, before returning 202"
    requirement: API-01
    verification:
      - kind: unit
        ref: "internal/api/handler/message_test.go#TestCreateMessageValid"
        status: pass
    human_judgment: false

duration: 79min
completed: 2026-06-26
status: complete
---

# Phase 3 Plan 02: JetStream Durability Layer Summary

**WorkQueue stream with publish-side dedup, worker stub, and composition root lifecycle wiring**

## Performance

- **Duration:** 79 min
- **Started:** 2026-06-26T00:03:28Z
- **Completed:** 2026-06-26T01:23:05Z
- **Tasks:** 2
- **Files modified:** 5

## Accomplishments
- JetStream WorkQueuePolicy stream "MESSAGES" with MaxMsgs=1000, DiscardNew, and 24h MaxAge
- JetStreamPublisher sends messages with Nats-Msg-Id = trace_id for publish-side dedup
- Worker stub reads messages from JetStream consumer, logs them, and acknowledges
- main.go wires stream, publisher, worker, and graceful shutdown (worker drains before NATS close)
- Integration tests cover stream creation, idempotency, publish-consume round-trip, and dedup

## Task Commits

Each task was committed atomically:

1. **Task 1: JetStream stream setup, publisher, and worker stub** - `2f1661e` (test)
2. **Task 2: Wire publisher into handler and worker lifecycle into composition root** - `998784a` (feat)

**Plan metadata:** pending (docs: complete plan)

## Files Created/Modified
- `internal/platform/queue/jetstream.go` - EnsureStream (WorkQueuePolicy), JetStreamPublisher with dedup
- `internal/platform/queue/jetstream_test.go` - 4 integration tests: stream creation, idempotency, publish-consume, dedup
- `internal/platform/queue/worker.go` - Worker stub using push-based Messages() channel
- `cmd/pergo/main.go` - JetStream init, publisher injection, worker creation, shutdown registration
- `internal/api/handler/message.go` - Publisher interface updated with traceID, handler calls Publish

## Decisions Made
- Publisher interface updated to accept traceID param for dedup header (breaking change from Plan 1)
- EnsureStream takes *nats.Conn directly (simpler call-site; creates jetstream.JetStream internally)
- Worker uses push-based Messages() channel for simplicity in stub phase (Phase 4 may switch to pull)
- Stream cleanup in each test avoids WorkQueuePolicy single-consumer-per-subject restriction

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed Publisher interface signature mismatch**
- **Found during:** Task 2 (wiring publisher into handler)
- **Issue:** Handler's Publisher interface had `Publish(ctx, subject, data)` but JetStreamPublisher needed `Publish(ctx, subject, data, traceID)` for dedup header
- **Fix:** Updated Publisher interface to accept traceID parameter; handler now marshals request and calls Publish with traceID
- **Files modified:** internal/api/handler/message.go
- **Verification:** go build ./... passes, handler tests pass
- **Committed in:** 998784a (Task 2 commit)

**2. [Rule 1 - Bug] Fixed WorkQueuePolicy single-consumer test conflict**
- **Found during:** Task 1 (test execution)
- **Issue:** TestPublishDedup failed with error 10099 because WorkQueuePolicy only allows one non-filtered consumer; previous test left consumer behind
- **Fix:** Added stream delete+recreate in test setup for tests that create consumers, ensuring clean state
- **Files modified:** internal/platform/queue/jetstream_test.go
- **Verification:** All 4 integration tests pass
- **Committed in:** 2f1661e (Task 1 commit)

---

**Total deviations:** 2 auto-fixed (2 bugs)
**Impact on plan:** Both auto-fixes essential for correctness. Interface change is intentional — traceID propagates dedup from handler to JetStream.

## Issues Encountered
None beyond the auto-fixed deviations.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- JetStream durability layer complete — messages persist across server restarts
- Worker stub validates consumer pipeline end-to-end
- Ready for Phase 3 Plan 3 (backpressure + rate limiting) or Phase 4 (real channel dispatch)

---
*Phase: 03-ingest-api-queue*
*Completed: 2026-06-26*

## Self-Check: PASSED

- All 5 key files exist on disk
- Both task commits (2f1661e, 998784a) verified in git log
- All 4 integration tests pass: TestEnsureStream, TestEnsureStreamIdempotent, TestPublishAndConsume, TestPublishDedup
- Full build and vet pass: `go build ./... && go vet ./...`
