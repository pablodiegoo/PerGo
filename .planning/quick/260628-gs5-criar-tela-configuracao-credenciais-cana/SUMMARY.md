---
status: complete
date: 2026-06-28
description: Channel credentials configuration UI for WABA and Telegram
---

# Quick Task GS5: criar-tela-configuracao-credenciais-canais - Summary

## Work Done
1. **Workspace Detail Update**: Add structured channel config sections for WhatsApp Cloud (WABA) and Telegram Bot in `workspaces.templ`.
2. **HTMX Integration**: The forms submit configs using `hx-post` and allow revoking with `hx-delete`, replacing the target card inline without full page reloads.
3. **Graceful Masking**: Implemented `maskToken` in the templ file to display masked tokens (`EAAB...****`) to operators.
4. **Backend Implementation**: Integrated `SaveCredentials` and `DeleteCredentials` endpoints inside `WorkspaceHandler` and wired them in `main.go`.
5. **Fixed Test Suite**:
   - Fixed signature compatibility for `LoginPost` and `StartDebugServer` across all tests.
   - Fixed foreign key violations by creating test workspaces.
   - Fixed upsert target for the unique partial index `idx_devices_jid`.
