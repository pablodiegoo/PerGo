---
title: Implement Dynamic Onboarding Logic for Admin Dashboard
date: 2026-06-29
priority: low
---

# Todo: Implement Dynamic Onboarding Logic for Admin Dashboard

## Description
Modify the admin handler logic to dynamically determine if the workspace has completed the onboarding requirements (having at least 1 API key and 1 active connection) and display the correct panel layout.

## Action Items
- [ ] Implement `Count(ctx, workspaceID)` in `APIKeyRepository` to return the number of non-revoked API keys.
- [ ] Implement `CountByWorkspace(ctx, workspaceID)` in `DeviceRepository` (or `connections`) to return the number of configured connections.
- [ ] Update the `OverviewHandler` in `internal/api/handler/admin/dashboard.go`:
  - Fetch API key count and connection count.
  - Set a boolean flag `ShowOnboarding = (apiKeyCount == 0 || connectionCount == 0)`.
  - Pass this flag to the Templ template rendering the dashboard.
- [ ] Update the dashboard Templ template to conditionally render either the checklist component or the operational metrics graphs based on `ShowOnboarding`.
