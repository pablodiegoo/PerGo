---
name: spike-findings-pergo
description: Implementation blueprint from spike experiments. Requirements, proven patterns, and verified knowledge for building PerGo. Auto-loaded during implementation work.
---

<context>
## Project: PerGo

Redesign the PerGo channel credentials and devices architecture to support multiple instances of WhatsApp Web (whatsmeow), WABA, and Telegram bots per workspace, routing outbound messages dynamically via a `from` sender identity or connection ID.

Spike sessions wrapped: 2026-06-29, 2026-07-03
</context>

<requirements>
## Requirements

- Must support multiple configurations of the same channel type per workspace.
- The public API `POST /api/v1/messages` must allow selecting the sender via a `from` field (matching phone number or bot username) or defaulting to a primary connection.
- Outbound dispatch routing must locate and load credentials/sessions dynamically without requiring application restarts.
- Inbox must show conversations grouped by sender (from + channel) derived from audit_logs GROUP BY — no new table needed for MVP
- Chat view must use split-pane layout (sidebar | conversation list | chat panel) with HTMX partial replacement
- Message bubbles: inbound = left-aligned white, outbound = right-aligned blue (#3b82f6)
- Realtime updates via HTMX polling: chat panel at 3s (append-only with ID cursor), conversation list at 5s (full-replace)
- Unread notifications for background conversations via toast — no browser notification API for MVP
</requirements>

<findings_index>
## Feature Areas

| Area | Reference | Key Finding |
|------|-----------|-------------|
| Multi-Instance Routing & Consolidation | [multi-instance-routing.md](file:///.agents/skills/spike-findings-pergo/references/multi-instance-routing.md) | Consolidate credentials/devices into a single connections table and route dynamically using static adapters to avoid memory leaks. |
| Inbox UI | [inbox-ui.md](file:///.agents/skills/spike-findings-pergo/references/inbox-ui.md) | Split-pane chat view driven by HTMX polling (3s chat / 5s list); conversations derived from audit_logs GROUP BY — no new table for MVP. |
| Settings UI | [settings-ui.md](file:///.agents/skills/spike-findings-pergo/references/settings-ui.md) | Inline collapsible sidebar configurations menu and unified settings sub-pages layout. |
| Conversational Sessions | [conversational-session.md](file:///.agents/skills/spike-findings-pergo/references/conversational-session.md) | Database-driven recipient session tracking for unread badges and persistent bidirectional conversation states. |
| Webhook Delivery & Security | [webhook-delivery.md](file:///.agents/skills/spike-findings-pergo/references/webhook-delivery.md) | Asynchronous NATS JetStream-backed webhook dispatching with HMAC-SHA256 signature verification. |
| Messaging Flow Verbs | [flow-verbs.md](file:///.agents/skills/spike-findings-pergo/references/flow-verbs.md) | Declarative JSON-based flow routing verbs (reply, wait, forward) executing dynamic flows. |
| Compliance Logging | [selective-logging.md](file:///.agents/skills/spike-findings-pergo/references/selective-logging.md) | Selective metadata-only logging and PII content redaction. |
| Omnichannel Contacts | [contact-merging.md](file:///.agents/skills/spike-findings-pergo/references/contact-merging.md) | Merging and resolving multiple channel identities into unified customer profiles. |


## Source Files

Original spike source files are preserved in `sources/` for complete reference:
- [sources/001-multi-instance-schema/](file:///.agents/skills/spike-findings-pergo/sources/001-multi-instance-schema/)
- [sources/002-api-routing-payload/](file:///.agents/skills/spike-findings-pergo/sources/002-api-routing-payload/)
- [sources/003-dynamic-adapter-registry/](file:///.agents/skills/spike-findings-pergo/sources/003-dynamic-adapter-registry/)
- [sources/004-inbox-conversation-list/](file:///.agents/skills/spike-findings-pergo/sources/004-inbox-conversation-list/)
- [sources/005-inbox-chat-view/](file:///.agents/skills/spike-findings-pergo/sources/005-inbox-chat-view/)
- [sources/006-inbox-realtime-polling/](file:///.agents/skills/spike-findings-pergo/sources/006-inbox-realtime-polling/)
- [sources/007-inbox-polling-stability/](file:///.agents/skills/spike-findings-pergo/sources/007-inbox-polling-stability/)
- [sources/008-connection-management-unification/](file:///.agents/skills/spike-findings-pergo/sources/008-connection-management-unification/)
- [sources/009-waba-template-inbox-delivery/](file:///.agents/skills/spike-findings-pergo/sources/009-waba-template-inbox-delivery/)
- [sources/010-settings-nested-sidebar/](file:///.agents/skills/spike-findings-pergo/sources/010-settings-nested-sidebar/)
- [sources/011-settings-layout-optimization/](file:///.agents/skills/spike-findings-pergo/sources/011-settings-layout-optimization/)
- [sources/012-conversational-session-schema/](file:///.agents/skills/spike-findings-pergo/sources/012-conversational-session-schema/)
- [sources/013-queue-decoupled-webhook-dispatcher/](file:///.agents/skills/spike-findings-pergo/sources/013-queue-decoupled-webhook-dispatcher/)
- [sources/014-hmac-webhook-verification/](file:///.agents/skills/spike-findings-pergo/sources/014-hmac-webhook-verification/)
- [sources/015-messaging-verbs-engine/](file:///.agents/skills/spike-findings-pergo/sources/015-messaging-verbs-engine/)
- [sources/016-selective-metadata-logging/](file:///.agents/skills/spike-findings-pergo/sources/016-selective-metadata-logging/)
- [sources/017-omnichannel-contact-merging/](file:///.agents/skills/spike-findings-pergo/sources/017-omnichannel-contact-merging/)
</findings_index>

<metadata>
## Processed Spikes

- 001-multi-instance-schema
- 002-api-routing-payload
- 003-dynamic-adapter-registry
- 004-inbox-conversation-list
- 005-inbox-chat-view
- 006-inbox-realtime-polling
- 007-inbox-polling-stability
- 008-connection-management-unification
- 009-waba-template-inbox-delivery
- 010-settings-nested-sidebar
- 011-settings-layout-optimization
- 012-conversational-session-schema
- 013-queue-decoupled-webhook-dispatcher
- 014-hmac-webhook-verification
- 015-messaging-verbs-engine
- 016-selective-metadata-logging
- 017-omnichannel-contact-merging
</metadata>
