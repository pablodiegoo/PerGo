---
phase: 02-admin-shell
plan: 02
subsystem: ui
tags: [templ, htmx, workspace-crud, api-keys, modal, admin-panel]

requires:
  - phase: 02-admin-shell
    provides: "Session auth, HTMX fragment detection, base layout, sidebar navigation, templ render helper"
  - phase: 01-Foundation
    provides: "Workspace and API key repositories, pgxpool, database schema"
provides:
  - "Workspace CRUD handlers (list, create, detail, confirm-delete, delete)"
  - "API key management handlers (list, generate, confirm-revoke, revoke)"
  - "Workspace list/create/detail/delete templates with HTMX fragments"
  - "API key list/generate/revealed/revoke templates"
  - "Reusable ConfirmModal component for HTMX delete confirmations"
  - "Workspace repository List (with limit) and Delete methods"
  - "API key repository ListByWorkspace method"
  - "Admin routes wired: workspace CRUD + API key management"
affects: [03-Ingest-API]

tech-stack:
  added: []
  patterns: [htmx-fragment-crud, modal-confirmation, api-key-one-time-display]

key-files:
  created:
    - internal/api/handler/admin/workspace.go
    - internal/api/handler/admin/apikey.go
    - templates/pages/workspaces.templ
    - templates/pages/apikeys.templ
    - templates/components/modal.templ
    - cmd/pergo/admin_workspace_test.go
  modified:
    - internal/repository/workspace.go
    - internal/repository/apikey.go
    - cmd/pergo/main.go

key-decisions:
  - "Hex-encoded API key generation to avoid invalid UTF-8 in PostgreSQL prefix column"
  - "Unique workspace names per test to avoid constraint violations on shared DB"
  - "Workspace Delete returns empty 200 for HTMX row removal pattern"

patterns-established:
  - "HTMX CRUD pattern: list fragment + create form + detail page + delete modal"
  - "One-time key display: plaintext shown in APIKeyRevealed component on generate only"
  - "Modal confirmation: ConfirmModal component with hx-delete + closest tr target"

requirements-completed: [ADMIN-01, ADMIN-02]

coverage:
  - id: D1
    description: "Workspace list view with table, create form, and delete confirmation modal"
    requirement: ADMIN-01
    verification:
      - kind: integration
        ref: "cmd/pergo/admin_workspace_test.go#TestAdminWorkspaceList"
        status: pass
      - kind: integration
        ref: "cmd/pergo/admin_workspace_test.go#TestAdminWorkspaceCreate"
        status: pass
    human_judgment: false
  - id: D2
    description: "Workspace detail page showing API keys and workspace metadata"
    requirement: ADMIN-01
    verification:
      - kind: integration
        ref: "cmd/pergo/admin_workspace_test.go#TestAdminWorkspaceDetail"
        status: pass
    human_judgment: false
  - id: D3
    description: "Workspace delete with HTMX modal confirmation and row removal"
    requirement: ADMIN-01
    verification:
      - kind: integration
        ref: "cmd/pergo/admin_workspace_test.go#TestAdminWorkspaceConfirmDelete"
        status: pass
      - kind: integration
        ref: "cmd/pergo/admin_workspace_test.go#TestAdminWorkspaceDelete"
        status: pass
    human_judgment: false
  - id: D4
    description: "API key list with active/revoked status badges per workspace"
    requirement: ADMIN-02
    verification:
      - kind: integration
        ref: "cmd/pergo/admin_workspace_test.go#TestAdminAPIKeyList"
        status: pass
    human_judgment: false
  - id: D5
    description: "API key generation showing plaintext once with copy-to-clipboard"
    requirement: ADMIN-02
    verification:
      - kind: integration
        ref: "cmd/pergo/admin_workspace_test.go#TestAdminAPIKeyGenerate"
        status: pass
    human_judgment: false
  - id: D6
    description: "API key revoke with HTMX modal confirmation and badge update"
    requirement: ADMIN-02
    verification:
      - kind: integration
        ref: "cmd/pergo/admin_workspace_test.go#TestAdminAPIKeyConfirmRevoke"
        status: pass
      - kind: integration
        ref: "cmd/pergo/admin_workspace_test.go#TestAdminAPIKeyRevoke"
        status: pass
    human_judgment: false

duration: 3min
completed: 2026-06-25
status: complete
---

# Phase 2 Plan 2: Workspace & API Key Management Summary

**Workspace CRUD and API key lifecycle management via admin panel with HTMX fragment updates, modal confirmations, and one-time plaintext key display**

## Performance

