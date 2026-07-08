# Phase 11: Settings Sidebar & Layout Unification - Context

**Gathered:** 2026-07-08
**Status:** Ready for planning
**Source:** Spike findings (010, 011)

<domain>
## Phase Boundary

This phase delivers the real-world implementation of the collapsible nested configurations sidebar menu and the optimized settings page layouts inside the Go/templ/HTMX admin UI. It retires the old top tab menu patterns on Workspace, Webhooks, and Telemetry settings pages.

</domain>

<decisions>
## Implementation Decisions

### Sidebar Configurations Accordion
- Implement a collapsible configurations nested submenu under "Configurações" in the left sidebar.
- Submenu options: **Conexões** (`/admin/connections`), **Logs** (`/admin/logs`), **Workspace** (`/admin/workspace`), **Webhooks** (`/admin/webhooks`), and **Telemetry** (`/admin/telemetry`).
- Use Tailwind transition heights (`max-h-0` / `max-h-[240px]` or inline max-height styling) with rotating chevrons (`rotate-180`).
- Ensure the settings submenu stays expanded when loading any settings sub-page.

### Layout Unification
- Remove the old top tab switcher menus on the Workspace, Webhooks, and Telemetry pages.
- Standardize all configuration page headers with clean margins, headings, and description layout using standard DaisyUI panels.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Spikes
- `.agents/skills/spike-findings-pergo/references/settings-ui.md` — Settings UI Blueprint
- `.planning/spikes/010-settings-nested-sidebar/index.html` — Nested sidebar prototype
- `.planning/spikes/011-settings-layout-optimization/index.html` — Layout optimization prototype

</canonical_refs>

<specifics>
## Specific Ideas
- Use vanilla CSS/JS transitions (`transition-all duration-200`) for the submenu slide effect.
- Render server-side active class checks to keep accordion open.

</specifics>

<deferred>
## Deferred Ideas
- None — all settings UI elements are within the current phase scope.

</deferred>

---

*Phase: 11-settings-sidebar-layout-unification*
*Context gathered: 2026-07-08 via Spike Wrap-up*
