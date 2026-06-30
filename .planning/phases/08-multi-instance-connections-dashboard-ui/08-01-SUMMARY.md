---
phase: 08-multi-instance-connections-dashboard-ui
plan: "08-01"
subsystem: database
tags: [postgres, goose, golang]
requires:
  - phase: 07-webhook-failures-retry-dlq
    provides: DB schema & Webhooks DLQ
provides:
  - connections DB migration consolidation
  - ConnectionRepository and tests
  - shimmed legacy CredentialsRepository & DeviceRepository
affects:
  - 08-multi-instance-connections-dashboard-ui
tech-stack:
  added: []
  patterns: [unified connections table migration and shimmed repository compatibility pattern]
key-files:
  created:
    - internal/platform/postgres/migrations/012_consolidate_connections.sql
    - internal/repository/connection.go
    - internal/repository/connection_test.go
    - internal/repository/connection_migration_test.go
  modified:
    - internal/repository/credentials.go
    - internal/repository/credentials_test.go
    - internal/session/device.go
key-decisions:
  - "Consolidated legacy devices and channel_credentials into unified connections table with UNIQUE(sender_identity)."
  - "Shimmed legacy CredentialsRepository and DeviceRepository on top of connections table to maintain backward compatibility without breaking existing code."
patterns-established:
  - "Resetting goose baseFS in migration tests to isolate local filesystem runs from other tests' embedded FS side effects."
requirements-completed:
  - "[D-01]"
coverage:
  - id: D-01
    description: "Goose migration 012_consolidate_connections.sql consolidates devices and channel_credentials into connections table, and ConnectionRepository manages CRUD operations and credentials crypto"
    requirement: "[D-01]"
    verification:
      - kind: integration
        ref: "internal/repository/connection_migration_test.go#TestConnectionMigration"
        status: pass
      - kind: integration
        ref: "internal/repository/connection_test.go#TestConnectionRepository"
        status: pass
    human_judgment: false
duration: 10min
completed: 2026-06-30
status: complete
---

# Phase 8 Wave 1: Multi-Instance Connections Database Schema Consolidation

**Consolidated devices and channel credentials into a single unified connections table, migrated all records safely, and shimmed legacy repositories with full backwards compatibility.**

## Performance

- **Duration:** 10 min
- **Started:** 2026-06-30T03:16:16Z
- **Completed:** 2026-06-30T03:25:00Z
- **Tasks:** 4
- **Files modified:** 7

## Accomplishments
- Implemented `012_consolidate_connections` Goose migration to create `connections` table, automatically migrate existing records, and drop legacy `devices`/`channel_credentials` tables safely.
- Implemented `ConnectionRepository` with CRUD methods and automated credential encryption/decryption.
- Refactored legacy `CredentialsRepository` and `DeviceRepository` as shims querying `connections` to maintain API and behavior backwards compatibility.
- Implemented extensive unit and integration tests covering migration correctness, rollbacks, and repository CRUD functionality.

## Task Commits

Each task was committed atomically:

1. **Task 1: Create connection migration validation test stub (Wave 0)** - `e7a58e2` (feat)
2. **Task 2: Write Goose migration to consolidate devices and credentials into connections** - `990310c` (feat)
3. **Task 3: Implement Connection and ConnectionRepository with CRUD and crypto support** - `6e0c5b3` (feat)
4. **Task 4: Refactor legacy DeviceRepository and CredentialsRepository to query connections table as shims** - `127bf9c` (refactor)

## Files Created/Modified
- `internal/platform/postgres/migrations/012_consolidate_connections.sql` - Consolidates legacy tables into connections.
- `internal/repository/connection.go` - Implements Connections model and repository.
- `internal/repository/connection_test.go` - Tests Connections repository CRUD and encryption.
- `internal/repository/connection_migration_test.go` - Integration tests verifying migration from version 11 to 12.
- `internal/repository/credentials.go` - Refactors legacy CredentialsRepository onto connections.
- `internal/repository/credentials_test.go` - Modifies legacy credentials test to query connections table.
- `internal/session/device.go` - Refactors legacy DeviceRepository onto connections.

## Decisions Made
- Used UUID-based sender_identity names for legacy credentials (`"legacy_" + channel + "_" + id`) to guarantee uniqueness constraints on table insert.
- Reset the package-level `goose` baseFS to `nil` in tests to prevent embedded FS side effects from breaking local filesystem migration tests.

## Deviations from Plan
None - plan executed exactly as written.

## Issues Encountered
- Encountered a global state side-effect in `goose` where testing packages calling `postgres.RunMigrations` set the base FS globally, breaking subsequent local filesystem test runs. Solved by calling `goose.SetBaseFS(nil)` at test startup.

## User Setup Required
None - database migration handles schemas automatically.

## Next Phase Readiness
- Connections database layer consolidated and compatibility shims fully verified.
- Core architecture ready for multi-instance outbound routing (Phase 8 Wave 2).

---
*Phase: 08-multi-instance-connections-dashboard-ui*
*Completed: 2026-06-30*
