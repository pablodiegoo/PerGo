---
phase: 02-admin-shell
plan: 01
subsystem: ui
tags: [templ, htmx, session-auth, admin-panel, sidebar, dashboard]

requires:
  - phase: 01-Foundation
    provides: "Echo v5 server scaffold, pgxpool dual-access PostgreSQL, workspace/API key repositories"
  - phase: 01-Foundation
    provides: "Auth middleware, audit batch writer, config loading"
provides:
  - "Session-based admin auth middleware with HMAC-signed cookies"
  - "HTMX fragment detection middleware with templ Render helper"
  - "Base HTML layout with sidebar navigation (templ compile-time templates)"
  - "Login page with password validation via PERGO_ADMIN_PASSWORD env var"
  - "Dashboard page showing workspace count and recent audit activity"
  - "Admin CSS with custom properties for theming"
  - "Workspace repository List/Count methods for dashboard"
  - "Audit log querier for recent entries"
affects: [03-Ingest-API, 04-WhatsApp-Web]

tech-stack:
  added: [a-h/templ, htmx.org]
  patterns: [templ-echo-integration, session-signed-cookie, htmx-fragment-detection, sidebar-navigation]

key-files:
  created:
    - internal/api/middleware/session.go
    - internal/api/middleware/htmx.go
    - internal/api/handler/admin/dashboard.go
    - internal/api/handler/admin/login.go
    - internal/platform/audit/querier.go
    - templates/layout/base.templ
    - templates/layout/sidebar.templ
    - templates/pages/login.templ
    - templates/pages/dashboard.templ
    - static/css/admin.css
  modified:
    - cmd/pergo/main.go
    - internal/repository/workspace.go
    - go.mod

key-decisions:
  - "HMAC-signed session cookies over Echo session middleware — simpler single-operator model"
  - "Cached session secret via sync.Once — avoids secret regeneration across requests"
  - "Login routes on separate unprotected group — prevents redirect loop"
  - "Dashboard handler nil-guards for DB dependencies — graceful degradation in tests"

patterns-established:
  - "Templ Echo integration: Render() helper with GetBuffer/ReleaseBuffer"
  - "HTMX fragment detection: HX-Request header stored in context via middleware"
  - "Session auth: signed cookie with HMAC-SHA256, HttpOnly + SameSite=Lax"
  - "Admin route groups: public (login/logout) vs protected (dashboard)"
  - "Sidebar navigation pattern for admin panel"

requirements-completed: [ADMIN-01]

coverage:
  - id: D1
    description: "Admin panel accessible at /admin/ with session authentication — unauthenticated redirect to login"
    requirement: ADMIN-01
    verification:
      - kind: integration
        ref: "cmd/pergo/admin_test.go#TestAdminRedirectUnauthenticated"
        status: pass
    human_judgment: false
  - id: D2
    description: "Login page renders at /admin/login with password form"
    requirement: ADMIN-01
    verification:
      - kind: integration
        ref: "cmd/pergo/admin_test.go#TestAdminLoginPage"
        status: pass
    human_judgment: false
  - id: D3
    description: "Login with correct password sets session cookie and redirects to dashboard"
    requirement: ADMIN-01
    verification:
      - kind: integration
        ref: "cmd/pergo/admin_test.go#TestAdminLoginSuccess"
        status: pass
    human_judgment: false
  - id: D4
    description: "Login with wrong password returns 401 with error message"
    requirement: ADMIN-01
    verification:
      - kind: integration
        ref: "cmd/pergo/admin_test.go#TestAdminLoginWrongPassword"
        status: pass
    human_judgment: false
  - id: D5
    description: "Dashboard renders with sidebar navigation linking to Dashboard, Workspaces, Audit Logs"
    requirement: ADMIN-01
    verification:
      - kind: integration
        ref: "cmd/pergo/admin_test.go#TestAdminDashboardAuthenticated"
        status: pass
    human_judgment: false
  - id: D6
    description: "Dashboard shows workspace count and audit section"
    requirement: ADMIN-01
    verification:
      - kind: integration
        ref: "cmd/pergo/admin_test.go#TestAdminDashboardContent"
        status: pass
    human_judgment: false
  - id: D7
    description: "HTMX fragment detection returns HTML fragment for HX-Request header"
    requirement: ADMIN-01
    verification:
      - kind: integration
        ref: "cmd/pergo/admin_test.go#TestAdminHTMXFragment"
        status: pass
    human_judgment: false

duration: 15min
completed: 2026-06-25
status: complete
---

# Phase 2 Plan 1: Admin Shell Summary

**Server-rendered admin panel with templ + HTMX, HMAC-signed session auth, sidebar navigation, login/logout flow, and dashboard landing page with workspace count and audit activity**

## Performance

- **Duration:** 15 min
- **Started:** 2026-06-25T19:24:15Z
- **Completed:** 2026-06-25T19:39:40Z
- **Tasks:** 2 (TDD: RED + GREEN)
- **Files created:** 15

