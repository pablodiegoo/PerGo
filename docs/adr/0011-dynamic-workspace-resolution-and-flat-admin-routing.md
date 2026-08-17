# 0011. Dynamic Workspace Resolution and Flat Admin Routing

Status: accepted

## Context

Previously, PerGo utilized hardcoded deterministic UUIDs (e.g. `a0000000-0000-0000-0000-000000000001`) and fallbacks to `uuid.Nil` (`00000000-0000-0000-0000-000000000000`) across handlers and bootstrap scripts. Additionally, the admin console mixed URL-based tenant routing (`/admin/workspaces/:workspace_id/...`) with cookie-based flat routing (`/admin/inbox`, `/admin/connections`), causing multi-tenant workspace switching in the operator UI to fail when visiting or switching between secondary workspaces.

## Decision

1. **Eliminate Magic UUIDs**: Workspaces are identified purely by dynamic UUIDs generated at insertion (`uuid.New()` / DB default `gen_random_uuid()`). The startup bootstrap creates a default workspace dynamically only if the database contains zero workspaces.
2. **Unified Admin Workspace Resolution**: Admin web console routes are standardized to flat URLs (`/admin/campaigns`, `/admin/tags`, `/admin/templates`, `/admin/integrations/chatwoot`, etc.). The active workspace is resolved centrally via the `pergo-active-workspace` cookie in the admin middleware. If missing or invalid, it automatically falls back to the first available workspace in the database; if the database is empty, it redirects to `/admin/workspaces/new`. Legacy routes containing `:workspace_id` redirect to the flat equivalent.
3. **Strict Repository Validation**: All repository methods accepting `workspace_id` fail fast and reject `uuid.Nil`, preventing orphaned or zero-UUID records from querying or polluting the PostgreSQL database.
4. **Smart Switcher UX**: Switching the active workspace from the sidebar reloads list pages in place, while detail pages redirect to their section root to prevent cross-tenant 404 errors.
