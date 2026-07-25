---
phase: 29-connection-slugs-and-api-channel-routing
plan: 29-04
subsystem: ui
tags: [admin-ui, htmx, templ, slug, go]

requires:
  - phase: 29-connection-slugs-and-api-channel-routing
    provides: "030_connection_slugs.sql database migration, slug package, ConnectionRepository GetBySlug and UpdateSlug methods"
provides:
  - "Auto-slug generation with suffix collision handling on connection creation in DeviceHandler.Create"
  - "Slug column and inline slug edit form in ConnectionTable templates"
  - "POST /admin/devices/:id/slug endpoint for updating connection slugs"
affects: []

tech-stack:
  added: []
  patterns: [auto-slug-collision-suffixing, inline-htmx-form-editing]

key-files:
  created: []
  modified:
    - internal/api/handler/admin/device.go
    - templates/pages/devices.templ
    - templates/pages/devices_templ.go
    - cmd/pergo/main.go

key-decisions:
  - "DeviceHandler.Create auto-generates slug from name and increments numeric suffixes (-2, -3) when collisions occur within the workspace"
  - "Added POST /admin/devices/:id/slug route and handler to support editing slugs via the admin UI"
  - "Renders inline form in ConnectionTable allowing operators to edit and save connection slugs with Enter"

patterns-established:
  - "Inline form editing via HTMX targeting table container for instant table refresh"

requirements-completed: []

coverage:
  - id: D1
    description: "DeviceHandler.Create populates slug field with collision suffix handling"
    verification:
      - kind: unit
        ref: "internal/api/handler/admin/device_test.go"
        status: pass
    human_judgment: false
  - id: D2
    description: "ConnectionTable UI displays slug column and provides inline editing"
    verification:
      - kind: other
        ref: "templates/pages/devices.templ"
        status: pass
    human_judgment: false

duration: 5 min
completed: 2026-07-25
status: complete
---

# Plan 29-04: Admin UI Slug Auto-Generation and Display Summary

**Automatic connection slug generation on creation with collision suffixing and admin UI display with inline editing**

## Performance

- **Duration:** 5 min
- **Started:** 2026-07-25T14:43:04Z
- **Completed:** 2026-07-25T14:47:58Z
- **Tasks:** 2
- **Files modified:** 4

## Accomplishments
- Implemented automatic slug generation in `DeviceHandler.Create` using `slug.Generate(name)` and collision handling (`baseSlug-2`, `baseSlug-3`) per workspace.
- Added `POST /admin/devices/:id/slug` endpoint and handler method `UpdateSlug` to allow updating connection slugs.
- Added `Slug (API)` column to `ConnectionTable` and implemented an inline HTMX edit form in `templates/pages/devices.templ`.
- Executed `templ generate` to compile template changes.

## Task Commits

1. **Task 04.1 & Task 04.2: Admin UI Slug Auto-Generation & Display** - `6ae4e06` (feat)

## Files Created/Modified
- `internal/api/handler/admin/device.go` - Auto-slug generation in `Create` and `UpdateSlug` endpoint
- `templates/pages/devices.templ` - Added `Slug (API)` table column and inline form
- `templates/pages/devices_templ.go` - Generated template code
- `cmd/pergo/main.go` - Registered `/admin/devices/:id/slug` route

## Decisions Made
- Used `GetBySlug` to detect collisions on creation and automatically increment numeric suffixes before inserting into the database.
- Used inline HTMX form targeting `#connections-table-container` for instant feedback on slug updates.

## Deviations from Plan
None - plan executed exactly as written.

## Issues Encountered
None.

## Next Phase Readiness
- Phase 29 is fully executed! All 4 plans complete.

---
*Phase: 29-connection-slugs-and-api-channel-routing*
*Completed: 2026-07-25*
