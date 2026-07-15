# Phase 16 Research: Deprecating Redundant Workspace Subviews

## 1. Findings & Blueprint (derived from Spike 023)
The goal is to deprecate redundant workspace credentials and simplify the workspace details settings.

### A. Template sync decoupling (WABA Template Handler)
- **File**: `internal/api/handler/admin/waba_template.go`
- **Current implementation**: Calls `h.CredsRepo.Get(ctx, workspaceID, "whatsapp_cloud")` to fetch the API token and WABA Account ID.
- **New implementation**:
  - In `WABATemplateHandler` struct, replace `CredsRepo *repository.CredentialsRepository` with `ConnectionsRepo *repository.ConnectionRepository`.
  - In `List` and `Create` methods, fetch WABA credentials by querying `h.ConnectionsRepo.ListByWorkspace(ctx, workspaceID)`.
  - Iterate through connections and find the first active connection where `conn.Channel == "whatsapp_cloud"`.
  - Extract credentials from `conn.Credentials`.

### B. Workspace details simplification (Workspace Handler)
- **File**: `internal/api/handler/admin/workspace.go`
- **Changes**:
  - Modify `Detail` method to remove credentials retrieval (`h.Credentials.Get(...)` for WABA and Telegram).
  - Modify `WorkspaceDetailPage` rendering inside pages to only receive workspace, keys list, and external URL.
  - Delete `SaveCredentials` and `DeleteCredentials` handlers as they are no longer needed.
- **File**: `templates/pages/workspaces.templ`
  - Simplify `WorkspaceDetailContent` to remove `Channel Credentials` card section entirely.
  - Remove helper cards `WABACredentialsCard` and `TelegramCredentialsCard`.
  - Simplify `WorkspaceListPage` and `WorkspaceListContent` if workspaces listing is accessed, but focus primarily on simplification of details.
- **File**: `cmd/pergo/main.go`
  - Remove routes POST `/admin/workspaces/:id/credentials/:channel` and DELETE `/admin/workspaces/:id/credentials/:channel`.
  - Map `GET /admin/workspaces` to redirect to `/admin/workspace`.

### C. Test suites updates
- **File**: `internal/api/handler/admin/workspace_test.go`
- **File**: `internal/api/handler/admin/waba_template_test.go`
- Update test cases to match the simplified handlers and mock calls.
