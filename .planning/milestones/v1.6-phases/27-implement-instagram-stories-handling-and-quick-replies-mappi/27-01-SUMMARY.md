---
phase: 27-implement-instagram-stories-handling-and-quick-replies-mappi
plan: 01
subsystem: channel
tags: [instagram, adapter, webhook]

# Dependency graph
requires:
  - phase: 26-implement-telegram-inline-keyboards-and-forum-threads-mappin
    provides: []
provides:
  - Instagram Outbound Adapter using Meta Graph API
  - Instagram Inbound Adapter supporting Story Mentions/Replies and Quick Replies
affects: []

# Tech tracking
tech-stack:
  added: []
  patterns: [instagram-graph-api]

key-files:
  created:
    - internal/channel/instagram/adapter.go
    - internal/channel/instagram/inbound.go
  modified:
    - internal/domain/message.go
    - internal/inbound/processor.go
    - cmd/pergo/main.go

key-decisions:
  - "Modeled Instagram webhooks directly handling generic payloads and waba-style payload for robust compatibility."

patterns-established:
  - "Instagram inbound mapping pattern mirroring WhatsApp structure with unique story payload translation."

requirements-completed:
  - INSTA-01

coverage:
  - id: D1
    description: "Instagram outbound adapter formats unified payloads to Meta Graph API for Instagram"
    verification: []
    human_judgment: true
    rationale: "Requires testing with actual Meta Instagram account credentials to ensure end-to-end functionality."
  - id: D2
    description: "Instagram inbound adapter maps Instagram webhooks to unified InboundEvents"
    verification: []
    human_judgment: true
    rationale: "Requires sending real Instagram story mentions and replies to the webhook endpoint."

# Metrics
duration: 10min
completed: 2026-07-20
status: complete
---

# Phase 27 Plan 01: Implement Instagram Stories handling and Quick Replies mapping Summary

**Instagram channel adapters implemented with full support for Outbound messaging, Story Mentions, and Quick Replies**

## Performance

- **Duration:** 10 min
- **Started:** 2026-07-20T13:05:00Z
- **Completed:** 2026-07-20T13:15:00Z
- **Tasks:** 4
- **Files modified:** 5

## Accomplishments
- Extended domain and inbound models for `instagram` ValidChannel and `InboundStoryEvent`
- Implemented `InstagramAdapter` translating domain messages to Meta Graph API for Instagram, including quick replies mapping
- Implemented `InstagramInboundAdapter` translating standard and WABA-wrapped webhooks into generic domain events, preserving story mentions and interactive replies
- Registered `instagram` adapter in the central `dispatcherRegistry` in `cmd/pergo/main.go`

## Task Commits

Each task was committed atomically:

1. **Task 1: Update Domain Models** - `0ba0aaf` (feat)
2. **Task 2: Implement Instagram Outbound Adapter** - `70fd928` (feat)
3. **Task 3: Implement Instagram Inbound Adapter** - `5756f15` (feat)
4. **Task 4: Register Instagram Adapter** - `b6e5771` (feat)

## Files Created/Modified
- `internal/domain/message.go` - Added instagram to ValidChannels
- `internal/inbound/processor.go` - Added InboundStoryEvent structs and parsing
- `internal/channel/instagram/adapter.go` - Outbound dispatcher for Instagram
- `internal/channel/instagram/inbound.go` - Webhook parser for Instagram
- `cmd/pergo/main.go` - Registered instagram dispatcher

## Decisions Made
- Implemented dual-format webhook parsing to robustly handle standard Messenger API and WABA-style Webhook payloads

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Instagram channel integration ready for end-to-end UAT.

---
*Phase: 27-implement-instagram-stories-handling-and-quick-replies-mappi*
*Completed: 2026-07-20*
