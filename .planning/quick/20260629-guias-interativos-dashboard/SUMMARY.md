---
status: complete
date: 2026-06-29
description: Added interactive onboarding and helper guides with copyable webhook URLs inside credentials cards in the admin dashboard.
---

# Quick Task: guias-interativos-dashboard - Summary

## Work Done
1. **Interactive Guides**:
   - Added collapsible `<details>` components inside `WABACredentialsCard` and `TelegramCredentialsCard` in [workspaces.templ](file:///home/pablo/Coding/OmniGo/templates/pages/workspaces.templ).
   - Each guide features copyable webhook URL textboxes that dynamically resolve using the system's `ExternalURL` config and the workspace's ID.
   - Built dynamic click-to-copy behavior using `navigator.clipboard`.
2. **Backend Integration**:
   - Updated the `WorkspaceHandler` inside [workspace.go](file:///home/pablo/Coding/OmniGo/internal/api/handler/admin/workspace.go) to pass the configured `ExternalURL` parameter downstream into the page and card components.
3. **Documentation Sync**:
   - Updated [docs/CHANNELS.md](file:///home/pablo/Coding/OmniGo/docs/CHANNELS.md) to fix the WABA Callback URL placeholder format, matching the actual endpoint format `/webhooks/waba/[workspace_id]`.
4. **Verification**:
   - Regenerated templates (`make generate`).
   - Ran `make test` confirming all tests compile and pass successfully.
