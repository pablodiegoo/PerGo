---
spike: 004
name: inbox-conversation-list
type: standard
validates: "Given inbound messages in audit_logs grouped by (from, channel), when an operator opens Inbox, then they see a conversation list with previews and channel filter"
verdict: VALIDATED
related: [001, 002]
tags: [ui, inbox, admin, conversations]
---

# Spike 004: Inbox Conversation List

## What This Validates
Given inbound messages stored in audit_logs (event_type='inbound_message') with JSONB payload containing `from`, `body`, `channel`, when an operator opens the Inbox page, then they see conversations grouped by sender, with channel filter tabs, unread indicators, search, and last-message preview.

## Research

### The Conversation Data Model
PerGo has no dedicated conversations table. Conversations must be derived from audit_logs by grouping on (workspace_id, payload->>'from', payload->>'channel') with the last message as preview. This is a GROUP BY query — no schema changes needed for MVP.

SQL shape:
```sql
SELECT 
  payload->>'from' AS contact,
  payload->>'channel' AS channel,
  MAX(created_at) AS last_at,
  COUNT(*) AS msg_count,
  (SELECT payload->>'body' FROM audit_logs a2
   WHERE a2.workspace_id = a1.workspace_id
     AND a2.payload->>'from' = a1.payload->>'from'
     AND a2.payload->>'channel' = a1.payload->>'channel'
     AND a2.event_type = 'inbound_message'
   ORDER BY created_at DESC LIMIT 1) AS preview
FROM audit_logs a1
WHERE workspace_id = $1 AND event_type = 'inbound_message'
GROUP BY payload->>'from', payload->>'channel'
ORDER BY last_at DESC
```

## How to Run
open .planning/spikes/004-inbox-conversation-list/index.html

## What to Expect
- Left sidebar with Inbox highlighted and unread badge (3)
- Conversation list with name, channel badge (WA Web / WABA / Telegram), last-message preview, timestamp
- Unread indicator dots on unread conversations
- Channel filter tabs: click WA Web to show only WhatsApp conversations
- Search box filters by name, phone, or message content
- Clicking a conversation marks it selected (blue left-border highlight)

## Investigation Trail

### Key Finding: Contact name resolution
The audit_logs.payload only has `from` (phone/username) and `body`. There's no contact name stored. For MVP, display the phone number directly. A contacts table can be added later.

### Channel filter tabs vs dropdown
Tried dropdown first, switched to pill-style tabs — scans faster for operators juggling multiple channels.

## Results
**Verdict: VALIDATED**

Conversation list UX works cleanly with the existing audit_logs data model. No schema changes needed for MVP. Channel filter tabs are the right interaction pattern.

**Requirements captured:**
- Conversations derived from audit_logs GROUP BY (from, channel) — no new table needed for MVP
- Channel filter tabs for fast scanning
- Must show: contact identifier, channel badge, last message preview, timestamp, unread dot
- Clicking a conversation must open chat without full page reload (HTMX)
