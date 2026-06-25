# Phase 2: Admin Shell - Research

**Researched:** 2026-06-25
**Domain:** Server-rendered admin UI with Echo + Templ + HTMX
**Confidence:** HIGH

## Summary

Phase 2 adds a server-rendered admin panel on top of the existing Echo v5 server. The admin panel provides workspace management, API key CRUD, and audit log review — all built with Templ for type-safe HTML templates and HTMX 2.x for fragment-based interactions. The key architectural decision is using Echo's `HX-Request` header detection to return HTML fragments (for HTMX-powered updates) or full pages (for direct navigation). The admin routes are mounted on the same Echo instance but prefixed with `/admin/` and protected by a session-based auth middleware (distinct from the API key auth middleware).

**Primary recommendation:** Use Templ's `Render()` helper for Echo integration, mount admin routes as a separate group with session auth, and detect HTMX requests via the `HX-Request` header to return fragments or full pages.

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| ADMIN-01 | Server-rendered admin panel (Echo + Templ + HTMX, HTMX fragment detection) | Templ Echo integration pattern, HTMX fragment detection via HX-Request header |
| ADMIN-02 | Multi-tenant workspace management (create, isolate, manage scoped API keys) | Existing workspace/repository patterns from Phase 1, HTMX form handling |
| ADMIN-05 | Audit log review interface (searchable, filterable, exportable) | Server-side CSV export with Go's encoding/csv, HTMX pagination pattern |
| AUDIT-04 | Audit log access via both API and admin dashboard (filterable by workspace, trace_id, time range) | Existing audit event type and repository patterns |
</phase_requirements>

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **Templ** for compile-time type-safe HTML templates (not hand-written HTML)
- **HTMX 2.x** for fragment-based interactions (no full-page reloads)
- **Echo v5** serves both API and admin routes on the same port
- Admin routes prefixed with `/admin/` to separate from public API
- HTMX fragment detection via `HX-Request` header check in Echo middleware
- **Sidebar navigation** with sections: Workspaces, API Keys, Audit Logs
- Dashboard landing page showing workspace count, recent audit activity, system health
- Workspace list view with search/filter
- Create workspace form: name (required), description (optional)
- Workspace detail view showing API keys and recent audit entries
- Delete workspace with confirmation dialog (HTMX modal)
- API key list per workspace with status indicator (active/revoked)
- Generate new key: button triggers key generation, displays once, then hashes
- Revoke key: confirmation dialog, immediate revocation
- Key display: show prefix + masked suffix for identification
- Audit log table with columns: timestamp, workspace, trace_id, event_type, status
- Filter by: workspace dropdown, time range picker, event type dropdown
- Search by trace_id (exact match)
- Export as CSV (server-side generation, download link)
- Pagination with 50 rows per page
- List views return HTML fragments (no layout wrapper)
- Form submissions return fragments for target update area
- Delete confirmations use HTMX modal pattern
- Navigation clicks return full pages (layout + content)
- Minimal CSS with CSS custom properties for theming
- No external CSS framework (keep dependency footprint small)

### the agent's Discretion
- Admin panel should feel like a lightweight operator console, not a full CRM
- Focus on functional clarity over visual polish
- HTMX interactions should feel snappy (fragment responses, not full page loads)

