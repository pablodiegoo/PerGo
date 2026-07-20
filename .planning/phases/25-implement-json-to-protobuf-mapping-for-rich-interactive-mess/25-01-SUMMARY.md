---
phase: 25-implement-json-to-protobuf-mapping-for-rich-interactive-mess
plan: 01
subsystem: api
tags: [whatsapp, whatsmeow, protojson, waba, interactive, fallback]

# Dependency graph
requires: []
provides:
  - Unified Interactive schema mapping to WABA and whatsmeow
  - Channel override mechanism for WABA and whatsmeow
  - Fallback degradation logic for native channel limits
affects: []

# Tech tracking
tech-stack:
  added: ["google.golang.org/protobuf/encoding/protojson"]
  patterns: ["adapter mapping isolation"]

key-files:
  created: []
  modified: 
    - internal/domain/message.go
    - internal/channel/whatsapp/waba.go
    - internal/channel/whatsapp/adapter.go

key-decisions:
  - "D-01: Gateway performs well-formedness checks only. Downstream adapters enforce limits (e.g., max 3 buttons) and apply fallback behavior."
  - "D-02: Overrides (`channel_overrides.whatsapp_cloud` and `whatsapp`) trigger total replacement of payload; unified Interactive schema is ignored."
  - "D-03: `fallback_behavior` supports `degrade` (transform to text) or `fail` (return `TerminalError`)."

patterns-established:
  - "Interactive mapping: extract mapping logic to helpers to facilitate unit testing without requiring a connected whatsmeow client."

requirements-completed: []

coverage: []

# Metrics
duration: 45min
completed: 2026-07-20
status: complete
---

# Phase 25: Implement JSON-to-Protobuf mapping for rich interactive messages Summary

**Unified interactive message mapping and override handling for WABA and Whatsmeow with graceful degradation fallbacks.**

## Performance

- **Duration:** 45m
- **Started:** 2026-07-20T10:30:00Z
- **Completed:** 2026-07-20T11:15:00Z
- **Tasks:** 4
- **Files modified:** 7

## Accomplishments
- Implemented `Interactive` unified domain schema for buttons and lists.
- Added support for `ChannelOverrides` to bypass domain mapping and inject raw channel payloads (JSON for WABA, protojson for whatsmeow).
- Added `FallbackBehavior` (`degrade`, `fail`) to handle cases where interactive messages exceed channel-specific limits (e.g., max 3 buttons in WhatsApp).
- Successfully mapped schemas to both Meta HTTP format (WABA) and `waE2E.Message` protobufs (whatsmeow).

## Task Commits

Each task was committed atomically:

1. **Task 1: Domain structures** - `5fa692c` (feat)
2. **Task 2: Queue orchestrator routing** - `8b6ea40` (feat)
3. **Task 3: WABA adapter mapping** - `80534a9` (feat)
4. **Task 4: Whatsmeow adapter mapping** - `603fc2e` (feat)

## Files Created/Modified
- `internal/domain/message.go` - Added Interactive, ChannelOverrides, FallbackBehavior
- `internal/domain/message_test.go` - Added validation tests
- `internal/outbound/processor.go` - Map fields to queue payload
- `internal/channel/dispatcher.go` - Expose fields in MessagePayload
- `internal/platform/queue/orchestrator.go` - Propagate fields to dispatch
- `internal/channel/whatsapp/waba.go` - Added WABA mapping and overrides
- `internal/channel/whatsapp/waba_test.go` - Added WABA mapping tests
- `internal/channel/whatsapp/adapter.go` - Added whatsmeow mapping, overrides, fallback
- `internal/channel/whatsapp/adapter_test.go` - Added mapping tests using helper extraction

## Decisions Made
- Extracted whatsmeow mapping logic into `buildInteractiveOrOverrideMsg` to enable comprehensive unit testing of the protobuf generation without requiring a connected device session.
- Implemented degradation by converting interactive bodies and buttons/lists into a single formatted plaintext string.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
- `isButtonsMessage_Header` in `waE2E` is an unexported interface, preventing direct allocation of the protobuf oneof field for `ButtonsMessage.Header`. Resolved by setting `msg.ButtonsMessage.Header` directly to the `*waE2E.ButtonsMessage_Text` struct after initialization.

## Next Phase Readiness
- Interactive message routing and rendering is fully implemented and covered by unit tests. Ready for integration.
