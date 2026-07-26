# Research Question: WABA SMB Message Echoes Webhook Format

**Date**: 2026-07-25
**Context**: gsd-explore session — WABA features gap analysis
**Priority**: MEDIUM — informs REQ-WABA-SMB-ECHOES implementation

## Question

What is the exact webhook payload format for Meta's `smb_message_echoes` field, and how do we enable it on the WABA webhook subscription?

## What We Need

1. **Subscription setup**: How to add `smb_message_echoes` to the webhook subscription fields? Is it `POST /{app_id}/subscriptions` with `fields: ["messages", "smb_message_echoes"]` or a separate call?
2. **Webhook payload format**: What does the `smb_message_echoes` webhook body look like? Does it mirror the standard `messages` format but with outgoing direction?
3. **Message types covered**: Does it echo all message types (text, media, template, interactive) or only text?
4. **Metadata available**: Does the echo include the business user who sent it (for multi-agent scenarios)?
5. **Deduplication**: If PerGo also sends a message via API, and the operator views it in the Business app, does it echo back? How to distinguish API-originated messages from app-originated?
6. **Availability**: Is `smb_message_echoes` available to all WABA accounts or only certain tiers/plans?
7. **Timing**: Is the echo delivered in real-time or with a delay?

## Known Information (from Chatwoot)

- Chatwoot subscribes to `['messages', 'smb_message_echoes']` (plus `'calls'` if voice enabled)
- Chatwoot detects echoes via `field: "smb_message_echoes"` in the webhook entry
- Echo messages are ingested as `message_type: :outgoing, external_echo: true, status: :delivered`
- Chatwoot uses this to keep conversation timeline synchronized when agents reply from the mobile app

## Sources to Investigate

- [Meta Webhooks — WhatsApp Business Account](https://developers.facebook.com/docs/graph-api/webhooks/reference/whatsapp-business-account)
- Meta developer docs for `smb_message_echoes` field
- Chatwoot source: `Webhooks::WhatsappEventsJob` echo handling
