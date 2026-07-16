---
phase: 17-multi-webhook-subscriptions
plan: 17-P01
subsystem: database
tags: [postgres, go, nats, migrations, testing]

# Dependency graph
requires:
  - phase: 11-settings-layout
    provides: settings dashboard infrastructure
provides:
  - Database schema and repository for multi-webhook subscriptions
  - Glob wildcard matching utilities for webhook event filtering
  - Webhook delivery stream definitions in NATS JetStream
affects: [17-multi-webhook-subscriptions]

# Tech tracking
tech-stack:
  added: []
  patterns: [Envelope encryption for subscriptions, Glob wildcard matching via path.Match]

key-files:
  created:
    - internal/platform/postgres/migrations/023_create_webhook_subscriptions.sql
    - internal/repository/webhook_subscription.go
    - internal/repository/webhook_subscription_test.go
    - internal/webhook/wildcard.go
    - internal/webhook/wildcard_test.go
  modified:
    - internal/repository/webhook_dlq.go
    - internal/repository/webhook_dlq_test.go
    - internal/platform/queue/jetstream.go
    - internal/webhook/dispatcher.go
    - internal/webhook/dispatcher_test.go
    - cmd/pergo/admin_webhook_dlq_test.go
    - cmd/pergo/admin_audit_test.go

key-decisions:
  - "Used stdlib path.Match for dot-separated glob matching of webhook event patterns"
  - "Provided backward compatible WebhookConfig access layer by emulating a single configuration using the first active subscription"

patterns-established:
  - "Subscription-based DLQ routing by mapping to webhook_subscriptions table with a foreign key constraint"

requirements-completed: ["SUBS-01", "SUBS-02", "SUBS-03", "SUBS-04"]

coverage:
  - id: D1
    description: "Database migration for multi-webhook subscriptions table and DLQ foreign key reference"
    requirement: "SUBS-01"
    verification:
      - kind: integration
        ref: "internal/repository/webhook_subscription_migration_test.go#TestWebhookSubscriptionMigration"
        status: pass
    human_judgment: false
  - id: D2
    description: "Encrypted Webhook Subscription CRUD operations repository"
    requirement: "SUBS-02"
    verification:
      - kind: unit
        ref: "internal/repository/webhook_subscription_test.go#TestWebhookSubscriptionRepository"
        status: pass
    human_judgment: false
  - id: D3
    description: "Wildcard event pattern matching via Go stdlib path.Match"
    requirement: "SUBS-03"
    verification:
      - kind: unit
        ref: "internal/webhook/wildcard_test.go#TestMatchEvent"
        status: pass
    human_judgment: false
  - id: D4
    description: "NATS JetStream WEBHOOK_DELIVERIES work-queue stream setup"
    requirement: "SUBS-04"
    verification:
      - kind: integration
        ref: "internal/platform/queue/jetstream_test.go#TestEnsureWebhookStream"
        status: pass
    human_judgment: false

# Metrics
duration: 8min
completed: 2026-07-16
status: complete
---

# Phase 17: Multi-Webhook Subscriptions - Plan P01 Summary

**Database schema, encrypted repositories, glob pattern matching, and NATS JetStream configurations to support multi-webhook subscriptions**

## Performance

- **Duration:** 8 min
- **Started:** 2026-07-16T10:44:42-03:00
- **Completed:** 2026-07-16T10:52:00-03:00
- **Tasks:** 7
- **Files modified:** 8 (13 total files touched/created)

