# Spike Wrap-Up Summary

**Date:** 2026-07-14
**Spikes processed:** 14 (004, 005, 006, 007, 008, 009, 010, 011, 012, 013, 014, 015, 016, 017)
**Feature areas:** Conversational Inbox, Unified Connection Management, Settings UI, Conversational Sessions, Webhook Delivery & Security, Messaging Flow Verbs, Compliance Logging, Omnichannel Contacts
**Skill output:** `./.agents/skills/spike-findings-pergo/`

## Processed Spikes

| # | Name | Type | Verdict | Feature Area |
|---|------|------|---------|--------------| 
| 004 | inbox-conversation-list | standard | VALIDATED | Inbox UI |
| 005 | inbox-chat-view | standard | VALIDATED | Inbox UI |
| 006 | inbox-realtime-polling | standard | VALIDATED | Inbox UI |
| 007 | inbox-polling-stability | standard | VALIDATED | Polling Stability |
| 008 | connection-management-unification | standard | VALIDATED | Unified Connections |
| 009 | waba-template-inbox-delivery | standard | VALIDATED | WABA Templates |
| 010 | settings-nested-sidebar | standard | VALIDATED | Settings UI |
| 011 | settings-layout-optimization | standard | VALIDATED | Settings UI |
| 012 | conversational-session-schema | standard | VALIDATED | Conversational Sessions |
| 013 | queue-decoupled-webhook-dispatcher | standard | VALIDATED | Webhook Delivery |
| 014 | hmac-webhook-verification | standard | VALIDATED | Webhook Security |
| 015 | messaging-verbs-engine | standard | VALIDATED | Messaging Flow Verbs |
| 016 | selective-metadata-logging | standard | VALIDATED | Compliance Logging |
| 017 | omnichannel-contact-merging | standard | VALIDATED | Omnichannel Contacts |

## Key Findings

**Data model:** No new `conversations` table needed for MVP. Conversations derived from `audit_logs` GROUP BY `(from, channel)`. A JSONB index on `payload->>'from'` and `payload->>'channel'` is required for acceptable query performance.

**UI pattern:** Three-column split-pane (sidebar 220px | conv list 300px | chat flex-1). HTMX partial replacement for all panel transitions — consistent with existing PerGo admin pattern.

**Realtime & Polling Stability (Spike 007):**
To completely avoid client-side event listeners and infinite reload loops:
1. Render initial `ChatPanel` with `after_id` set to the last message ID directly.
2. The server-side polling endpoint `/admin/inbox/messages` returns new messages AND an out-of-band swap element `<div id="chat-poll-anchor" hx-swap-oob="true" ...>` with the updated `after_id` cursor.
3. Use `hx-swap="beforeend scroll:bottom"` on the poll anchor. HTMX automatically scrolls to the bottom with zero custom JS.

**Connection Management Unification (Spike 008):**
1. Replaced the separate devices screen and workspace configuration credentials with a single, consolidated **Connections** view.
2. Clicking "Nova Conexão" opens a modal to select the channel type:
   - *WhatsApp Web*: Input phone number -> click "Pair" -> starts Whatsmeow pairing and shows QR code inside the modal.
   - *WABA / Telegram*: Input credentials -> validates token/credentials and saves instantly.
3. Integrated the developer playground: each connection has a "Testar" button opening a mini-modal to send a test message and show a live debug log of NATS event dispatch. The standalone playground screen is decommissioned.

**WABA Templates & 24h Window (Spike 009):**
1. Outside the 24-hour window, the standard compose text area is disabled and a warning banner is shown: *"Janela de 24h fechada. Envie um template para reabrir a conversa."*
2. A "Templates" button is available for WABA conversations. Clicking it opens a modal to select templates and input variable parameters (`{{1}}`, `{{2}}`).
3. Initiating new conversations ("Novo Chat" button) requires using templates for WABA.

**Settings UI (Spikes 010 & 011):**
1. Accordion Configurations menu toggles sub-navigation options inline with smooth height expansion.
2. Settings layout is standardized, removing top tabs and relying purely on the nested sidebar.

**Conversational Sessions (Spike 012):**
1. Recipient sessions are persisted to a `recipient_sessions` table mapping unique composite keys `(workspace_id, recipient_phone, channel, recipient_identity)`.
2. Automatic repository upsert using PostgreSQL's `ON CONFLICT` prevents write failures and tracks unread state cleanly across node crashes.

**Webhook Delivery & Security (Spikes 013 & 014):**
1. Decoupled webhook processing uses a NATS JetStream stream `webhooks.events` to prevent third-party CRM downtime from affecting socket loops or gateways.
2. Webhook payload security is enforced via an HMAC-SHA256 signature calculated from the raw JSON payload and the workspace-specific secret key, sent via the `X-PerGo-Signature` header.

**Messaging Flow Verbs Engine (Spike 015):**
1. Designed a declarative dynamic flow engine parsing JSON verbs like `reply`, `wait`, and `forward`.
2. Goroutine execution respects precise time delays and is cancelable via standard Go `context` structures.

**Selective Metadata Logging (Spike 016):**
1. Compliance configuration (`SaveMessageBodies = false`) filters message content (PII) before storage.
2. Cryptographic metadata (body length, type, identifiers) is retained in database JSONB payloads for tracing and billing.

**Omnichannel Contact Merging (Spike 017):**
1. Customer identity registry maps multiple channel endpoints (WhatsApp, Telegram) to a single `Contact` profile.
2. Atomic merge queries update associated identities and conversation threads, preventing dangling relationships.