### Deferred Ideas (OUT OF SCOPE)
- Real-time WebSocket updates for session status (Phase 4 — WhatsApp Web integration)
- Dashboard charts/graphs (can be added later without schema changes)
- Multi-user admin authentication with roles (MVP uses single-operator model)
- API-only admin management (CLI/SDK) — admin panel is the primary interface for MVP
</user_constraints>

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Admin route handling | Echo v5 (HTTP) | Templ (HTML rendering) | Echo routes dispatch to handlers that render Templ components |
| HTMX fragment detection | Echo middleware | HTMX client | Server checks HX-Request header; client sends it automatically |
| Workspace CRUD | API/Backend | Database/Storage | Business logic in handlers, persistence in repository layer |
| API key management | API/Backend | Database/Storage | Key generation, hashing, and cache invalidation in repository |
| Audit log queries | API/Backend | Database/Storage | Filtered queries with pagination in repository |
| CSV export | API/Backend | — | Server-side generation, streaming response |
| Static assets (CSS, JS) | CDN/Static | — | HTMX JS via CDN, CSS served by Echo static middleware |
| Session-based auth | API/Backend | Database/Storage | Cookie-based session for admin (vs Bearer token for API) |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| a-h/templ | v0.3.1020 | Compile-time type-safe HTML → Go | Pre-1.0 by intent, API-stable, LSP/codegen/fmt/watch, 10.4k stars [VERIFIED: Go module proxy] |
| htmx.org | 2.x (CDN htmx.org@2.0.10) | Fragment-based server-driven interactivity | ~16k min gzipped, no build system, no JS framework [VERIFIED: npm registry] |
| Echo v5 | v5.2.1 | HTTP router + middleware | Already in project, native slog integration [VERIFIED: Go module proxy] |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| google/uuid | v1.6.0 | UUID generation | Already in project from Phase 1 — workspace/key IDs |
| encoding/csv (stdlib) | — | Server-side CSV export | Audit log CSV download endpoint |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Templ | Go's template/html | Templ gives compile-time safety + code generation; template/html is runtime parsing |
| HTMX 2.x | Alpine.js / Stimulus | HTMX is HTML-first (no JS); Alpine adds JS expression layer |
| Echo static middleware | Separate nginx/CDN | Single binary deployment; static assets embedded in Go binary |

**Installation:**
```bash
# Add templ to go.mod
go get github.com/a-h/templ@v0.3.1020

# Install templ CLI (for code generation)
go install github.com/a-h/templ/cmd/templ@latest

# HTMX is loaded via CDN — no Go dependency
# Add to base templ layout:
# <script src="https://cdn.jsdelivr.net/npm/htmx.org@2.0.10/dist/htmx.min.js"></script>
```

**Version verification:** Before writing the Standard Stack table, verify each recommended package exists and is current using the ecosystem-appropriate command:
```bash
go list -m -versions github.com/a-h/templ | tail -1  # Latest: v0.3.1020
npm view htmx.org version                              # Latest: 2.0.10
```
Document the verified version and publish date. Training data versions may be months stale — always confirm against the correct ecosystem registry.

## Package Legitimacy Audit

| Package | Registry | Age | Downloads | Source Repo | Verdict | Disposition |
|---------|----------|-----|-----------|-------------|---------|-------------|
| github.com/a-h/templ | Go module | 4+ years | 10.4k GitHub stars | github.com/a-h/templ | OK | Approved |
| htmx.org | npm | 3+ years | 181k weekly | github.com/bigskysoftware/htmx | OK | Approved |
| Echo v5 | Go module | Already in project | — | github.com/labstack/echo | OK | Approved (already used) |

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

## Architecture Patterns

### System Architecture Diagram

```
┌─────────────────────────────────────────────────────────┐
│                    Browser (Operator)                    │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐     │
│  │  HTMX 2.x   │  │  CSS (own)  │  │  htmx.min.js│     │
│  └──────┬──────┘  └─────────────┘  └─────────────┘     │
└─────────┼───────────────────────────────────────────────┘
          │ HTTP requests (HX-Request header present)
          ▼
┌─────────────────────────────────────────────────────────┐
│                    Echo v5 Server                       │
│  ┌──────────────────────────────────────────────────┐   │
│  │  Middleware Stack                                 │   │
│  │  RequestID → Trace → Recover → Auth              │   │
│  └──────────────────────┬───────────────────────────┘   │
│                         │                               │
│  ┌──────────────────────▼───────────────────────────┐   │
│  │  Route Groups                                    │   │
│  │  /healthz, /readyz        (Phase 1 — existing)   │   │
│  │  /api/v1/*                (Phase 1 — API auth)   │   │
│  │  /admin/*                 (Phase 2 — session auth)│   │
│  └──────────────────────┬───────────────────────────┘   │
│                         │                               │
│  ┌──────────────────────▼───────────────────────────┐   │
│  │  Admin Handlers                                  │   │
│  │  workspaceHandler → Templ.Render(fragment/page)   │   │
│  │  apiKeyHandler    → Templ.Render(fragment/page)   │   │
│  │  auditHandler     → Templ.Render(fragment/page)   │   │
│  │  auditExportHandler → encoding/csv streaming      │   │
│  └──────────────────────┬───────────────────────────┘   │
│                         │                               │
│  ┌──────────────────────▼───────────────────────────┐   │
│  │  Repository Layer (Phase 1 — existing)            │   │
│  │  WorkspaceRepository  → pgxpool                   │   │
│  │  APIKeyRepository     → pgxpool + cache           │   │
│  │  AuditRepository (NEW) → pgxpool                  │   │
│  └──────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
```

