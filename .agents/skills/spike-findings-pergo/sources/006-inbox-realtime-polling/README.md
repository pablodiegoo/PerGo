---
spike: 006
name: inbox-realtime-polling
type: standard
validates: "Given the chat panel is open, when a new inbound message arrives, then the panel updates automatically within the polling interval without user action"
verdict: VALIDATED
related: [004, 005]
tags: [ui, inbox, realtime, polling, htmx, notifications]
---

# Spike 006: Inbox Realtime Polling

## What This Validates
Given the chat panel is open, when a new inbound message arrives on the server, then the messages area appends the new bubble within 3 seconds without the user reloading the page.

## Research

### HTMX polling pattern
```html
<div id="messages-area"
     hx-get="/admin/inbox/{{convId}}/messages?after={{lastMsgId}}"
     hx-trigger="every 3s"
     hx-swap="beforeend">
```
Server returns only new messages as HTML fragments. HTMX appends them. Empty response = no DOM change.

### Two-tier polling rates
- Conversation list: every 5s (full innerHTML replace — cheap, simple)
- Chat panel: every 3s (beforeend append — smooth, no flicker)

### Polling vs SSE
- Polling at 3s: acceptable for operator console
- SSE would give sub-second latency but requires persistent connections
- Defer SSE to phase 2 if usage data demands it

### Critical constraint
The `after` parameter must be the last audit_log row ID (not a timestamp) to avoid race conditions with clock skew.

## How to Run
open .planning/spikes/006-inbox-realtime-polling/index.html

## What to Expect
- Opens with Carlos Mendes' chat loaded, green pulsing dot = polling active
- After 8s: new bubble appears in Carlos's chat (blue outline highlight for 2s)
- After 14s: toast notification for Ana Lima (not active chat)
- After 22s: another bubble in Carlos's chat
- After 30s: toast for Mariana Costa
- Activity log (bottom-right, toggle with Log button) shows every poll cycle
- Unread badge updates when messages arrive for background conversations

## Observability
Bottom-right activity log:
- [poll] — each polling cycle with simulated GET request
- [new] — when a new message is injected
- [send] — when operator sends a message

## Investigation Trail

### New message highlight
New bubbles get a 2-second blue outline highlight so operators catch them mid-scroll. Removes itself automatically.

### Toast vs browser notification
Toast (fixed top-center, auto-dismiss 3.5s) is right for MVP. Browser Notification API requires permission prompt — overkill for an operator tool. Defer.

### Polling validation
Each poll is a cheap indexed query: WHERE id > $lastId AND from=$contact AND channel=$channel. At 500 msg/s throughput and a reasonable number of active chat panels, this is well within PerGo's latency budget.

## Results
**Verdict: VALIDATED**

HTMX polling is the right approach for this product. 3s interval feels responsive without hammering the DB. Two-tier polling cleanly separates list and chat concerns.

**Key constraint:** Use row ID (not timestamp) for the `after` cursor.
