---
phase: 21-chatwoot-integration
plan: "01"
subsystem: api
tags: [go, postgres, echo, templ, htmx]

requires: []
provides:
  - integrations database schema and migrations
  - IntegrationRepository and ChatwootMappingRepository implementations
  - integrations admin settings UI template page
  - ChatwootAdminHandler settings endpoints
  - ChatwootWebhookHandler authenticated receiver stub
affects:
  - 21-02-PLAN.md

tech-stack:
  added: []
  patterns:
    - Encrypted integration configurations inside unified BYTEA column config using AES-256-GCM.
    - Composite primary key `(workspace_id, contact_id, connection_id)` for chatwoot mappings to avoid multi-instance crosstalk.

key-files:
  created:
    - internal/platform/postgres/migrations/027_create_integrations_and_chatwoot_mappings.sql
    - internal/repository/integration.go
    - internal/repository/chatwoot_mapping.go
    - internal/api/handler/admin/integration.go
    - templates/pages/integrations.templ
    - internal/api/handler/chatwoot_webhook.go
    - internal/api/handler/chatwoot_webhook_test.go
  modified:
    - templates/layout/sidebar.templ
    - cmd/pergo/main.go

key-decisions:
  - "D-01: Used a single unified integrations database table for third-party config storage."
  - "D-02: Stored integration-specific credentials as encrypted JSON block using KEK AES-256-GCM."
  - "D-03: Maintained a local mapping database table chatwoot_mappings referencing connection_id."
  - "D-05: Authenticated webhook endpoint using AuthMiddleware query-parameter token validation."

patterns-established:
  - "Composite constraints on chatwoot_mappings: Prevent crosstalk across multiple accounts using connection_id."

requirements-completed:
  - CHAT-01
  - CHAT-02

coverage:
  - id: D1
    description: "Database integrations table and mapping table are created with composite indexes"
    requirement: "CHAT-01"
    verification:
      - kind: integration
        ref: "internal/repository/integration_test.go"
        status: pass
      - kind: integration
        ref: "internal/repository/chatwoot_mapping_test.go"
        status: pass
    human_judgment: false
  - id: D2
    description: "Admin configurations panel UI is integrated with sidebar configurations"
    requirement: "CHAT-01"
    verification:
      - kind: integration
        ref: "internal/api/handler/admin/integration_test.go"
        status: pass
    human_judgment: false
  - id: D3
    description: "Webhook receiver stub validates token query parameter using AuthMiddleware"
    requirement: "CHAT-02"
    verification:
      - kind: integration
        ref: "internal/api/handler/chatwoot_webhook_test.go"
        status: pass
    human_judgment: false

duration: 15min
completed: 2026-07-17
status: complete
---

# Phase 21 Plan 01: Chatwoot Schema, Repositories, settings UI and Webhook Stub Summary

**Implemented the core database schemas, encryption-at-rest repository interfaces, admin UI settings forms, and query-parameter authenticated receiver webhook stub.**

## Performance

- **Duration:** 15 min
- **Started:** 2026-07-17T17:25:20Z
- **Completed:** 2026-07-17T17:35:48Z
- **Tasks:** 3
- **Files modified:** 9

## Accomplishments
- Created database migration `027_create_integrations_and_chatwoot_mappings.sql` defining the `integrations` table (with config encryption KEK) and `chatwoot_mappings` table ( composite primary key `(workspace_id, contact_id, connection_id)` and lookups by conversation ID index).
- Implemented `IntegrationRepository` and `ChatwootMappingRepository` with pgx/v5.
- Designed integrations settings templ page and added its link to the admin sidebar layout.
- Implemented `ChatwootAdminHandler` for GET/POST setting paths.
- Setup `ChatwootWebhookHandler` stub on POST `/api/integrations/chatwoot` route protected by `AuthMiddleware` verifying key/token query validation.

## Task Commits

Each task was committed atomically:

1. **Task 1: Database Migration & Repositories** - `5061d33` (feat)
2. **Task 2: Admin Panel settings UI & Handler** - `385e0ef` (feat)
3. **Task 3: Webhook Route & Authentication test** - `49fbb6a` (feat)

## Files Created/Modified
- `internal/platform/postgres/migrations/027_create_integrations_and_chatwoot_mappings.sql` - migrations
- `internal/repository/integration.go` - Integration config repository
- `internal/repository/chatwoot_mapping.go` - Chatwoot mapping repository
- `internal/api/handler/admin/integration.go` - Admin settings controller
- `templates/pages/integrations.templ` - Integrations page template
- `templates/layout/sidebar.templ` - Added admin submenu route links
- `internal/api/handler/chatwoot_webhook.go` - Webhook receiver stub
- `internal/api/handler/chatwoot_webhook_test.go` - Webhook key authorization tests
- `cmd/pergo/main.go` - Wired repositories, settings and webhook route.

## Decisions Made
- Used a composite index and primary key `(workspace_id, contact_id, connection_id)` to scale mapping for omnichannel connections.
- Managed API key validation via PerGo's existing token parameter extraction in `AuthMiddleware`.

## Self-Check: PASSED
