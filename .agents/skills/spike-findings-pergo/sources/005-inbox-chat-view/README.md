---
spike: 005
name: inbox-chat-view
type: standard
validates: "Given a selected conversation, when the operator clicks it, then a split-pane chat view loads with alternating message bubbles (inbound left, outbound right) without full page reload"
verdict: VALIDATED
related: [004]
tags: [ui, inbox, chat, htmx, split-pane]
---

# Spike 005: Inbox Chat View

## What This Validates
Given a conversation selected in the list, when the operator clicks it, then the right panel loads a full chat view with inbound messages as left-aligned bubbles (white) and outbound messages as right-aligned bubbles (blue).

## Research

### Split-pane HTMX pattern
```html
<div hx-get="/admin/inbox/c1/chat"
     hx-target="#chat-panel"
     hx-swap="innerHTML">
```
The server returns an HTML fragment. HTMX swaps it into #chat-panel. Sidebar and conversation list stay in place.

### Message thread query
Needs both directions:
- Inbound: audit_logs WHERE event_type='inbound_message' AND payload->>'from'=$contact
- Outbound: audit_logs WHERE event_type='outbound_message' AND payload->>'to'=$contact
- Union both, ORDER BY created_at ASC

### Bubble design
- Inbound: white bubble, left-aligned, avatar initial
- Outbound: blue bubble, right-aligned, no avatar, delivery checkmarks

## How to Run
open .planning/spikes/005-inbox-chat-view/index.html

## What to Expect
- Three-column layout: Sidebar | Conv list | Chat panel
- Opens automatically on first conversation
- Click any conversation, chat panel updates without page reload
- Alternating bubbles (inbound left, outbound right)
- Send messages: type + Enter → bubble appears immediately
- Date separators between message groups

## Investigation Trail

### Layout: CSS Grid vs Flexbox
Switched from grid to flexbox — more predictable overflow/scroll behavior for the messages area.

### Bubble corner radius
Inbound: border-bottom-left-radius:3px. Outbound: border-bottom-right-radius:3px. Standard chat tail UX.

### Textarea auto-resize
oninput: this.style.height='auto'; this.style.height=this.scrollHeight+'px' with max-height:100px.

### Gap discovered: outbound audit events
The spike uses mock data for outbound. In production, audit_logs with event_type='outbound_message' needs payload->>'to' field for thread stitching. Must verify before implementing.

## Results
**Verdict: VALIDATED**

Split-pane HTMX pattern is consistent with existing PerGo admin (audit filters, workspace selector). Chat bubble UX is solid.

**Gap to resolve:** confirm audit_logs outbound events have payload->>'to' field.
