---
status: complete
date: 2026-06-28
description: Auto-load WABA templates from Meta when WABA credentials are saved
---

# Plan - Auto-load templates from Meta on credentials save

Implement automatic fetching and saving of pre-existing Meta WABA message templates when WABA credentials are saved.

## Tasks
1. **Repository Upsert**: Implement `Upsert` method in `WABATemplateRepository` targeting unique constraint `(workspace_id, name, language)`.
2. **Sync Logic**: Implement `syncTemplatesFromMeta` helper in `WorkspaceHandler` calling Meta's GET `/message_templates` API.
3. **Background Hook**: Call the sync helper in a background goroutine during `SaveCredentials` form submission.
4. **Dependency Injection**: Inject `WABATemplateRepository` into `WorkspaceHandler` inside `main.go`.
5. **Verification**: Run `go build` and `make test-race`.
