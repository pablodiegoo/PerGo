---
phase: 18-omnichannel-contact-merging
plan: 18-P01
subsystem: database
tags: [postgres, go, migrations, testing, inbound, queue]

# Dependency graph
requires:
  - phase: 17-multi-webhook-subscriptions
    provides: Webhook subscriptions infrastructure
provides:
  - Database schema and repositories for contacts and contact identities
  - Contextual inbound contact resolution pipeline
  - Compatibility-adapted legacy Telegram repositories
  - Orchestrator translation of Telegram identities using the unified registry
affects: [18-omnichannel-contact-merging]

# Tech tracking
tech-stack:
  added: []
  patterns: [Concurrent contact upserts, Contact identity mapping cross-linking, Space-split name parsing compatibility]

key-files:
  created:
    - internal/platform/postgres/migrations/024_omnichannel_contact_merging.sql
    - internal/domain/contact.go
    - internal/repository/contact.go
    - internal/repository/contact_test.go
  modified:
    - internal/repository/telegram_contact.go
    - internal/inbound/processor.go
    - internal/inbound/processor_test.go
    - internal/channel/telegram/inbound.go
    - internal/api/handler/telegram_webhook.go
    - internal/api/handler/telegram_webhook_test.go
    - internal/api/handler/waba_webhook_test.go
    - internal/platform/queue/orchestrator.go
    - internal/platform/queue/worker_test.go
    - cmd/pergo/main.go

key-decisions:
  - "Concurrently protected contact resolution using transaction blocks and ON CONFLICT handling"
  - "Implemented space-splitting on the Contact name field to seamlessly map back to first/last name columns expected by legacy Telegram repos"
  - "Removed direct dependency on TelegramContactRepository from inbound webhook flow, offloading contact resolution to the generic InboundProcessor"

patterns-established:
  - "Omnichannel identity mapping registry linked via contact_identities and resolved dynamically on ingestion"

requirements-completed: ["CONT-01", "CONT-02", "CONT-03", "CONT-04"]

coverage:
  - id: D1
    description: "Database migration for contacts and contact_identities tables, backfilling legacy telegram contacts and audit logs"
    requirement: "CONT-01"
    verification:
      - kind: integration
        ref: "internal/repository/connection_migration_test.go#TestConnectionMigration"
        status: pass
    human_judgment: false
  - id: D2
    description: "Unified contact and contact identity domain models struct mapping"
    requirement: "CONT-01"
    verification:
      - kind: unit
        ref: "internal/repository/contact_test.go#TestContactRepository"
        status: pass
    human_judgment: false
  - id: D3
    description: "Contact Repository implementing GetByID, ResolveContact (concurrency-safe), MergeContacts, SearchContacts, and ResolveTelegramChatID"
    requirement: "CONT-02"
    verification:
      - kind: integration
        ref: "internal/repository/contact_test.go#TestContactRepository"
        status: pass
    human_judgment: false
  - id: D4
    description: "Inbound contact identity resolution pipeline integrated into InboundProcessor"
    requirement: "CONT-03"
    verification:
      - kind: integration
        ref: "internal/inbound/processor_test.go#TestInboundProcessor_Process"
        status: pass
    human_judgment: false
  - id: D5
    description: "Orchestrator translation of Telegram handle/phone identifiers using ContactRepository"
    requirement: "CONT-04"
    verification:
      - kind: integration
        ref: "internal/platform/queue/worker_test.go#TestOrchestrator_TelegramContactResolution"
        status: pass
    human_judgment: false

# Metrics
duration: 15min
completed: 2026-07-16
status: complete
---

# Phase 18: Omnichannel Contact Merging - Plan P01 Summary

**Establishment of the core omnichannel contacts data models, migration schema, ContactRepository, inbound identity resolution pipeline, adapted legacy Telegram repositories, and queue orchestrator integration.**

## Performance

- **Duration:** 15 min
- **Started:** 2026-07-16T12:15:40-03:00
- **Completed:** 2026-07-16T12:30:30-03:00
- **Tasks:** 9
- **Files modified:** 8 (11 total files touched/created)