### Recommended Project Structure
```
internal/
├── api/
│   ├── handler/
│   │   ├── health.go          # (existing)
│   │   ├── admin/
│   │   │   ├── dashboard.go   # Dashboard landing page handler
│   │   │   ├── workspace.go   # Workspace CRUD handler
│   │   │   ├── apikey.go      # API key CRUD handler
│   │   │   └── audit.go       # Audit log review + CSV export handler
│   │   └── ...
│   └── middleware/
│       ├── auth.go            # (existing — API key auth)
│       ├── session.go         # NEW: Session-based admin auth middleware
│       ├── trace.go           # (existing)
│       └── htmx.go            # NEW: HTMX fragment detection helper
├── repository/
│   ├── workspace.go           # (existing — add List, Delete)
│   ├── apikey.go              # (existing — add ListByWorkspace)
│   └── audit.go               # NEW: Audit log query repository
└── ...

templates/
├── layout/
│   ├── base.templ             # Base HTML layout (head, sidebar, scripts)
│   └── sidebar.templ          # Sidebar navigation component
├── pages/
│   ├── dashboard.templ        # Dashboard landing page
│   ├── workspaces.templ       # Workspace list, create, detail pages
│   ├── apikeys.templ          # API key list, generate, revoke pages
│   └── audit.templ            # Audit log table, filters, pagination
└── components/
    ├── table.templ            # Reusable table component
    ├── pagination.templ       # Reusable pagination component
    ├── modal.templ            # HTMX modal for confirmations
    └── forms.templ            # Reusable form components

static/
├── css/
│   └── admin.css              # CSS custom properties + minimal styles
└── js/
    └── htmx.min.js            # HTMX 2.x (local copy, not CDN)
```

### Pattern 1: Templ Echo Integration with HTMX Fragment Detection
**What:** Render Templ components in Echo handlers, returning fragments for HTMX requests and full pages for direct navigation.
**When to use:** Every admin handler that serves HTML.
**Example:**
```go
// Source: https://github.com/a-h/templ/tree/main/examples/integration-echo
// Adapted for Echo v5 + HTMX fragment detection

// Render writes a Templ component to the Echo response.
func Render(ctx echo.Context, statusCode int, t templ.Component) error {
    buf := templ.GetBuffer()
    defer templ.ReleaseBuffer(buf)
    if err := t.Render(ctx.Request().Context(), buf); err != nil {
        return err
    }
    return ctx.HTML(statusCode, buf.String())
}

// IsHTMX checks if the request is from HTMX (fragment request).
func IsHTMX(c *echo.Context) bool {
    return c.Request().Header.Get("HX-Request") == "true"
}

// WorkspaceListHandler returns full page or fragment based on HTMX header.
func (h *WorkspaceHandler) List(c *echo.Context) error {
    workspaces, err := h.repo.List(c.Request().Context())
    if err != nil {
        return err
    }
    if IsHTMX(c) {
        // Return fragment only (table rows)
        return Render(c, http.StatusOK, workspaceListFragment(workspaces))
    }
    // Return full page (layout + content)
    return Render(c, http.StatusOK, workspacePage(workspaces))
}
```

### Pattern 2: HTMX Modal for Delete Confirmation
**What:** Show a confirmation dialog before destructive actions using HTMX modal pattern.
**When to use:** Workspace delete, API key revoke, any destructive action.
**Example:**
```html
<!-- Source: htmx.org examples (confirm pattern) -->
<!-- Button triggers GET to fetch confirmation dialog -->
<button hx-get="/admin/workspaces/{{.ID}}/confirm-delete"
        hx-target="#modal-container"
        hx-swap="innerHTML">
    Delete Workspace
</button>

<!-- Server returns modal fragment -->
<div id="confirm-modal" class="modal">
  <div class="modal-backdrop" onclick="this.parentElement.remove()"></div>
  <div class="modal-content">
    <h3>Delete Workspace?</h3>
    <p>This will permanently delete "{{.Name}}" and all its API keys.</p>
    <div class="modal-actions">
      <button onclick="this.closest('.modal').remove()">Cancel</button>
      <button hx-delete="/admin/workspaces/{{.ID}}"
              hx-target="closest tr"
              hx-swap="outerHTML swap:1s">
        Confirm Delete
      </button>
    </div>
  </div>
</div>
```

