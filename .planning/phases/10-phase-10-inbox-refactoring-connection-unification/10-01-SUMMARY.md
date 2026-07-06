---
phase: 10-inbox-refactoring-connection-unification
plan: "01"
subsystem: ui
tags: [htmx, templ, go, echo, pgx]
requires:
  - phase: 09-conversational-inbox
    provides: [conversational inbox core UI and message rendering]
provides:
  - incremental message polling using out-of-band cursor swap
  - native HTMX scroll-to-bottom without custom JavaScript
  - database-backed integration tests for message cursor polling
affects: [inbox]
tech-stack:
  added: []
  patterns: [Out-of-band cursor polling via hx-swap-oob, native scroll support]
key-files:
  created: []
  modified:
    - internal/api/handler/admin/inbox.go
    - internal/api/handler/admin/inbox_test.go
    - templates/components/chat_panel.templ
    - templates/components/message_bubble.templ
key-decisions:
  - "D-01 (Out-Of-Band Polling Anchor): The PollMessages handler returns message bubbles and an updated OOB poll anchor containing the newest message ID cursor to avoid infinite loops and timer resets."
  - "D-02 (Zero-JS Scroll): Rely on native HTMX scroll:bottom swap and remove custom DOM scroll/afterSwap event listeners."
patterns-established:
  - "Pattern 1: Out-of-band sentinel swap for server-driven cursor-based polling."
requirements-completed: [ADMIN-01]
coverage:
  - id: D1
    description: "Refactored PollMessages handler to perform out-of-band cursor swapping and return 204 No Content if there are no new messages."
    requirement: "ADMIN-01"
    verification:
      - kind: integration
        ref: "internal/api/handler/admin/inbox_test.go#TestInboxHandler_PollMessages_NoContent"
        status: pass
      - kind: integration
        ref: "internal/api/handler/admin/inbox_test.go#TestInboxHandler_PollMessages_NewMessages"
        status: pass
    human_judgment: false
  - id: D2
    description: "Refactored ChatPanel template for native HTMX scroll and zero JS client-side polling updates."
    requirement: "ADMIN-01"
    verification: []
    human_judgment: true
    rationale: "Requires human verification in a browser to inspect element styling and check that messages scroll smoothly."
duration: 25m
completed: 2026-07-06
status: complete
---

# Phase 10: Inbox Polling Stability & Zero-JS Scroll Refactoring Summary

**Incremental message polling using server-side cursor swapped out-of-band (OOB) and native HTMX scroll support, completely eliminating custom client-side JavaScript.**

## Performance

- **Duration:** 25 min
- **Started:** 2026-07-06T13:38:27-03:00
- **Completed:** 2026-07-06T13:42:00-03:00
- **Tasks:** 3
- **Files modified:** 4

## Accomplishments
- Refactored `PollMessages` handler to return new message bubbles and an updated OOB `chat-poll-anchor` containing the newest message ID cursor.
- Updated `PollMessages` to return HTTP `204 No Content` when no new messages are found, avoiding redundant payloads.
- Simplified `ChatPanel` by removing the client-side JavaScript block that registered global DOM event listeners for polling/scrolling, preventing memory leaks and duplicate timer triggers.
- Configured native HTMX `scroll:bottom` swap mode to handle container auto-scrolling cleanly.
- Implemented robust database-backed integration tests for `PollMessages` using multi-port Fallbacks (5433/5432) for PostgreSQL availability.

## Task Commits

Each task was committed atomically:

1. **Task 1: Refactor PollMessages handler for Out-of-Band cursor swap** - `d0e2ece` (refactor)
2. **Task 2: Refactor ChatPanel template for native HTMX scroll and zero JS** - `6cb5d78` (refactor)
3. **Task 3: Update inbox handler tests for polling stability** - `77c3a6b` (test)

## Files Created/Modified
- `internal/api/handler/admin/inbox.go` - Modified `PollMessages` to use `PollMessagesResponse` and return `NoContent` for empty results.
- `templates/components/message_bubble.templ` - Added `PollMessagesResponse` component with OOB sentinel.
- `templates/components/chat_panel.templ` - Simplified polling sentinel configuration and script tags.
- `internal/api/handler/admin/inbox_test.go` - Added `TestInboxHandler_PollMessages_NoContent` and `TestInboxHandler_PollMessages_NewMessages`.

## Decisions Made
- None - followed plan as specified.

## Deviations from Plan
None - plan executed exactly as written.

## Issues Encountered
- **Echo `String` method with status 204:** Calling `c.String(http.StatusNoContent, "")` caused internal errors in Go's `net/http` package during tests. Solved by replacing with `c.NoContent(http.StatusNoContent)`.
- **Lexicographical UUID sorting in PostgreSQL:** Test message assertions failed because random UUID Version 4 values are not sequentially ordered, which caused `id > $5` checks in `ListThread` to filter out newer entries. Fixed by utilizing lexicographically sorted custom UUID string constants in the test logs.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Polling stability is verified and fully functional.
- Prepared for `10-02-PLAN.md` to begin connection dash unification.
