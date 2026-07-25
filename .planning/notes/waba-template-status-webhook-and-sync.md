---
title: WABA Template Status Webhook and Sync On-Demand Architecture
date: 2026-07-25
context: Exploration of WABA Template lifecycle and status updates for PerGo
---

# WABA Template Status Webhook and Sync On-Demand Architecture

## Overview
Meta WABA templates undergo automated and manual review before activation. Once submitted, Meta updates their status to `APPROVED`, `REJECTED`, `PAUSED`, `FLAGGED`, or `DISABLED`.

PerGo handles template lifecycle updates through two complementary paths:
1. Real-time Meta system webhook (`message_template_status_update`).
2. On-demand manual sync API endpoint (`POST /admin/devices/:id/templates/sync` / `POST /connections/:slug/templates/sync`).

## Webhook Processing (`message_template_status_update`)

When Meta emits `message_template_status_update`, PerGo:
- Extracts `message_template_name`, `message_template_language`, `event` (`APPROVED`/`REJECTED`/`PAUSED`/`DISABLED`), and `reason`.
- Updates the local `waba_templates` record for the corresponding connection.
- Emits a normalized `template.status_updated` event to client webhooks:
```json
{
  "event": "template.status_updated",
  "channel": "vendas-waba",
  "template_name": "confirmacao_pedido",
  "language": "pt_BR",
  "status": "APPROVED",
  "reason": "NONE"
}
```

## On-Demand Sync Endpoint (`POST /admin/devices/:id/templates/sync`)

Developers or operators can trigger a full template resynchronization from Meta Cloud API v25.0:
- Calls `GET /v25.0/{waba_id}/message_templates`.
- Upserts all returned template definitions, components, languages, and statuses into `waba_templates`.
- Returns a summary response with total synced, added, and updated counts.
