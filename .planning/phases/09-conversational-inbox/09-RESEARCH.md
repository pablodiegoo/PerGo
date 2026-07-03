# Phase 9 Research: Conversational Inbox

To plan and implement Phase 9 (Conversational Inbox) successfully, there are several key technical, architectural, and design patterns that must be followed. This research document compiles the validated patterns, answers the planning questions, and highlights crucial implementation gaps identified during the research.

---

## Executive Summary: What to Know to PLAN Well

1. **Leverage the Audit Log (D-01):** Do not create a separate `conversations` database table. Derive active conversations dynamically from the `audit_logs` table.
2. **Correct SQL JSONB Paths:** Outbound audit logs wrap the message inside a `"request"` JSON key (unlike inbound logs). Queries for outbound messages must check `payload->'request'->>'to'` and `payload->'request'->>'body'`, whereas inbound logs use `payload->>'from'` and `payload->>'body'`.
3. **Address the Multi-Instance Isolation Gap:** To prevent merging conversations from different WhatsApp Web instances or Telegram bots into a single thread, we must introduce the recipient identity (`to` or `recipient_identity`) into the inbound message audit logs.
4. **Implement Two-Tier HTMX Polling (D-07):** Wire up a 3-second poll for the active chat panel (appending only new messages using a UUID cursor) and a 5-second poll for the conversation list panel.
5. **Database-Free Read/Unread State:** Track read/unread states via a lightweight, database-free cookie (`pergo-inbox-read-state`) containing a JSON map of `contact:last_read_timestamp`.

---

## 1. Database Schema, Queries, and Performance Indexes

### A. Dynamic Conversation List Query
To build the conversation list panel, we group by contact, channel, and recipient identity. However, if we only check `inbound_message` events, the list won't update when the operator sends a reply. 

The optimized SQL query below groups by inbound messages to establish the active contacts but dynamically subqueries the absolute latest message (inbound or outbound) for the preview and timestamp, bubble sorting active chats to the top:

```sql
SELECT
    payload->>'from'    AS contact,
    payload->>'channel' AS channel,
    payload->>'to'      AS recipient_identity,
    -- Get the absolute latest message timestamp (inbound or outbound)
    COALESCE(
        (SELECT MAX(created_at)
         FROM audit_logs a2
         WHERE a2.workspace_id = a1.workspace_id
           AND (
               (a2.event_type = 'inbound_message' AND a2.payload->>'from' = a1.payload->>'from' AND a2.payload->>'channel' = a1.payload->>'channel' AND a2.payload->>'to' = a1.payload->>'to')
               OR 
               (a2.event_type = 'outbound_message' AND a2.payload->'request'->>'to' = a1.payload->>'from' AND a2.payload->'request'->>'channel' = a1.payload->>'channel' AND a2.payload->'request'->>'sender_identity' = a1.payload->>'to')
           )
        ),
        MAX(created_at)
    ) AS last_at,
    COUNT(*) AS msg_count,
    -- Get the body of the absolute latest message
    (SELECT 
        CASE 
            WHEN a3.event_type = 'inbound_message' THEN a3.payload->>'body'
            WHEN a3.event_type = 'outbound_message' THEN a3.payload->'request'->>'body'
        END
     FROM audit_logs a3
     WHERE a3.workspace_id = a1.workspace_id
       AND (
           (a3.event_type = 'inbound_message' AND a3.payload->>'from' = a1.payload->>'from' AND a3.payload->>'channel' = a1.payload->>'channel' AND a3.payload->>'to' = a1.payload->>'to')
           OR 
           (a3.event_type = 'outbound_message' AND a3.payload->'request'->>'to' = a1.payload->>'from' AND a3.payload->'request'->>'channel' = a1.payload->>'channel' AND a3.payload->'request'->>'sender_identity' = a1.payload->>'to')
       )
     ORDER BY a3.created_at DESC LIMIT 1) AS preview
FROM audit_logs a1
WHERE workspace_id = $1
  AND event_type = 'inbound_message'
  AND ($2 = '' OR payload->>'channel' = $2)
GROUP BY payload->>'from', payload->>'channel', payload->>'to'
ORDER BY last_at DESC;
```

### B. Message Thread Query (D-03)
To build the chronological conversation thread, we UNION inbound and outbound audit messages. Because outbound payloads wrap metadata in a `"request"` block, we must resolve the paths carefully:

```sql
SELECT id, 'inbound' AS direction, payload->>'body' AS body, created_at
FROM audit_logs
WHERE workspace_id = $1
  AND event_type = 'inbound_message'
  AND payload->>'from'    = $2
  AND payload->>'channel' = $3
  AND ($4::uuid IS NULL OR id > $4)
UNION ALL
SELECT id, 'outbound' AS direction, payload->'request'->>'body' AS body, created_at
FROM audit_logs
WHERE workspace_id = $1
  AND event_type = 'outbound_message'
  AND payload->'request'->>'to'      = $2
  AND payload->'request'->>'channel' = $3
  AND ($4::uuid IS NULL OR id > $4)
ORDER BY created_at ASC;
```

### C. Performance Index (D-02)
To ensure sub-millisecond query times on large datasets:
```sql
CREATE INDEX IF NOT EXISTS idx_audit_logs_inbound_grouping
  ON audit_logs (workspace_id, event_type, (payload->>'from'), (payload->>'channel'), created_at DESC);
```

---

## 2. Multi-Instance Thread Isolation Gap & Solution

