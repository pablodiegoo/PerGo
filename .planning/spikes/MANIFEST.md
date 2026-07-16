# Spike Manifest

## Idea
Redesign the PerGo channel credentials and devices architecture to support multiple instances of WhatsApp Web (whatsmeow), WABA, and Telegram bots per workspace, routing outbound messages dynamically via a `from` sender identity or connection ID. Also includes bulk campaign scheduling, CSV parsing, variable mapping, batch throttling, and analytics logging.

## Requirements
- Must support multiple configurations of the same channel type per workspace.
- The public API `POST /api/v1/messages` must allow selecting the sender via a `from` field (matching phone number or bot username) or defaulting to a primary connection.
- Outbound dispatch routing must locate and load credentials/sessions dynamically without requiring application restarts.
- Inbox must show conversations grouped by sender (from + channel) derived from audit_logs GROUP BY — no new table needed for MVP
- Chat view must use split-pane layout (sidebar | conversation list | chat panel) with HTMX partial replacement
- Message bubbles: inbound = left-aligned white, outbound = right-aligned blue
- Realtime updates via HTMX polling: chat panel at 3s (append-only with ID cursor), conversation list at 5s (full-replace)
- Unread notifications for background conversations via toast — no browser notification API for MVP
- Campaigns must support CSV mailing list upload, sanitization, WABA template variable mapping (static or dynamic), scheduling, batch throttling (delay and batch size), duration estimation, and exportable logs.

## Spikes

| # | Name | Type | Validates | Verdict | Tags |
|---|------|------|-----------|---------|------|
| 001 | multi-instance-schema | standard | Given a workspace with multiple configurations, when migrated to a unified connections schema, then we can store and encrypt distinct credentials/sessions cleanly. | VALIDATED | db, schema |
| 002 | api-routing-payload | standard | Given a message request, when multiple instances exist, then we can route it dynamically via the `from` field with fallback support. | VALIDATED | api, routing |
| 003 | dynamic-adapter-registry | standard | Given a running server, when connection credentials change, then the registry can dynamically instantiate/update dispatchers in memory. | VALIDATED | concurrency, registry |
| 004 | inbox-conversation-list | standard | Given inbound messages in audit_logs, when operator opens Inbox, then conversations grouped by sender with channel filter tabs | VALIDATED | ui, inbox, admin |
| 005 | inbox-chat-view | standard | Given a selected conversation, when operator clicks it, then split-pane chat loads with alternating bubbles via HTMX | VALIDATED | ui, inbox, chat, htmx |
| 006 | inbox-realtime-polling | standard | Given chat is open, when new inbound arrives, then panel updates within 3s without user action | VALIDATED | ui, inbox, realtime, polling |
| 007 | inbox-polling-stability | standard | Given an open active chat panel, when polling for new messages, then we can avoid infinite reloading loops and scroll jitter by updating the polling anchor after_id out-of-band | VALIDATED | ui, inbox, polling, htmx |
| 008 | connection-management-unification | standard | Given a workspace with multiple connections, when replacing separate connection pages and playground forms, then we can configure and test connections in a single dashboard | VALIDATED | ui, connections, workspace |
| 009 | waba-template-inbox-delivery | standard | Given WABA integration, when the 24-hour customer window is closed, then the Inbox UI disables free text input and enforces sending approved templates to reopen the window | VALIDATED | ui, inbox, waba, templates |
| 010 | settings-nested-sidebar | standard | Given a left navigation sidebar, when the "Configurações" option is clicked, then it toggles a sub-navigation section inline with sub-options (Logs, Conexões, Workspace, Webhooks, Telemetry), and when a settings page is active, the sub-navigation remains open/active. | VALIDATED | ui, sidebar, settings |
| 011 | settings-layout-optimization | standard | Given sub-pages (Logs, Connections, Workspaces, Webhooks, Telemetry) under configurations, when they are accessed, then they use a clean, unified settings layout structure that optimizes visual consistency. | VALIDATED | ui, settings, layout |
| 012 | conversational-session-schema | standard | Given inbound messages from multiple channels, when mapped using contacts and conversations tables, then we can track logical two-way conversations and update sessions across channel restarts. | VALIDATED | db, schema, conversation |
| 013 | queue-decoupled-webhook-dispatcher | standard | Given inbound messages on whatsmeow socket listeners, when enqueued to NATS JetStream and processed asynchronously, then slow/failed CRM webhook targets do not block or lag the whatsmeow socket loop. | VALIDATED | queue, nats, webhook |
| 014 | hmac-webhook-verification | standard | Given a JSON webhook event payload, when signed with an HMAC-SHA256 signature header using a workspace-specific secret key, then the receiving server can verify authenticity. | VALIDATED | security, hmac, webhook |
| 015 | messaging-verbs-engine | standard | Given an inbound message, when PerGo triggers a webhook and receives a list of JSON-serialized messaging verbs, then PerGo executes the sequence dynamically. | VALIDATED | api, verbs, routing |
| 016 | selective-metadata-logging | standard | Given a workspace with message body logging disabled, when a message is processed, then audit logs persist only cryptographic metadata without storing the message body or media URL in plaintext. | VALIDATED | db, compliance, privacy |
| 017 | omnichannel-contact-merging | standard | Given a contact with active conversations on both WhatsApp and Telegram, when queried via a unified contacts API, then their identities are linked to a single customer profile with consolidated history. | VALIDATED | db, schema, omnichannel |
| 018 | multi-webhook-subscriptions | standard | Given a workspace with multiple webhook subscriptions, when an event occurs, then only the webhooks subscribed to that specific event type are triggered. | VALIDATED | api, webhooks, routing |
| 019 | session-caching-router | standard | Given multiple active WhatsMeow connections, when a message is sent, then the gateway resolves and routes the request using an in-memory session cache instead of querying the database on every dispatch. | VALIDATED | api, cache, concurrency |
| 020 | campaign-engine | standard | Given a mailing list and throttling parameters, when configured in a UI, then we can clean the list, map variables, estimate duration, and simulate batch dispatching with logging comparison. | VALIDATED | ui, campaigns, logs |
| 021 | user-action-logs | standard | Given requests to PerGo from API keys or dashboard users, when recorded in a unified action log table, then we can track actors, actions, metadata, sources, and access times in the UI. | VALIDATED | db, schema, logs, audit |
| 022 | css-standardization | standard | Given varying page layouts and CSS styles, when analyzed and refactored into a unified style guide with standard CSS tokens, then we can guarantee visual consistency and a premium user experience across all PerGo dashboard pages. | VALIDATED | ui, css, style-guide, design |
| 023 | deprecated-workspace-subviews | standard | Given redundant workspace credentials and sub-telas, when WABA template sync is migrated to connection credentials and duplicate forms are removed, then we can manage active workspace settings cleanly. | VALIDATED | ui, workspace, credentials, refactoring |
| 024 | prd-implementation-gap-audit | standard | Given the context/ PRD documents, when compared exhaustively against the implemented codebase, then we identify every unimplemented feature and architecture gap. | VALIDATED | audit, gaps, prd, architecture |