## Accomplishments
- Created migration `024_omnichannel_contact_merging.sql` which establishes the workspace-scoped `contacts` and `contact_identities` tables, migrates legacy `telegram_contacts` records, backfills WhatsApp and Telegram identities from historical `audit_logs`, and drops `telegram_contacts`.
- Defined Go domain models for `Contact` and `ContactIdentity` in `internal/domain/contact.go`.
- Implemented `ContactRepository` in `internal/repository/contact.go` to handle database CRUD operations, concurrency-safe `ResolveContact` queries using transaction blocks, `MergeContacts` merging logic, `SearchContacts` type-ahead queries, and `ResolveTelegramChatID` identifier routing.
- Added comprehensive unit and integration tests verifying all repository behaviors including concurrent execution safety.
- Refactored legacy `TelegramContactRepository` as a backward-compatibility facade mapping legacy methods (`Upsert`, `Get`, `Resolve`) to the new schema tables behind the scenes, splitting contact names on spaces to satisfy first/last name expectations.
- Updated `InboundProcessor` to resolve contact profiles automatically during inbound message processing, extracting `SenderName` and `Metadata` fields from the channel-specific payloads.
- Refactored the Telegram webhook adapter to extract and return sender name and username/phone metadata, removing its legacy `telegramContactRepo` dependency since resolution is now generalized in `InboundProcessor`.
- Integrated `ContactRepository` into the queue orchestrator to translate Telegram identifier handles/phones on outbound dispatch.
- Wired all dependencies in the composition root `cmd/pergo/main.go` and verified successful compilation and test pass rates.

## Task Commits

Each task was committed atomically:

1. **Task 1: Database Migration Schema** - `91e496d` (feat)
2. **Task 2: Contact Domain Models** - `011a2ea` (feat)
3. **Task 3: Contact Repository Implementation** - `c13ef92` (feat)
4. **Task 4: Contact Repository Test Suite** - `0296492` (feat)
5. **Task 5: Legacy Telegram Repository Facade** - `e885527` (feat)
6. **Task 6: Inbound Processor Contact Resolution** - `c5dddc6` (feat)
7. **Task 7: Telegram Webhook & Adapter Refactoring** - `77a8393` (feat)
8. **Task 8: Queue Orchestrator Contact Resolution** - `1112375` (feat)
9. **Task 9: Composition Root Integration (main.go)** - `0639295` (feat)

## Files Created/Modified
- `internal/platform/postgres/migrations/024_omnichannel_contact_merging.sql` - Migration script (Created)
- `internal/domain/contact.go` - Contact and ContactIdentity structs (Created)
- `internal/repository/contact.go` - Access layer (Created)
- `internal/repository/contact_test.go` - Repository tests (Created)
- `internal/repository/telegram_contact.go` - Facade wrapper (Modified)
- `internal/inbound/processor.go` - Contact resolution pipeline hooks (Modified)
- `internal/inbound/processor_test.go` - Processor tests (Modified)
- `internal/channel/telegram/inbound.go` - Inbound adapter metadata extraction (Modified)
- `internal/api/handler/telegram_webhook.go` - Remove contact repo dependency (Modified)
- `internal/api/handler/telegram_webhook_test.go` - Webhook tests (Modified)
- `internal/api/handler/waba_webhook_test.go` - WABA webhook tests (Modified)
- `internal/platform/queue/orchestrator.go` - Resolve Telegram chats on outbound (Modified)
- `internal/platform/queue/worker_test.go` - Orchestrator tests (Modified)
- `cmd/pergo/main.go` - Wiring dependencies (Modified)

## Decisions Made
- Protected against concurrent resolution race conditions using database `ON CONFLICT DO NOTHING` inside pgx transactions, retrying lookups within the same transaction to guarantee absolute safety.
- Handled legacy first/last name compatibility inside the Telegram facade by splitting `contacts.name` on the first space when querying contact details.
- Avoided duplicating contact resolution logic inside channel adapters by centralizing it inside the unified `InboundProcessor` pipeline, keeping adapter code thin and focused solely on payload parsing.

## Deviations from Plan
None - plan executed exactly as written.

## Issues Encountered
None - tests compiled and passed without issues.

## User Setup Required
None - database migrations are executed automatically on application startup.

## Next Phase Readiness
- Foundations, database tables, and the inbound/outbound resolution engines are complete and fully tested.
- Ready to proceed to Plan P02 which implements Inbox conversational threads grouped by Contact ID, visual Dashboard merging features, and search endpoints.

---
*Phase: 18-omnichannel-contact-merging*
*Completed: 2026-07-16*
