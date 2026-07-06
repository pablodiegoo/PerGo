# Spike Manifest

## Idea
Redesign the PerGo channel credentials and devices architecture to support multiple instances of WhatsApp Web (whatsmeow), WABA, and Telegram bots per workspace, routing outbound messages dynamically via a `from` sender identity or connection ID.

## Requirements
- Must support multiple configurations of the same channel type per workspace.
- The public API `POST /api/v1/messages` must allow selecting the sender via a `from` field (matching phone number or bot username) or defaulting to a primary connection.
- Outbound dispatch routing must locate and load credentials/sessions dynamically without requiring application restarts.
- Inbox must show conversations grouped by sender (from + channel) derived from audit_logs GROUP BY — no new table needed for MVP
- Chat view must use split-pane layout (sidebar | conversation list | chat panel) with HTMX partial replacement
- Message bubbles: inbound = left-aligned white, outbound = right-aligned blue
- Realtime updates via HTMX polling: chat panel at 3s (append-only with ID cursor), conversation list at 5s (full-replace)
- Unread notifications for background conversations via toast — no browser notification API for MVP

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
