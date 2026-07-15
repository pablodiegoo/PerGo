# Phase 16 Context: Deprecating Redundant Workspace Subviews

## 1. Locked Decisions

### A. Routing & Redirection
- **GET `/admin/workspaces`** and **GET `/admin/workspaces/`** must redirect to **GET `/admin/workspace`** (the active workspace settings page).
- The workspaces table view is deprecated.

### B. Credentials Cleanup
- The legacy `credentials` database table is deprecated for channel configuration.
- Remove all WhatsApp Cloud and Telegram credentials configuration forms, inputs, and submit actions from the workspace details templates and handlers.
- Revoking credentials at the workspace level is deprecated. All credentials lifecycle/removal should be managed through **Devices / Conexões** (`/admin/devices`).

### C. Workspace View Simplification
- The `/admin/workspace` page (and the `/admin/workspaces/:id` route, since they display active workspaces) will be simplified to show:
  1. Workspace Header and Details (ID, Name, Date metrics)
  2. API Keys management (API Keys list and generation forms)
  3. Delete Workspace button (which triggers the confirmation modal at `/admin/workspaces/:id/confirm-delete`).

### D. WABA Templates Sync
- The `wabaTemplateHandler` (`internal/api/handler/admin/waba_template.go`) must be updated to load credentials from the workspace's active `whatsapp_cloud` connection in the `connections` database table.
- Inject `connectionsRepo` (`*repository.ConnectionRepository`) into the template handler.
- If multiple `whatsapp_cloud` connections exist for the workspace, fallback/default to the first active connection found.

## 2. Code Areas Affected
- **`internal/api/handler/admin/waba_template.go`** (Decouple template sync from legacy `credentials` repository).
- **`internal/api/handler/admin/workspace.go`** (Remove credentials actions and simplify details view).
- **`templates/pages/workspaces.templ`** (Remove credentials card components and workspaces list table).
- **`cmd/pergo/main.go`** (Update route injections and map redirects).
- **`internal/api/handler/admin/waba_template_test.go`** & **`internal/api/handler/admin/workspace_test.go`** (Update test assertions to match refactored handlers).