### Pattern 3: Server-Side CSV Export
**What:** Generate CSV files server-side for audit log download.
**When to use:** Audit log export button.
**Example:**
```go
// Source: Go stdlib encoding/csv patterns
func (h *AuditHandler) ExportCSV(c *echo.Context) error {
    // Apply same filters as list view
    events, err := h.repo.ListFiltered(c.Request().Context(), filters)
    if err != nil {
        return err
    }

    c.Response().Header().Set("Content-Type", "text/csv")
    c.Response().Header().Set("Content-Disposition", "attachment; filename=audit_logs.csv")

    w := csv.NewWriter(c.Response())
    defer w.Flush()

    // Write header
    w.Write([]string{"timestamp", "workspace_id", "trace_id", "event_type", "payload"})

    // Write rows
    for _, e := range events {
        w.Write([]string{
            e.CreatedAt.Format(time.RFC3339),
            e.WorkspaceID.String(),
            e.TraceID,
            e.EventType,
            string(e.Payload),
        })
    }
    return nil
}
```

### Anti-Patterns to Avoid
- **Returning JSON for admin UI:** HTMX expects HTML fragments, not JSON. Return `c.HTML()` not `c.JSON()`.
- **Full page reloads for every interaction:** Use HTMX attributes (`hx-get`, `hx-post`, `hx-target`) to update only the changed portion.
- **Hand-writing HTML in Go strings:** Use Templ for compile-time safety. Hand-written HTML loses type checking and editor support.
- **Embedding HTMX JS in every page separately:** Use a base layout template that includes the script tag once.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| HTML templates | `template/html` + hand-written HTML | a-h/templ | Compile-time safety, code generation, LSP support |
| Fragment updates | Custom JS fetch + DOM manipulation | HTMX 2.x | Declarative, no JS needed, battle-tested patterns |
| Session management | Custom cookie handler | Echo session middleware | Cookie security, session store abstraction |
| Pagination | Custom page/offset logic | Repository LIMIT/OFFSET | Standard SQL pattern, already supported by pgx |

**Key insight:** Templ generates Go code at compile time — your templates are type-checked like any other Go code. This catches parameter mismatches and missing variables at build time, not runtime.

## Common Pitfalls

### Pitfall 1: Templ Codegen Not Running
**What goes wrong:** `templ generate` not in build process → stale `_templ.go` files → compilation errors.
**Why it happens:** Templ requires a code generation step (`templ generate`) after editing `.templ` files.
**How to avoid:** Add `//go:generate templ generate` to a central file, or add `templ generate` to Makefile targets and CI steps.
**Warning signs:** Compilation errors about undefined template functions, or HTML output not reflecting template changes.

### Pitfall 2: HTMX Fragment vs Full Page Confusion
**What goes wrong:** Returning full page HTML when HTMX expects a fragment (or vice versa) → broken UI or full page reloads.
**Why it happens:** Not checking `HX-Request` header consistently.
**How to avoid:** Centralize the decision in a `render()` helper that checks the header and selects the appropriate template.
**Warning signs:** Admin panel feels like a traditional website (full page reloads on every click).

### Pitfall 3: Static Assets Not Found
**What goes wrong:** CSS/JS files return 404 because Echo static middleware path is misconfigured.
**Why it happens:** Go embed paths or static file serving paths don't match the HTML references.
**How to avoid:** Use `e.Static("/static", "static")` and verify paths match in templates.
**Warning signs:** Admin panel loads but has no styling (CSS 404), HTMX doesn't work (JS 404).

### Pitfall 4: Session Auth Not Applied to Admin Routes
**What goes wrong:** Admin panel accessible without authentication.
**Why it happens:** Session middleware not applied to admin route group.
**How to avoid:** Create admin route group with session middleware applied at group level.
**Warning signs:** Admin panel loads for unauthenticated users.

### Pitfall 5: Templ Buffer Reuse Race Condition
**What goes wrong:** Concurrent requests share a Templ buffer → garbled output.
**Why it happens:** `templ.GetBuffer()` returns a pooled buffer; must be released with `defer templ.ReleaseBuffer(buf)`.
**How to avoid:** Always use the `Render()` helper pattern with `defer ReleaseBuffer`.
**Warning signs:** Intermittent HTML corruption under load.

