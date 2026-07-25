---
title: Implement WABA Template Status Webhook Handler and On-Demand Sync API
date: 2026-07-25
priority: high
tags: [waba, templates, webhook, sync]
---

# Implement WABA Template Status Webhook Handler and On-Demand Sync API

## Context
WABA templates require status tracking when approved or rejected by Meta. PerGo must process `message_template_status_update` webhooks and expose an on-demand synchronization endpoint for operators and system integrations.

## Implementation Tasks

1. **Webhook Handler (`message_template_status_update`)**:
   - In `internal/channel/waba` webhook router, handle field `message_template_status_update`.
   - Update `waba_templates` record status (`APPROVED`, `REJECTED`, `PAUSED`, `DISABLED`) and `rejection_reason`.
   - Dispatch `template.status_updated` event to workspace webhook endpoints.

2. **On-Demand Template Sync Handler**:
   - Implement `SyncTemplates` in WABA client package calling Meta Graph API `GET /v25.0/{waba_id}/message_templates`.
   - Expose `POST /admin/devices/:id/templates/sync` endpoint in admin API handler.
   - Return JSON response `{ "synced": 15, "updated": 2, "status": "success" }`.

3. **Unit & Integration Tests**:
   - Test webhook event parsing for `APPROVED` and `REJECTED` events.
   - Test `SyncTemplates` with mock Meta Graph API server.
