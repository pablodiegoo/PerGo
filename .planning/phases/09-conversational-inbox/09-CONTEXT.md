# Phase 9: Conversational Inbox - Context

**Gathered:** 2026-07-03
**Status:** Ready for planning

<domain>
## Phase Boundary

This phase delivers a modern server-rendered split-pane conversational inbox in the operator console. It groups existing inbound/outbound messages dynamically by contact and channel, displays them in a traditional alternating chat bubble view, supports real-time updates via lightweight HTMX polling, and enables operators to send replies directly from the UI.

</domain>

<decisions>
## Implementation Decisions

### Data Model & Queries
- **D-01 (No New Tables):** Group `audit_logs` by `(payload->>'from', payload->>'channel')` to dynamically derive active conversations. Do not create a separate `conversations` table for the MVP.
- **D-02 (Performance Index):** Create a database migration adding a JSONB index on the audit logs table for `((payload->>'from'), (payload->>'channel'), workspace_id, created_at DESC)` filtered by `event_type = 'inbound_message'` to ensure sub-millisecond grouping query latency.
- **D-03 (Thread Stitching):** The thread query must UNION inbound and outbound audit log messages, matching on `payload->>'from'` / `payload->>'to'` and the connection channel, sorted chronologically.

### UI & UX Layout
- **D-04 (Split-Pane Layout):** Implement a three-column layout: collapsible left sidebar | conversation list panel (300px width with search and channel filter tabs) | active chat messages panel (flex-grow).
- **D-05 (Alternating Bubbles):** Inbound messages display as left-aligned white cards with contact avatar initials. Outbound messages display as right-aligned blue (`#3b82f6`) cards with delivery checkmarks.
- **D-06 (Auto-Resize Input):** The chat message text input is a multiline textarea that auto-grows as the operator types (capped at 100px height), sending the message upon pressing Enter (unless Shift is held).

### Realtime & Interactivity
- **D-07 (Two-Tier Polling):** Use HTMX polling (`hx-trigger="every Xs"`) to achieve real-time updates without WebSockets:
  - Chat panel polls every 3s using `hx-swap="beforeend"` to append new messages dynamically.
  - Conversation list polls every 5s using `hx-swap="innerHTML"` to refresh previews and unread indicators.
- **D-08 (ID-Based Cursor):** The chat polling endpoint must query messages `after` the last-seen message UUID/row ID, never a timestamp, to avoid race conditions and clock skew issues.
- **D-09 (Background Notifications):** If new messages arrive for non-active conversations, trigger a lightweight in-page toast notification (fixed top-center, auto-dismiss in 3.5s) using HTMX custom headers/events. Do not request browser Notification permissions for the MVP.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Spike Findings (Blueprints)
- `.agents/skills/spike-findings-pergo/references/inbox-ui.md` — Conversation list, chat view, and real-time polling implementation blueprint.
- `.agents/skills/spike-findings-pergo/sources/006-inbox-realtime-polling/index.html` — Fully functional, interactive HTML/JS prototype of the two-tier polling, toast notifications, and visual styling.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets & Files
- `internal/api/handler/admin/inbox.go`: Existing handler for the audit-log search view. This handler and its endpoints must be modified or replaced to serve the conversational inbox partial templates.
- `templates/pages/inbox.templ`: Existing templates for the inbox page. Needs to be rewritten/split into modular templ components (list, panel, items, bubbles).
- `templates/layout/sidebar.templ`: Sidebar navigation file. The "Inbox" navigation link needs to be updated with an active link and unread badge target.

### Established Patterns
- **HTMX Partial Replacement:** Handlers detect HTMX requests (using `HX-Request` header check or separate routes) and return only the required HTML fragment rather than full base layouts.

</code_context>

<specifics>
## Specific Ideas
- High-contrast, clean layout matching the Notion-style dashboard design (Phase 8).
- Briefly highlight newly arrived messages with a subtle blue outline animation to draw operator attention.

</specifics>

<deferred>
## Deferred Ideas
- Persistent `contacts` database table (for resolving contact phone numbers to display names). Show `from` field (phone or bot username) directly for the MVP.
- WebSockets or SSE for real-time connection. Defer until load metrics demonstrate polling pressure on the database.
- Browser desktop notification API support.

</deferred>

---

*Phase: 09-conversational-inbox*
*Context gathered: 2026-07-03*