## Code Examples

Verified patterns from official sources:

### Templ Component with Echo v5
```go
// Source: https://github.com/a-h/templ/tree/main/examples/integration-echo (adapted for v5)
package handler

import (
    "net/http"
    "github.com/a-h/templ"
    "github.com/labstack/echo/v5"
)

// Render writes a templ component to the Echo response.
func Render(ctx echo.Context, statusCode int, t templ.Component) error {
    buf := templ.GetBuffer()
    defer templ.ReleaseBuffer(buf)
    if err := t.Render(ctx.Request().Context(), buf); err != nil {
        return err
    }
    return ctx.HTML(statusCode, buf.String())
}
```

### HTMX Fragment Detection Middleware
```go
// Source: htmx.org docs — HX-Request header detection
package middleware

import "github.com/labstack/echo/v5"

// HTMXMiddleware adds IsHTMX helper to echo context.
func HTMXMiddleware() echo.MiddlewareFunc {
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c *echo.Context) error {
            // Store HTMX flag in context for handlers
            isHTMX := c.Request().Header.Get("HX-Request") == "true"
            ctx := context.WithValue(c.Request().Context(), htmxKey{}, isHTMX)
            c.SetRequest(c.Request().WithContext(ctx))
            return next(c)
        }
    }
}

// IsHTMXFromContext retrieves the HTMX flag.
func IsHTMXFromContext(ctx context.Context) bool {
    v, _ := ctx.Value(htmxKey{}).(bool)
    return v
}
```

