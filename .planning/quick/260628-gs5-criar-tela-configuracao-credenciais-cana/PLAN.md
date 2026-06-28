# Quick Task GS5: criar-tela-configuracao-credenciais-canais - Plan

**Status:** Ready to execute
**Slug:** criar-tela-configuracao-credenciais-cana

## Goals
Implement a user interface in the Workspace Detail page to allow operators to configure, update, and revoke channel credentials for WABA (WhatsApp Cloud API) and Telegram Bot API.

## Tasks

### Task 1: Update workspace handler dependencies
- Add `Credentials` of type `*repository.CredentialsRepository` to `admin.WorkspaceHandler` in `internal/api/handler/admin/workspace.go`.
- Wire `credentialsRepo` when constructing `workspaceHandler` in `cmd/pergo/main.go`.

### Task 2: Implement save/delete credentials handlers
- Implement `WorkspaceHandler.SaveCredentials` to handle `POST /admin/workspaces/:id/credentials/:channel`.
- Implement `WorkspaceHandler.DeleteCredentials` to handle `DELETE /admin/workspaces/:id/credentials/:channel`.
- Register the routes in `cmd/pergo/main.go`.

### Task 3: Load credentials in WorkspaceHandler.Detail
- In `WorkspaceHandler.Detail`, load the current config for `whatsapp_cloud` and `telegram` using `Credentials.Get`.
- Pass these details to the updated templates.

### Task 4: Design templates for Channel Credentials Configuration
- Create forms and states for `whatsapp_cloud` and `telegram` configurations inside `templates/pages/workspaces.templ`.
- Use HTMX to submit forms, load messages/alerts, and refresh inline states.

### Task 5: Verification
- Verify the build, compile templates, run manual test checks.
