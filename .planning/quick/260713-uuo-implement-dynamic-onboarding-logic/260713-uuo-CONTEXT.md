# Quick Task 260713-uuo: implement-dynamic-onboarding-logic - Context

**Gathered:** 2026-07-14
**Status:** Ready for planning

<domain>
## Task Boundary

Modify the admin handler logic to dynamically determine if the workspace has completed the onboarding requirements (having at least 1 API key and 1 active connection) and display the correct panel layout.

</domain>

<decisions>
## Implementation Decisions

### Onboarding Criteria & Connection Status
- We will count only non-revoked API keys and active/connected connections (where status is 'active' or 'connected').

### Dashboard Layout Behavior
- Complete replacement: Render ONLY the onboarding checklist until onboarding is complete, then swap to the operational metrics/graphs.

### Count Queries Implementation
- We will implement direct SQL `COUNT(*)` queries:
  - `CountActive(ctx, workspaceID)` in `APIKeyRepository` (filtering out revoked/expired keys if any status logic applies).
  - `CountActiveByWorkspace(ctx, workspaceID)` in `ConnectionRepository` (filtering for status = 'active' or 'connected').

</decisions>

<specifics>
## Specific Ideas
- The `OverviewHandler` in `internal/api/handler/admin/dashboard.go` will fetch both counts, compute `ShowOnboarding = (apiKeyCount == 0 || connectionCount == 0)`, and pass this flag to the Templ template.
- The dashboard template `templates/pages/dashboard.templ` (or whichever template renders the main admin page) will conditionally render either the checklist component or the metrics/graphs.

</specifics>

<canonical_refs>
## Canonical References
- [implement-dynamic-onboarding-logic.md](file:///home/pablo/Coding/PerGo/.planning/todos/pending/implement-dynamic-onboarding-logic.md)

</canonical_refs>