- **Duration:** 3 min
- **Started:** 2026-06-25T23:29:46Z
- **Completed:** 2026-06-25T23:32:10Z
- **Tasks:** 2 (TDD: RED + GREEN)
- **Files modified:** 11

## Accomplishments
- Workspace CRUD handlers: list, create, detail, confirm-delete, delete
- API key management handlers: list, generate, confirm-revoke, revoke
- Reusable ConfirmModal component for HTMX delete/revoke confirmations
- One-time API key plaintext display with copy-to-clipboard on generate
- Active/revoked status badges in API key list
- Workspace repository extended with List (limit) and Delete methods
- API key repository extended with ListByWorkspace method
- Fixed UTF-8 encoding bug in API key generation (hex-encoded random bytes)
- All 9 integration tests pass (workspace CRUD + API key lifecycle)

## Task Commits

Each task was committed atomically:

1. **Task 1: Failing tests for workspace and API key management** - `607cf47` (test)
2. **Task 2: Implement workspace and API key management** - `aa41164` (feat)

_TDD tasks may have multiple commits (test → feat → refactor)_

## Files Created/Modified
- `internal/api/handler/admin/workspace.go` - Workspace CRUD handlers (List, Create, Detail, ConfirmDelete, Delete)
- `internal/api/handler/admin/apikey.go` - API key handlers (List, Generate, ConfirmRevoke, Revoke)
- `templates/pages/workspaces.templ` - Workspace list, create form, detail page, delete confirmation
- `templates/pages/apikeys.templ` - API key list, generate form, revealed display, revoke confirmation
- `templates/components/modal.templ` - Reusable ConfirmModal with hx-delete pattern
- `internal/repository/workspace.go` - Added List (with limit) and Delete methods
- `internal/repository/apikey.go` - Added ListByWorkspace; fixed UTF-8 encoding in key generation
- `cmd/pergo/main.go` - Wired workspace and API key admin routes
- `cmd/pergo/admin_workspace_test.go` - 9 integration tests for workspace and API key flows

## Decisions Made
- **Hex-encoded API key generation:** `string(keyBytes)` produced invalid UTF-8 sequences rejected by PostgreSQL. Fixed with `hex.EncodeToString(keyBytes)` for safe prefix storage.
- **Unique workspace names per test:** Shared test DB caused constraint violations. Test names include UUID suffix for isolation.
- **Empty 200 on workspace delete:** HTMX `hx-swap="outerHTML swap:1s"` removes the row client-side; no content needed in response.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed invalid UTF-8 in API key generation**
- **Found during:** Task 2 (GREEN phase — API key tests failing with "invalid byte sequence for encoding UTF8")
- **Issue:** `apikey.go` used `string(keyBytes)` on random 32 bytes, producing invalid UTF-8. PostgreSQL rejected these when storing the key prefix column.
- **Fix:** Changed to `hex.EncodeToString(keyBytes)` — produces safe hex string, prefix is first 8 hex chars
- **Files modified:** internal/repository/apikey.go
- **Verification:** All 9 tests pass including API key generate and revoke
- **Committed in:** aa41164

**2. [Rule 1 - Bug] Fixed test isolation with unique workspace names**
- **Found during:** Task 2 (GREEN phase — tests failing with "duplicate key value violates unique constraint")
- **Issue:** Tests used static workspace names (e.g. "Detail Workspace") that persisted in shared DB across runs
- **Fix:** `createTestWorkspace` now appends UUID suffix; `TestAdminWorkspaceCreate` uses unique name
- **Files modified:** cmd/pergo/admin_workspace_test.go
- **Verification:** All 9 tests pass consistently across runs
- **Committed in:** aa41164

---

**Total deviations:** 2 auto-fixed (2 bugs)
**Impact on plan:** Both fixes necessary for correctness. UTF-8 encoding is a data integrity issue; test isolation prevents flaky tests. No scope creep.

## Issues Encountered
- PostgreSQL on port 5432 needed `pergo_test` database created before tests could run (test DSN targets port 5432, not the pergo-test-pg container on 5433)

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- Workspace and API key management complete via admin panel
- All CRUD operations available: create, list, view, delete workspaces; generate, list, revoke API keys
- HTMX fragment pattern established for future admin pages
- Ready for Plan 03 (audit log review) which builds on the same admin shell

---
*Phase: 02-admin-shell*
*Completed: 2026-06-25*

## Self-Check: PASSED

All key files exist on disk. Both task commits (607cf47, aa41164) verified in git log. Build and vet pass. All 9 workspace/API key tests pass. Templ generate produced _templ.go files without errors.