## Accomplishments
- Session-based admin auth with HMAC-signed cookies (HttpOnly, SameSite=Lax)
- Templ compile-time templates: base layout, sidebar navigation, login page, dashboard
- HTMX fragment detection via HX-Request header — dashboard returns fragments for snappy interactions
- Login/logout flow: password validation against PERGO_ADMIN_PASSWORD env var
- Dashboard page showing workspace count and recent audit activity
- Minimal CSS with custom properties — sidebar layout, responsive, no framework
- Workspace repository extended with List/Count methods
- Audit log querier for recent entries

## Task Commits

Each task was committed atomically:

1. **Task 1: Failing end-to-end test for admin shell** - `9344b84` (test)
2. **Task 2: Implement admin shell — templ, session auth, layout, dashboard, HTMX, CSS** - `73fee8e` (feat)

_TDD tasks may have multiple commits (test → feat → refactor)_

## Files Created/Modified
- `internal/api/middleware/session.go` - Session auth middleware with HMAC-signed cookies
- `internal/api/middleware/htmx.go` - HTMX fragment detection + templ Render helper
- `internal/api/handler/admin/dashboard.go` - Dashboard handler with workspace count and audit
- `internal/api/handler/admin/login.go` - Login page, login POST, logout handlers
- `internal/platform/audit/querier.go` - Audit log read-only queries for dashboard
- `templates/layout/base.templ` - Base HTML layout with head, sidebar, scripts
- `templates/layout/sidebar.templ` - Sidebar navigation component
- `templates/pages/login.templ` - Login form with password input
- `templates/pages/dashboard.templ` - Dashboard with stats grid and activity section
- `static/css/admin.css` - Minimal CSS with custom properties for theming
- `cmd/pergo/main.go` - Wired admin routes (public + protected groups)
- `internal/repository/workspace.go` - Added List/Count methods
- `go.mod` / `go.sum` - Added a-h/templ dependency

## Decisions Made
- **HMAC-signed cookies over Echo session middleware:** Simpler single-operator model; no external session store needed. Signed with SHA-256 HMAC, base64-encoded.
- **Cached session secret via sync.Once:** Avoids regenerating random secret on each request. Secret persists for process lifetime.
- **Login routes on separate unprotected group:** Prevents redirect loop when accessing /admin/login without a session.
- **Dashboard nil-guards for DB dependencies:** Graceful degradation when PostgreSQL unavailable (tests without Docker Compose).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Session secret caching with sync.Once**
- **Found during:** Task 2 (GREEN phase — tests failing with 302 on dashboard access)
- **Issue:** `getSessionSecret()` generated a new random secret on each call, causing middleware and login handler to use different secrets for signing/verification
- **Fix:** Added `sync.Once` to cache the generated secret for process lifetime
- **Files modified:** internal/api/middleware/session.go
- **Verification:** All 7 admin tests pass
- **Committed in:** 73fee8e

**2. [Rule 3 - Blocking] Login routes separated from protected admin group**
- **Found during:** Task 2 (GREEN phase — login page returning 302 redirect loop)
- **Issue:** Session auth middleware applied to entire /admin group caused redirect loop for /admin/login
- **Fix:** Split admin routes into public (login/logout) and protected (dashboard) groups
- **Files modified:** cmd/pergo/main.go, cmd/pergo/admin_test.go
- **Verification:** Login page accessible without session, dashboard requires session
- **Committed in:** 73fee8e

**3. [Rule 2 - Missing Critical] Dashboard nil-guards for DB dependencies**
- **Found during:** Task 2 (GREEN phase — nil pointer dereference in tests without PostgreSQL)
- **Issue:** DashboardHandler panicked on nil Workspaces/Audit dependencies when PostgreSQL unavailable
- **Fix:** Added nil checks before querying workspace count and audit entries
- **Files modified:** internal/api/handler/admin/dashboard.go
- **Verification:** Tests run gracefully without Docker Compose
- **Committed in:** 73fee8e

**4. [Rule 1 - Bug] Test stubs replaced with real handlers**
- **Found during:** Task 2 (GREEN phase — test stubs returning 501 instead of real behavior)
- **Issue:** RED phase test stubs were not replaced with real handler implementations
- **Fix:** Updated admin_test.go to use real LoginPage, LoginPost, Logout, and DashboardHandler
- **Files modified:** cmd/pergo/admin_test.go
- **Verification:** All 7 tests pass with real implementations
- **Committed in:** 73fee8e

---

**Total deviations:** 4 auto-fixed (1 bug, 1 missing critical, 2 blocking)
**Impact on plan:** All deviations necessary for correct behavior. Session secret caching and route separation are architectural correctness fixes. No scope creep.

## Issues Encountered
- Echo v5 does not have `middleware.Session` like v4 — implemented custom HMAC-signed cookie approach
- `postgres.NewPool` returns `*pgxpool.Pool`, not a wrapper type — test helper adjusted

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- Admin shell foundation complete: login, session auth, dashboard, sidebar navigation, HTMX fragment detection
- Ready for Plan 02 (workspace management) which builds CRUD operations on top of this shell
- Static CSS and templ templates provide the foundation for all subsequent admin pages

---
*Phase: 02-admin-shell*
*Completed: 2026-06-25*

## Self-Check: PASSED

All key files exist on disk. Both task commits (9344b84, 73fee8e) verified in git log. Build and vet pass. All 7 admin tests pass. Templ generate produced _templ.go files without errors.