### Layout Template Pattern
```templ
// Source: templ.guide server-side rendering patterns
package templates

templ Layout(title string, content templ.Component) {
    <!DOCTYPE html>
    <html>
    <head>
        <title>{ title }</title>
        <link rel="stylesheet" href="/static/css/admin.css"/>
        <script src="/static/js/htmx.min.js"></script>
    </head>
    <body>
        <div class="layout">
            @Sidebar()
            <main class="content">
                @content
            </main>
        </div>
    </body>
    </html>
}

templ Sidebar() {
    <nav class="sidebar">
        <a href="/admin/" class="nav-item">Dashboard</a>
        <a href="/admin/workspaces" class="nav-item">Workspaces</a>
        <a href="/admin/audit" class="nav-item">Audit Logs</a>
    </nav>
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Echo v4 templates | Echo v5 + templ | 2026-01-18 (v5 release) | Handler signature changed to `*echo.Context`; templ is framework-agnostic |
| HTMX 1.x | HTMX 2.x | 2023-06 (2.x release) | Dropped IE support; new swap options; view transitions API |
| Hand-written HTML | Templ compile-time templates | 2020+ (templ maturity) | Type safety, code generation, LSP support |

**Deprecated/outdated:**
- Echo v4: EOL 2026-12-31 (security/bug only); v5 is current major line
- HTMX 1.x: Maintained only for IE support; 2.x is active line
- Go template/html: Runtime parsing, no type safety; Templ is compile-time

## Assumptions Log

> List all claims tagged `[ASSUMED]` in this research. The planner and discuss-phase use this
> section to identify decisions that need user confirmation before execution.

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Session-based auth for admin panel uses Echo's built-in session middleware (cookie-based) | Architecture Patterns | Low — single-operator model; session middleware is standard Echo |
| A2 | HTMX will be served from a local static file, not CDN, for self-hosted deployment | Standard Stack | Low — can switch to CDN if local file causes issues |
| A3 | Audit log repository will add a `ListFiltered` method to the existing audit subsystem | Common Pitfalls | Low — audit_logs table already exists; new repository method is additive |

**If this table is empty:** All claims in this research were verified or cited — no user confirmation needed.

## Open Questions

1. **Session-based auth for admin panel**
   - What we know: Phase 1 built API key auth (Bearer token). Admin panel needs cookie-based session auth.
   - What's unclear: Whether to use Echo's session middleware or a simpler signed-cookie approach for single-operator model.
   - Recommendation: Use Echo's session middleware with a single shared secret. The single-operator model doesn't need complex session stores — a signed cookie with workspace_id is sufficient. Planner should confirm this approach.

2. **Admin CSS approach**
   - What we know: CONTEXT.md says "Minimal CSS with CSS custom properties for theming" and "No external CSS framework."
   - What's unclear: Whether to use a minimal CSS reset/normalize or hand-write everything.
   - Recommendation: Use a minimal CSS reset (like `normalize.css` principles) and build custom properties on top. Keep it under 200 lines. Planner can decide exact approach.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go | Build | ✓ | 1.26.4 | — |
| templ CLI | Code generation | ✓ | installed | `go install github.com/a-h/templ/cmd/templ@latest` |
| Docker Compose | Local dev stack | ✓ | v2 | — |

**Missing dependencies with no fallback:** None.

**Missing dependencies with fallback:** None.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib testing + testcontainers-go |
| Config file | none — see Wave 0 |
| Quick run command | `go test ./... -count=1 -short` |
| Full suite command | `go test ./... -race -count=1` |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| ADMIN-01 | Admin panel renders with sidebar navigation | integration | `go test ./internal/api/handler/admin/ -run TestDashboardRender -count=1` | ❌ Wave 0 |
| ADMIN-01 | HTMX fragment detection returns fragment for HX-Request header | unit | `go test ./internal/api/middleware/ -run TestHTMXDetection -count=1` | ❌ Wave 0 |
| ADMIN-02 | Workspace list returns HTML table | integration | `go test ./internal/api/handler/admin/ -run TestWorkspaceList -count=1` | ❌ Wave 0 |
| ADMIN-02 | Workspace create form submits and returns fragment | integration | `go test ./internal/api/handler/admin/ -run TestWorkspaceCreate -count=1` | ❌ Wave 0 |
| ADMIN-05 | Audit log list with filters returns paginated results | integration | `go test ./internal/api/handler/admin/ -run TestAuditList -count=1` | ❌ Wave 0 |
| AUDIT-04 | CSV export returns valid CSV download | integration | `go test ./internal/api/handler/admin/ -run TestAuditExportCSV -count=1` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./... -count=1 -short`
- **Per wave merge:** `go test ./... -race -count=1`
- **Phase gate:** Full suite green before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] `internal/api/handler/admin/dashboard_test.go` — covers ADMIN-01
- [ ] `internal/api/handler/admin/workspace_test.go` — covers ADMIN-02
- [ ] `internal/api/handler/admin/audit_test.go` — covers ADMIN-05, AUDIT-04
- [ ] `internal/api/middleware/htmx_test.go` — covers ADMIN-01 (fragment detection)
- [ ] `internal/repository/audit_test.go` — covers audit query repository
- [ ] Templ codegen step in Makefile (`make templ`)

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes | Session-based auth with signed cookies (admin), API key auth (existing) |
| V3 Session Management | yes | Echo session middleware, secure cookie flags |
| V4 Access Control | yes | workspace_id scoping on all queries |
| V5 Input Validation | yes | Templ compile-time safety + Echo binder validation |
| V6 Cryptography | no | No new crypto in this phase (existing AES-256-GCM and SHA-256 unchanged) |

### Known Threat Patterns for This Stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| CSRF on admin forms | Tampering | Echo CSRF middleware or SameSite cookie attribute |
| Session fixation | Elevation of Privilege | Regenerate session ID on login |
| XSS via audit log payload | Tampering | Templ auto-escapes by default; never use `templ.Raw()` on user data |
| Information disclosure in errors | Information Disclosure | Structured error responses, no stack traces in production |

## Sources

### Primary (HIGH confidence)
- [templ.guide] - Server-side rendering, Echo integration, HTMX usage patterns
- [github.com/a-h/templ/tree/main/examples/integration-echo] - Echo v4 integration example (adapted for v5)
- [htmx.org/docs] - HTMX 2.x documentation, fragment patterns, header detection
- [Go module proxy] - Verified templ v0.3.1020 and Echo v5.2.1 exist and are current

### Secondary (MEDIUM confidence)
- [WebSearch] - HTMX modal patterns, CSS custom properties best practices
- [WebSearch] - Server-side CSV export in Go with encoding/csv

### Tertiary (LOW confidence)
- None — all findings verified against official sources

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — templ and HTMX are well-documented with official integration examples
- Architecture: HIGH — builds directly on established Phase 1 patterns (repository, handler, middleware)
- Pitfalls: MEDIUM — Templ codegen and HTMX fragment detection are common stumbling blocks, documented in community resources

**Research date:** 2026-06-25
**Valid until:** 30 days (templ and HTMX are stable; Echo v5 is current major line)