## Accomplishments
- Created migration `023_create_webhook_subscriptions.sql` that establishes the multi-webhook subscriptions table, drops the legacy `webhook_configs` table, and references `subscription_id` in `webhook_dlqs` with cascade deletion constraints.
- Developed `WebhookSubscriptionRepository` with database CRUD operations, and envelope encryption/decryption using the KEK provider.
- Adapted `WebhookDLQRepository` to scan and persist the `subscription_id` column, and implemented a compatibility layer for legacy config operations (`GetConfig`, `SaveConfig`, `DeleteConfig`) using the first active workspace subscription.
- Implemented `MatchEvent` and `MatchesAny` wildcard glob utilities using `path.Match` standard library.
- Configured a new NATS JetStream work-queue stream `WEBHOOK_DELIVERIES` (listening to `webhooks.deliveries.>`) and isolated the raw events stream `WEBHOOKS` to `webhooks.events` to prevent overlapping subject issues.
- Updated all integration and unit tests across repository, webhook, queue, and admin packages, ensuring a 100% pass rate.

## Task Commits

Each task was committed atomically:

1. **Task 1: Database Migration Schema** - `25b583a` (feat)
2. **Task 2: Webhook Subscription Repository** - `8e01847` (feat)
3. **Task 3: Webhook DLQ Repository Update & Fallbacks** - `71195e6` (refactor)
4. **Task 4: Repository Tests Update** - `8451299` (test)
5. **Task 5: Wildcard Glob Event Matching** - `62ddb7b` (feat)
6. **Task 6: NATS Streams Setup & Dispatcher Interfaces** - `2c3efcd` (refactor)
7. **Task 7: Command/Integration Tests Update** - `17f72de` (test)

## Files Created/Modified
- `internal/platform/postgres/migrations/023_create_webhook_subscriptions.sql` - Database migration script (Created)
- `internal/repository/webhook_subscription_migration_test.go` - Up/Down migration tests (Created)
- `internal/repository/webhook_subscription.go` - Subscription CRUD operations with envelope encryption (Created)
- `internal/repository/webhook_subscription_test.go` - Subscription CRUD unit tests (Created)
- `internal/repository/webhook_dlq.go` - DLQ repository with subscription support and legacy compatibility (Modified)
- `internal/repository/webhook_dlq_test.go` - DLQ tests updated to pass subscription ID (Modified)
- `internal/webhook/wildcard.go` - Wildcard glob pattern matching utilities (Created)
- `internal/webhook/wildcard_test.go` - Glob pattern matching unit tests (Created)
- `internal/platform/queue/jetstream.go` - Restricts webhooks stream and creates webhook deliveries stream (Modified)
- `internal/webhook/dispatcher.go` - Updates dispatcher interfaces and DLQ storage logic (Modified)
- `internal/webhook/dispatcher_test.go` - Mocks and dispatcher unit tests (Modified)
- `cmd/pergo/admin_webhook_dlq_test.go` - Integration test updated with subscription ID (Modified)
- `cmd/pergo/admin_audit_test.go` - Fixed pre-existing partition test bug by dynamically creating partitions (Modified)

## Decisions Made
- Used Go's standard library `path.Match` to evaluate wildcard event pattern fits because dots match standard glob wildcard `*` symbols perfectly.
- Retained support for legacy `WebhookConfig` structs and functions inside repositories and handlers to minimize changes in other modules until the worker delivery pipeline is fully replaced.
- Modified `admin_audit_test.go`'s `seedAuditEvent` helper to dynamically call `create_monthly_partition` during testing to ensure partition-related insert errors are completely avoided on any run date.

## Deviations from Plan
None - plan executed exactly as written.

## Issues Encountered
- Pre-existing partition error in `admin_audit_test.go` was exposed when the PostgreSQL database was started via `make infra` (it was previously skipped in environments where PostgreSQL was not reachable). The bug was successfully resolved by dynamically calling `create_monthly_partition` before seeding test events.

## User Setup Required
None - database migrations are executed automatically on application startup.

## Next Phase Readiness
- Foundation is completely ready. The database schemas, NATS stream definitions, event matching logic, and compatible access methods are online.
- The next step (Phase 17, Plan P02) is ready to implement the worker delivery routing pipeline, worker concurrency, delivery backoffs, and user interfaces.

---
*Phase: 17-multi-webhook-subscriptions*
*Completed: 2026-07-16*
