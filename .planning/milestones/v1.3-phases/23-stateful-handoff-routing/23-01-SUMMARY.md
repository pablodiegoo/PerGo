---
phase: 23-stateful-handoff-routing
plan: "01"
subsystem: database
tags: [postgres, go, testing, migration]

requires:
  - phase: 22-typebot-integration
    provides: Typebot integration infrastructure
provides:
  - Stateful bot_active flag and bot_paused_at on contacts schema
  - Typebot integration interception logic based on bot_active flag
  - 12-hour inactivity cooldown reset in InboundProcessor
  - pause_bot Messaging Verbs Engine action with duration parsing
  - Chatwoot webhook receiver auto-pause on human agent reply
affects:
  - stateful-handoff-routing

tech-stack:
  added: []
  patterns:
    - Offset bot_paused_at to encode manual pause duration

key-files:
  created:
    - internal/platform/postgres/migrations/029_add_bot_active_to_contacts.sql
  modified:
    - internal/domain/contact.go
    - internal/repository/contact.go
    - internal/repository/contact_test.go
    - internal/integration/typebot/forwarder.go
    - internal/integration/typebot/forwarder_test.go
    - internal/inbound/processor.go
    - internal/inbound/processor_test.go
    - internal/webhook/verbs.go
    - internal/webhook/verbs_test.go
    - internal/api/handler/chatwoot_webhook.go
    - cmd/pergo/main.go
    - internal/api/handler/chatwoot_webhook_test.go

key-decisions:
  - "Used duration offset on bot_paused_at to calculate dynamic cooldowns without introducing new schema columns"

patterns-established:
  - "Pattern 1: Lazy evaluated timeout checks inside central InboundProcessor to reset state on first incoming message"

requirements-completed:
  - HAND-01
  - HAND-02
  - HAND-03
  - HAND-04
  - HAND-06

coverage:
  - id: D1
    description: "Database migrations, domain model, and repository methods for contacts bot_active and bot_paused_at fields"
    requirement: "HAND-01"
    verification:
      - kind: unit
        ref: "internal/repository/contact_test.go#TestContactRepository/UpdateBotState"
        status: pass
    human_judgment: false
  - id: D2
    description: "Interception logic inside TypebotForwarder to check bot_active state"
    requirement: "HAND-02"
    verification:
      - kind: unit
        ref: "internal/integration/typebot/forwarder_test.go#TestTypebotForwarder/SyncInboundMessage_BotInactive"
        status: pass
    human_judgment: false
  - id: D3
    description: "InboundProcessor 12-hour inactivity cooldown lazy reset"
    requirement: "HAND-06"
    verification:
      - kind: unit
        ref: "internal/inbound/processor_test.go#TestInboundProcessor_BotCooldown"
        status: pass
    human_judgment: false
  - id: D4
    description: "pause_bot messaging verb execution engine logic"
    requirement: "HAND-04"
    verification:
      - kind: unit
        ref: "internal/webhook/verbs_test.go#TestVerbsEngine/Pause_bot_indefinitely"
        status: pass
      - kind: unit
        ref: "internal/webhook/verbs_test.go#TestVerbsEngine/Pause_bot_with_duration"
        status: pass
    human_judgment: false
  - id: D5
    description: "ChatwootWebhookHandler auto-pausing bot active state on human agent response"
    requirement: "HAND-03"
    verification:
      - kind: unit
        ref: "internal/api/handler/chatwoot_webhook_test.go#TestChatwootWebhookHandler_Integration/ProcessValidAgentReplyAndPublish"
        status: pass
    human_judgment: false

duration: 15min
completed: 2026-07-17
status: complete
---

# Phase 23: Stateful Handoff Routing — Plan 01 Summary

**Stateful handoff routing foundation including schema migration, Typebot sync interception, lazy-evaluated cooldowns, pause_bot verb, and Chatwoot agent reply auto-pausing.**

## Performance

- **Duration:** 15 min
- **Started:** 2026-07-17T19:32:30-03:00
- **Completed:** 2026-07-17T19:35:40-03:00
- **Tasks:** 6
- **Files modified:** 13

## Accomplishments
- Implemented stateful `bot_active` and `bot_paused_at` schema columns in `contacts` table.
- Intercepted incoming customer messages inside `TypebotForwarder` to skip forwarding when `bot_active` is `false`.
- Added lazy-evaluated 12-hour inactivity cooldown reset within `InboundProcessor` on incoming messages.
- Added `pause_bot` verb with optional duration parameters to Webhook Messaging Verbs Engine.
- Updates `bot_active` to `false` automatically when a valid agent reply is received on Chatwoot webhook handler.

## Task Commits

Each task was committed atomically:

1. **Task 1: Create a migration** - `64dd9d0` (feat)
2. **Task 2: Update domain and repository** - `f6eff96` (feat)
3. **Task 3: Intercept TypebotForwarder** - `3264e24` (feat)
4. **Task 4: Implement InboundProcessor cooldown** - `d8d6991` (feat)
5. **Task 5: Extend Messaging Verbs Engine** - `b78be7c` (feat)
6. **Task 6: Update Chatwoot webhook receiver** - `39a31c0` (feat)

## Files Created/Modified
- `internal/platform/postgres/migrations/029_add_bot_active_to_contacts.sql` - Migration defining new columns
- `internal/domain/contact.go` - Fields added to Contact model
- `internal/repository/contact.go` - Scanning and updating query fields
- `internal/repository/contact_test.go` - Tests for UpdateBotState
- `internal/integration/typebot/forwarder.go` - Early exit when bot is inactive
- `internal/integration/typebot/forwarder_test.go` - Early exit unit test assertion
- `internal/inbound/processor.go` - 12-hour inactivity cooldown reactivation
- `internal/inbound/processor_test.go` - Cooldown unit tests
- `internal/webhook/verbs.go` - Added pause_bot verb
- `internal/webhook/verbs_test.go` - pause_bot verb unit tests
- `internal/api/handler/chatwoot_webhook.go` - Update contact state on agent reply
- `cmd/pergo/main.go` - Wired dependencies for Chatwoot webhook handler
- `internal/api/handler/chatwoot_webhook_test.go` - Assert bot active toggled to false on agent replies

## Decisions Made
- Used duration offset on `bot_paused_at` (e.g. subtracting `12h - d` from `NOW()`) to allow the existing 12-hour inactivity check to trip exactly when the requested duration expires, eliminating the need for adding another database column.

## Deviations from Plan
None - plan executed exactly as written.

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Stateful handoff backend routing is fully functional and tested.
- Ready for Plan 02 of Phase 23: build the manual toggle badge inside the Chat Panel component header.

---
*Phase: 23-stateful-handoff-routing*
*Completed: 2026-07-17*
