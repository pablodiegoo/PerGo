---
phase: 09-conversational-inbox
plan: 01
subsystem: database
tags: [postgres, go, telegram, whatsapp, audit]

requires:
  - phase: 08-multi-instance-connections-dashboard-ui
    provides: connections schema and credentials encryption
provides:
  - recipient_sessions multi-instance compound key migration
  - inbound webhooks displaying recipient identity "to" field
  - window checker multi-instance isolation
  - conversation grouping and thread union queries in audit repo
affects: [09-conversational-inbox]

tech-stack:
  added: []
  patterns: [Union-based query for multi-instance chronological messaging logs]

key-files:
  created: [internal/repository/audit_test.go]
  modified:
    - internal/repository/recipient_session.go
    - internal/repository/credentials.go
    - internal/session/inbound_processor.go
    - internal/session/manager.go
    - internal/api/handler/telegram_webhook.go
    - internal/api/handler/waba_webhook.go
    - internal/channel/whatsapp/waba.go
    - internal/repository/audit.go
    - internal/session/window.go

key-decisions:
  - "Decided to include recipient_identity as part of recipient_sessions primary key to prevent data mixing between multiple connections/bots of the same channel."
  - "Decided to run a UNION query in ListThread matching on both inbound (payload->>'to') and outbound (payload->'request'->>'sender_identity') recipient identities to cleanly separate multi-instance conversation lines."

patterns-established:
  - "Multi-instance isolation: passing recipient identity explicitly to session tracking, webhooks, and queries."

requirements-completed:
  - migration_recipient_sessions_to_column
  - enrich_whatsapp_inbound
  - enrich_telegram_waba_inbound
  - implement_audit_queries
  - update_recipient_session_repository

coverage:
  - id: D1
    description: "Database migration adding recipient_identity to recipient_sessions primary key and partition self-healing logic"
    requirement: migration_recipient_sessions_to_column
    verification:
      - kind: integration
        ref: "internal/repository/connection_migration_test.go"
        status: pass
    human_judgment: false
  - id: D2
    description: "Enrich WhatsApp inbound processing to write recipientIdentity (d.Phone or JID fallback)"
    requirement: enrich_whatsapp_inbound
    verification:
      - kind: unit
        ref: "internal/session/inbound_test.go"
        status: pass
    human_judgment: false
  - id: D3
    description: "Enrich Telegram webhook and credentials saving to capture bot username and log to field"
    requirement: enrich_telegram_waba_inbound
    verification:
      - kind: integration
        ref: "internal/api/handler/telegram_webhook_test.go"
        status: pass
    human_judgment: false
  - id: D4
    description: "Enrich WABA webhook to parse metadata display phone number and log to field"
    requirement: enrich_telegram_waba_inbound
    verification:
      - kind: integration
        ref: "internal/api/handler/waba_webhook_test.go"
        status: pass
    human_judgment: false
  - id: D5
    description: "Audit repository methods ListConversations and ListThread with multi-instance thread stitching"
    requirement: implement_audit_queries
    verification:
      - kind: integration
        ref: "internal/repository/audit_test.go"
        status: pass
    human_judgment: false
  - id: D6
    description: "Recipient Session Repository updated to accept and save recipientIdentity"
    requirement: update_recipient_session_repository
    verification:
      - kind: integration
        ref: "internal/repository/recipient_session_test.go"
        status: pass
    human_judgment: false

duration: 120min
completed: 2026-07-03
status: complete
---

# Phase 9: Conversational Inbox (Plan 01) Summary

**Conversational view data layer supporting multi-instance isolation, including schema migration, enriched inbound webhooks, and thread stitching queries.**

## Performance

- **Duration:** 120 min
- **Started:** 2026-07-03T18:55:00Z
- **Completed:** 2026-07-03T20:55:00Z
- **Tasks:** 5
- **Files modified:** 12

## Accomplishments
- Migrated `recipient_sessions` schema to add `recipient_identity` to the primary key, avoiding collision between multiple connections of the same channel.
- Updated `RecipientSessionRepository` to save and lookup sessions using the `recipientIdentity`.
- Enriched inbound webhooks for WhatsApp Web, WhatsApp Cloud, and Telegram to log the `to` field (representing our connection/bot identity) and upsert the session with this identity.
- Implemented `ListConversations` grouping logs by contact, channel, and recipient identity, and `ListThread` stitching inbound and outbound messages together chronologically with strict multi-instance isolation.
- Created `internal/repository/audit_test.go` verifying the grouping and thread stitching.

## Task Commits

Each task was committed atomically:

1. **Task 1: migration_recipient_sessions_to_column** - `d3fc101` (migration)
2. **Task 5: update_recipient_session_repository** - `e74b76a` (feat)
3. **Task 2: enrich_whatsapp_inbound** - `030c43c` (feat)
4. **Task 3: enrich_telegram_waba_inbound** - `f5e346c` (feat)
5. **Task 4: implement_audit_queries** - `4e14992` (feat)

## Files Created/Modified
- `internal/platform/postgres/migrations/013_recipient_sessions_to_column.sql` - Added column, pk, index, fixed legacy July partition
- `internal/repository/recipient_session.go` - Updated struct and signatures for recipient identity
- `internal/repository/recipient_session_test.go` - Test updates
- `internal/session/inbound_processor.go` - Added recipient identity parameter and to field logging
- `internal/session/manager.go` - Pass recipient identity to Handle
- `internal/api/handler/telegram_webhook.go` - Capture and log bot username
- `internal/api/handler/telegram_webhook_test.go` - Updated tests
- `internal/api/handler/waba_webhook.go` - Capture and log display phone number
- `internal/channel/whatsapp/waba.go` - Window checker signature updates
- `internal/channel/whatsapp/waba_test.go` - Test mock updates
- `internal/repository/credentials.go` - Save bot username as sender identity
- `internal/session/window.go` - Interface update to support recipient identity
- `internal/session/window_test.go` - Window test updates
- `internal/repository/audit.go` - Implemented ListConversations and ListThread
- `internal/repository/audit_test.go` - Added integration tests for ListConversations and ListThread

## Decisions Made
- Used the bot's username (e.g. `@testbot`) as the recipient identity for Telegram bots, and Display Phone Number for WABA.
- Designed `ListThread` using a SQL `UNION ALL` statement that enforces matching recipient identities on both inbound and outbound halves to guarantee database isolation of conversations.

## Deviations from Plan
None - plan executed exactly as written.

## Issues Encountered
- `cmd/pergo/main.go` failed to build initially because `session.RecipientSessionReader` interface signature expected the old `Get` method parameter count. Resolved by refactoring the `WindowChecker` interface and method calls to pass `recipientIdentity` throughout the fallback checks.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Data layer for conversational inbox is complete and verified. Ready for the next plan or phase (API endpoints for fetching threads and front-end inbox integration).

---
*Phase: 09-conversational-inbox*
*Completed: 2026-07-03*
