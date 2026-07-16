# Workspace Simplification

## Requirements

- Workspace credentials for WABA and Telegram must be managed exclusively through the unified Connections (`/admin/devices`) system — no duplicate forms at the workspace level.
- WABA template synchronization must retrieve credentials from the active `whatsapp_cloud` connection, not the legacy `credentials` table.
- The workspace detail page (`/admin/workspace`) shows only: active workspace name/ID, API key management, and delete workspace action.
- Root endpoint `/admin/workspaces` redirects to `/admin/workspace` (active workspace).

## How to Build It

### 1. Migrate WABA Template Handler

Replace `CredsRepo` dependency with `ConnectionsRepo` in `internal/api/handler/admin/waba_template.go`:

```go
// Before: handler fetches from legacy credentials table
creds, err := h.CredsRepo.GetByWorkspace(ctx, workspaceID)

// After: handler fetches from active whatsapp_cloud connection
conn, err := h.ConnectionsRepo.GetDefaultChannelConnection(ctx, workspaceID, "whatsapp_cloud")
```

### 2. Simplify Workspace Templates

Remove from `templates/pages/workspaces.templ`:
- `WABACredentialsCard` component
- `TelegramCredentialsCard` component

Keep only:
- Workspace info header (name, ID)
- API keys list + generation form
- Delete workspace button with confirmation modal

### 3. Update Routes

In `cmd/pergo/main.go`:
- Inject `connectionsRepo` into `NewWABATemplateHandler`
- Redirect `/admin/workspaces` → `/admin/workspace`
- Redirect `/admin/workspaces/:id` → `/admin/workspace`

### 4. Clean Up Handler

Remove from `internal/api/handler/admin/workspace.go`:
- `SaveCredentials` endpoint
- `DeleteCredentials` endpoint

## What to Avoid

- Don't delete the `credentials` table or repository immediately — it may be referenced by migration history. Deprecate in code, remove in a future cleanup.
- Don't break the workspace selector dropdown in the sidebar — it must continue working as the primary workspace switching mechanism.

## Constraints

- The `connections` table must have a `whatsapp_cloud` connection with `is_default=true` for the WABA template sync to work. Add validation/error handling for workspaces without a default WABA connection.
- Connection-scoped WABA templates (migration 022) means templates are tied to `connection_id`, not `workspace_id` — the sync handler must pass the connection ID through.

## Origin

Synthesized from spike: 023
Source files available in: sources/023-deprecated-workspace-subviews/
