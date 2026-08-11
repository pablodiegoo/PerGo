---
name: spike-findings-pergo
description: Implementation blueprint from spike experiments. Requirements, proven patterns, and verified knowledge for building PerGo. Auto-loaded during implementation work.
---

<context>
## Project: PerGo

Redesign the PerGo channel credentials and devices architecture to support multiple instances of WhatsApp Web (whatsmeow), WABA, and Telegram bots per workspace, routing outbound messages dynamically via a `from` sender identity or connection ID.

Spike sessions wrapped: 2026-06-29, 2026-07-03, 2026-07-16
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
- Campaigns must support CSV mailing list upload, sanitization, WABA template variable mapping (static or dynamic), scheduling, batch throttling (delay and batch size), duration estimation, and enriched outbound logs.
</requirements>
