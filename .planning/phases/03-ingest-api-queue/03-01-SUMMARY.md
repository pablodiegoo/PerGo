---
phase: 03-ingest-api-queue
plan: 01
subsystem: api
tags: [echo-v5, validation, domain-types, http-handler]

requires:
  - phase: 01-Foundation
    provides: "Echo v5 server, pgxpool, auth middleware, trace middleware, tenant context"
provides:
  - "POST /messages endpoint returning 202 Accepted with trace correlation"
  - "Domain types: MessageStatus, CreateMessageRequest/Response, ErrorResponse, FieldError"
  - "ValidateMessage function with channel whitelist and TTL validation"
  - "Publisher interface for future JetStream integration"
affects: [03-ingest-api-queue, 04-channel-dispatch]

tech-stack:
  added: []
  patterns: ["Echo v5 handler with dependency injection", "Domain validation returning structured errors"]

key-files:
  created:
    - internal/domain/message.go
    - internal/domain/message_test.go
    - internal/api/handler/message.go
    - internal/api/handler/message_test.go
  modified: []

key-decisions:
  - "Publisher interface defined now for DI; JetStream implementation deferred to Plan 2"
  - "Validation returns nil on success, *ErrorResponse on failure (no multi-error wrapper)"
  - "Handler skips publish when Publisher is nil (stub behavior until Plan 2)"

patterns-established:
  - "Domain validation: ValidateMessage returns *ErrorResponse with FieldError details"
  - "Handler pattern: RegisterRoutes + Create method with dependency injection"

requirements-completed: [API-01, API-02, API-03, API-04, API-05]

coverage:
  - id: D1
    description: "POST /messages accepts valid JSON payload and returns 202 Accepted with message_id, status, and queued_at"
    requirement: API-01
    verification:
      - kind: unit
        ref: "internal/api/handler/message_test.go#TestCreateMessageValid"
        status: pass
    human_judgment: false
  - id: D2
    description: "Response includes X-Trace-Id header matching request context trace_id"
    requirement: API-01
    verification:
      - kind: unit
        ref: "internal/api/handler/message_test.go#TestCreateMessageTraceHeader"
        status: pass
    human_judgment: false
  - id: D3
    description: "Invalid JSON body returns 400 with ErrorResponse code invalid_payload"
    requirement: API-02
    verification:
      - kind: unit
        ref: "internal/api/handler/message_test.go#TestCreateMessageInvalidJSON"
        status: pass
    human_judgment: false
  - id: D4
    description: "Validation failure returns 400 with field-level FieldError details"
    requirement: API-02
    verification:
      - kind: unit
        ref: "internal/api/handler/message_test.go#TestCreateMessageMissingTo"
        status: pass
    human_judgment: false
  - id: D5
    description: "MessageStatus enum defines queued, sent, delivered, read, failed"
    requirement: API-03
    verification:
      - kind: unit
        ref: "internal/domain/message_test.go#TestMessageStatusValues"
        status: pass
    human_judgment: false
  - id: D6
    description: "ErrorResponse marshals to JSON with code, message, more_info, details fields"
    requirement: API-04
    verification:
      - kind: unit
        ref: "internal/domain/message_test.go#TestErrorResponseJSON"
        status: pass
    human_judgment: false
  - id: D7
    description: "TTL field rejects zero or negative values"
    requirement: API-05
    verification:
      - kind: unit
        ref: "internal/domain/message_test.go#TestValidateMessageZeroTTL"
        status: pass
    human_judgment: false

duration: 2min
completed: 2026-06-26
status: complete
---

# Phase 3 Plan 01: Unified POST /messages Endpoint Summary

**POST /messages with JSON validation, structured error responses, and X-Trace-Id correlation header**

## Performance

- **Duration:** 2 min
- **Started:** 2026-06-25T23:58:51Z
- **Completed:** 2026-06-26T00:00:59Z
- **Tasks:** 2
- **Files modified:** 4

## Accomplishments
- Domain types (MessageStatus, CreateMessageRequest/Response, ErrorResponse) with validation logic
- POST /messages handler returning 202 Accepted with trace correlation via X-Trace-Id header
- Structured 400 errors with field-level details for invalid payloads
- Publisher interface defined for future JetStream integration (Plan 2)

## Task Commits

Each task was committed atomically:

1. **Task 1: Domain types, validation, and error contract** - `50f3245` (test)
2. **Task 2: POST /messages handler with happy path and error responses** - `db44ac8` (feat)

## Files Created/Modified
- `internal/domain/message.go` - Message status enum, request/response structs, ValidateMessage function
- `internal/domain/message_test.go` - Table-driven tests for all validation rules and JSON serialization
- `internal/api/handler/message.go` - POST /messages handler with Publisher interface and dependency injection
- `internal/api/handler/message_test.go` - Handler tests for happy path, invalid JSON, missing fields, invalid channel, zero TTL, trace header

## Decisions Made
- Publisher interface defined now for DI; JetStream implementation deferred to Plan 2
- Validation returns nil on success, *ErrorResponse on failure (no multi-error wrapper)
- Handler skips publish when Publisher is nil (stub behavior until Plan 2)

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- Domain types and handler contract established for Plan 2 (JetStream integration)
- Publisher interface ready for JetStream implementation
- Auth middleware integration verified in existing test suite

---
*Phase: 03-ingest-api-queue*
*Completed: 2026-06-26*