> [Spacer alert]
> **The Thread Merging Issue:** If a workspace has multiple connections of the same channel type (e.g. WhatsApp Web Connection A and Connection B), and a contact messages both numbers, grouping by just `(from, channel)` merges these chats. Additionally, the system won't know which connection to dispatch replies from.

### Recommended Fix
Add a `to` (recipient JID/phone ID) property to the inbound event payload in `internal/session/inbound_processor.go`, `waba_webhook.go`, and `telegram_webhook.go`. 

1. **WhatsApp Web:** Pass the client JID (`wc.JID().String()`) into `InboundProcessor.Handle`.
2. **WABA:** Extract the recipient phone number ID from the metadata payload.
3. **Telegram:** Pass the bot's username.

By logging this value, we can query and group on `(from, channel, to)` ensuring complete conversation isolation.

---

## 3. Backend Routes and Handler Architecture

Modify or extend `internal/api/handler/admin/inbox.go` to provide the following endpoints:

| Endpoint | Method | Response Type | Description |
|----------|--------|---------------|-------------|
| `/admin/inbox` | `GET` | Full HTML Page | Renders the split-pane dashboard, sidebar, conversation list, and active chat placeholder. |
| `/admin/inbox/chat` | `GET` | HTML Fragment | Renders the full chat panel for a selected `contact` and `channel`. |
| `/admin/inbox/messages` | `GET` | HTML Fragment | Returns only new messages (if any) since the `after` UUID parameter. |
| `/admin/inbox/conversations/poll` | `GET` | HTML Fragment | Returns the updated conversation list panel for background refresh. |
| `/admin/inbox/send` | `POST` | HTML/No Content | Enqueues a reply to the NATS `messages.outbound` queue. |

### Active Workspace Resolution
Use the pattern from `dashboard.go` to extract the workspace ID from the `"pergo-active-workspace"` cookie, falling back to the first workspace or creating a default one if none exists:
```go
cookie, err := c.Cookie("pergo-active-workspace")
if err == nil && cookie != nil && cookie.Value != "" {
    wsID, _ = uuid.Parse(cookie.Value)
}
```

---

## 4. Real-time Polling & UI State Management

### A. Two-Tier HTMX Triggers
Wired directly into the compiled `templ` layouts:
* **Chat Message Polling (3s):**
  ```html
  <div id="messages-area"
       hx-get="/admin/inbox/messages?contact={{.Contact}}&channel={{.Channel}}&after={{.LastMsgID}}"
       hx-trigger="every 3s"
       hx-swap="beforeend scroll:bottom">
  ```
* **Conversation List Polling (5s):**
  ```html
  <div id="conv-list"
       hx-get="/admin/inbox/conversations/poll?channel={{.ActiveChannel}}"
       hx-trigger="every 5s"
       hx-swap="innerHTML">
  ```

### B. Database-Driven Read/Unread State
Instead of tracking unread state in client-side cookies (which limits cross-device consistency and breaks team collab), store the read status server-side:
* Add a `last_read_at` TIMESTAMPTZ column to the `recipient_sessions` table.
* When the chat panel loads, the server updates the `last_read_at` timestamp for the selected session `(workspace_id, recipient_phone, channel, recipient_identity)`.
* When rendering the conversation list, the server compares the conversation's `LastAt` with the session's `last_read_at` to show the unread dot indicator.

### C. Background Event Toast Notifications
If the messages polling endpoint detects a message for a conversation *other* than the one currently open, set the `HX-Trigger` header to fire a toast:
```go
c.Response().Header().Set("HX-Trigger", `{"showToast":{"text":"Nova mensagem de +5511..."}}`)
```
On the client side:
```html
<body hx-on:show-toast="showToast(event.detail.text)">
```

---

## 5. UI Layout & Design Details

* **Split-Pane Layout:** Collapsible left sidebar | conversation list (300px with search/tabs) | chat panel (flex-grow).
* **Alternating Bubbles:** Inbound = left-aligned white cards with initials avatar. Outbound = right-aligned blue (`#3b82f6`) cards with delivery checkmarks.
* **Auto-Grow Textarea:** The reply box auto-expands from 1 to 5 lines (max 100px height) and sends the message on pressing `Enter` (retaining `Shift+Enter` for newlines).
* **Arrival Highlight:** New messages slide in smoothly and flash with a subtle blue outline animation to grab the operator's attention.

---

## 6. What to Avoid (Traps & Pitfalls)

* ❌ **Do not use client-side cookies for unread state:** Storing a map of contact unread statuses in a cookie easily overflows the 4KB browser limit and prevents multiple operators from seeing synchronized read status. Store it on the server in the `recipient_sessions` table.
* ❌ **Do not use timestamps for polling cursors:** Use message UUIDs or row IDs. Timestamps can cause missed messages due to clock skew or database latency.
* ❌ **Do not re-render the entire chat window on message poll:** Use `hx-swap="beforeend"` to append new messages. Full swaps cause scroll position jumps and break textarea focus.
* ❌ **Do not implement SSE or WebSockets for the MVP:** Simple HTMX polling meets the requirements and avoids connection lifecycle weight.
* ❌ **Do not request browser Notification permissions:** Use lightweight, non-intrusive in-page toasts for background alerts.
* ❌ **Do not connect to a contacts table:** Show phone numbers/usernames directly.

---

*Phase: 09-conversational-inbox*  
*Research compiled: 2026-07-03*  
