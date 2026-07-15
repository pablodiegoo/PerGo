# Spike 023: Deprecating Redundant Workspace Subviews

## 1. Context & Motivation
Currently, PerGo manages WABA and Telegram credentials in two distinct layers:
1. The legacy **Workspace Credentials** (`credentials` table, managed via `/admin/workspaces/:id` subview forms).
2. The modern **Multi-Instance Connections** (`connections` table, managed via `/admin/devices` Nova Conexão modal).

Because the platform now supports active workspace switching directly from the navigation sidebar, having a complex hierarchy of `/admin/workspaces` (list of all workspaces) and `/admin/workspaces/:id` (detailed subviews with duplicate credentials forms) is redundant and confusing. 

We can completely eliminate the duplicate credentials forms from the workspace page and migrate WABA template synchronization to retrieve credentials directly from the workspace's active `whatsapp_cloud` connection.

## 2. Analysis of Information & Relocation Strategy

### A. Channel Credentials (WABA & Telegram)
- **Current display**: Configured at `/admin/workspaces/:id` inside two cards (`WABACredentialsCard` and `TelegramCredentialsCard`).
- **Relocation**: Fully deprecated at the workspace level. These credentials are now configured solely under **Conexões** (`/admin/devices`) via the unified `whatsapp_cloud` and `telegram` connection options.
- **WABA Templates Sync**: `wabaTemplateHandler` currently fetches credentials from the old `credentials` table. We will refactor this handler to search for the active connection of type `whatsapp_cloud` for the active workspace in the `connections` table.

### B. API Keys
- **Current display**: Listed and generated inside `/admin/workspaces/:id`.
- **Relocation**: Retained as the primary configuration element of the active workspace settings page (`/admin/workspace`).

### C. Workspace Management
- **Current display**: Workspace list page (`/admin/workspaces`) allows seeing all workspaces and deleting them.
- **Relocation**: 
  - The workspace dropdown selector in the sidebar remains the primary way to switch between workspaces.
  - Clicking **Workspace** in the sidebar settings goes directly to `/admin/workspace` which displays details for the **currently active workspace**.
  - We will simplify `/admin/workspace` to display:
    1. Active Workspace Name / ID
    2. API Keys management (list and generate form)
    3. Delete Workspace button (which triggers the confirmation modal)
  - This removes the list page `/admin/workspaces` from general use, redirecting it to the active workspace settings.

## 3. Impact Assessment & Code Changes
1. **`internal/api/handler/admin/waba_template.go`**:
   - Replace `CredsRepo` with `ConnectionsRepo *repository.ConnectionRepository`.
   - Update sync and creation methods to fetch credentials from the active `whatsapp_cloud` connection.
2. **`cmd/pergo/main.go`**:
   - Inject `connectionsRepo` into `NewWABATemplateHandler`.
   - Map `/admin/workspace` to a consolidated route that serves the active workspace details, or redirect `/admin/workspaces` to `/admin/workspace`.
3. **`templates/pages/workspaces.templ`**:
   - Simplify `WorkspaceDetailContent` to remove `WABACredentialsCard` and `TelegramCredentialsCard` components.
4. **`internal/api/handler/admin/workspace.go`**:
   - Remove unused credentials endpoints (`SaveCredentials`, `DeleteCredentials`).
