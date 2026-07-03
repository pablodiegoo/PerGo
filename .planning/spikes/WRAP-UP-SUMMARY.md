# Spike Wrap-Up Summary

**Date:** 2026-07-03
**Spikes processed:** 3 (004, 005, 006)
**Feature areas:** Inbox UI
**Skill output:** `./.agents/skills/spike-findings-pergo/`

## Processed Spikes

| # | Name | Type | Verdict | Feature Area |
|---|------|------|---------|--------------| 
| 004 | inbox-conversation-list | standard | VALIDATED | Inbox UI |
| 005 | inbox-chat-view | standard | VALIDATED | Inbox UI |
| 006 | inbox-realtime-polling | standard | VALIDATED | Inbox UI |

## Key Findings

**Data model:** No new `conversations` table needed for MVP. Conversations derived from `audit_logs` GROUP BY `(from, channel)`. A JSONB index on `payload->>'from'` and `payload->>'channel'` is required for acceptable query performance.

**UI pattern:** Three-column split-pane (sidebar 220px | conv list 300px | chat flex-1). HTMX partial replacement for all panel transitions — consistent with existing PerGo admin pattern (audit filters, workspace selector). No JS framework needed.

**Realtime:** HTMX two-tier polling validated as right approach for operator console scale:
- Chat panel: every 3s, `hx-swap="beforeend"` with UUID cursor (prevents clock skew)
- Conversation list: every 5s, `hx-swap="innerHTML"` (simpler, cheap)

**Unread UX:** Toast notification (fixed top-center, auto-dismiss 3.5s) for messages arriving in non-active conversations. Sidebar badge count updates via conv-list poll response.

**Gap to resolve before implementation:** Confirm `audit_logs` outbound events carry `payload->>'to'` field. Check the dispatch audit writer in `internal/channel/*/dispatcher.go`.

**Deferred to phase 2:** Contact display names (show `from` phone/username in MVP), SSE/WebSocket (polling sufficient), browser Notification API.
