---
phase: 29-connection-slugs-and-api-channel-routing
plan: 29-03
subsystem: api
tags: [validation, routing, outbound, go]

requires:
  - phase: 29-connection-slugs-and-api-channel-routing
    provides: "ConnectionRepository GetBySlug method and slug cache"
provides:
  - "Relaxed payload validation allowing connection slugs in channel field"
  - "OutboundProcessor slug route resolution precedence (GetBySlug -> GetDefaultChannelConnection fallback)"
affects: [29-04]

tech-stack:
  added: []
  patterns: [slug-first-routing-precedence]

key-files:
  created: []
  modified:
    - internal/domain/message.go
    - internal/outbound/processor.go
    - internal/outbound/processor_test.go

key-decisions:
  - "Relaxed ValidateMessage to allow arbitrary strings (slugs) in req.Channel while keeping required check"
  - "Updated Processor.Ingest to attempt GetBySlug first when req.From is omitted, falling back to GetDefaultChannelConnection on ErrConnectionNotFound"
  - "Set QueueMessage.Channel to underlying conn.Channel for downstream workers"

patterns-established:
  - "Routing precedence: req.From -> GetBySenderIdentity; req.Channel -> GetBySlug -> GetDefaultChannelConnection"

requirements-completed: []

coverage:
  - id: D1
    description: "ValidateMessage allows connection slugs in req.Channel and req.FallbackChannels"
    verification:
      - kind: unit
        ref: "internal/domain/message_test.go"
        status: pass
    human_judgment: false
  - id: D2
    description: "OutboundProcessor resolves connections by slug with fallback to default channel"
    verification:
      - kind: unit
        ref: "internal/outbound/processor_test.go#TestProcessor_Ingest/Ingest_message_with_connection_slug_succeeds"
        status: pass
    human_judgment: false

duration: 5 min
completed: 2026-07-25
status: complete
---

# Plan 29-03: API Routing & Validation Updates Summary

**Relaxed API payload validation to accept connection slugs in `channel` field and updated `OutboundProcessor` for slug-first route resolution**

## Performance

- **Duration:** 5 min
- **Started:** 2026-07-25T14:41:45Z
- **Completed:** 2026-07-25T14:42:53Z
- **Tasks:** 2
- **Files modified:** 3

## Accomplishments
- Removed rigid static `ValidChannels` whitelist validation in `domain.ValidateMessage`, enabling `channel` to accept connection slugs (e.g. `"vendas-sp"`).
- Updated `RouteResolver` interface with `GetBySlug` method signature.
- Implemented slug-first routing precedence in `OutboundProcessor.Ingest`: `GetBySlug` -> fallback to `GetDefaultChannelConnection`.
- Set output `QueueMessage.Channel` to `conn.Channel` so worker engines receive the canonical channel type.

## Task Commits

1. **Task 03.1 & Task 03.2: Payload Validation & Processor Routing** - `5f82776` (feat)

## Files Created/Modified
- `internal/domain/message.go` - Removed static `ValidChannels` check from `req.Channel` and `req.FallbackChannels`
- `internal/outbound/processor.go` - Updated `RouteResolver` interface and `Ingest` routing precedence
- `internal/outbound/processor_test.go` - Updated `fakeRouteResolver` and added `Ingest_message_with_connection_slug_succeeds` unit test

## Decisions Made
- Allowed `req.Channel` to contain any non-empty string so CRMs can target specific connection slugs directly.
- Retained fallback channel duplicate detection while removing static whitelist checking.

## Deviations from Plan
None - plan executed exactly as written.

## Issues Encountered
None.

## Next Phase Readiness
- Ready for Plan 29-04 (Admin UI device handling for connection slug auto-generation and display).

---
*Phase: 29-connection-slugs-and-api-channel-routing*
*Completed: 2026-07-25*
