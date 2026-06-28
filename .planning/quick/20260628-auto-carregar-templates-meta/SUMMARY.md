---
status: complete
date: 2026-06-28
description: Auto-load WABA templates from Meta when WABA credentials are saved
---

# Quick Task: auto-carregar-templates-meta - Summary

## Work Done
1. **Upsert Operation**: Added an `Upsert` method to `WABATemplateRepository` in `waba_template.go` to safely insert or update templates locally on matching `(workspace_id, name, language)`.
2. **Meta API Synchronization Integration**:
   - In `WorkspaceHandler` (`workspace.go`), added the `syncTemplatesFromMeta` helper.
   - The helper requests Meta Graph API `GET /v18.0/{waba_account_id}/message_templates` using the newly saved WABA API token.
   - For each template in the payload, it parses and saves/upserts it to the local `waba_templates` table in PostgreSQL.
3. **Background Dispatch**: Triggered the synchronization in a background goroutine from `SaveCredentials` to avoid delaying HTTP response times for UI components.
4. **Successful Compilation and Run**: Generated templates, compiled Go code, and passed 100% of whole repository integration tests without regressions.
