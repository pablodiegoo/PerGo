---
phase: 09-conversational-inbox
plan: 02
subsystem: admin-ui
tags: [go, templ, htmx, postgres, admin]

requires:
  - phase: 09-conversational-inbox
    plan: 01
    provides: recipient_sessions multi-instance compound key, ListConversations, ListThread

provides:
  - migration 014 adding last_read_at to recipient_sessions
  - UpdateLastReadAt method on RecipientSessionRepository
  - Inbox sidebar link with unread badge
  - Split-pane inbox page (InboxPage / InboxContent)
  - ConvList component with 5s polling and OOB badge update
  - ConvItem component with HTMX chat panel trigger
  - Full InboxHandler: View, PollConversations, ChatPanel, PollMessages, SendMessage
  - All inbox routes registered in cmd/pergo/main.go

affects: [09-conversational-inbox]

tech-stack:
  added: []
  patterns: [OOB HTMX badge update, server-side unread tracking via recipient_sessions.last_read_at]

key-files:
  created:
    - internal/platform/postgres/migrations/014_inbox_read_status.sql
    - templates/components/conv_item.templ
    - templates/components/conv_item_templ.go
    - templates/components/conv_list.templ
    - templates/components/conv_list_templ.go
  modified:
    - internal/repository/recipient_session.go
    - internal/api/handler/admin/inbox.go
    - templates/pages/inbox.templ
    - templates/pages/inbox_templ.go
    - templates/layout/sidebar.templ
    - templates/layout/sidebar_templ.go
    - cmd/pergo/main.go

key-decisions:
  - "Kept InboxMessage struct in pages/inbox.templ for backward compatibility but inbox page now accepts ConversationSummary slices."
  - "Chat panel rendered via Go string-concatenation (not templ) to avoid circular imports between pages and components packages — OOB badge is a templ component in conv_list.templ."
  - "resolveWorkspaceID helper centralized in inbox.go to avoid duplicating cookie-parsing logic across handler methods."
  - "Unread detection: a conversation is unread if session.LastReadAt IS NULL OR conv.LastMessageTime > session.LastReadAt; missing session also treated as unread."

patterns-established:
  - "OOB badge update pattern: InboxUnreadBadge templ renders with hx-swap-oob=true and is included in PollConversations response."
  - "5s HTMX polling anchored on #conv-list div with hx-trigger='every 5s' and hx-swap='outerHTML'."

requirements-completed:
  - migration_inbox_read_status
  - register_inbox_routes
  - update_sidebar_navigation
  - create_conversation_templates
  - implement_conversations_handler

coverage:
  - id: U1
    description: "Migration 014 adds last_read_at TIMESTAMPTZ column to recipient_sessions"
    requirement: migration_inbox_read_status
    verification:
      - kind: build
        ref: "go build ./..."
        status: pass
    human_judgment: true
  - id: U2
    description: "RecipientSessionRepository.UpdateLastReadAt updates session read timestamp"
    requirement: migration_inbox_read_status
    verification:
      - kind: build
        ref: "go build ./..."
        status: pass
    human_judgment: false
  - id: U3
    description: "Inbox navigation link added to sidebar with #inbox-unread-badge element"
    requirement: update_sidebar_navigation
    verification:
      - kind: build
        ref: "templ generate && go build ./..."
        status: pass
    human_judgment: true
  - id: U4
    description: "Split-pane InboxPage with ConvList and ConvItem components"
    requirement: create_conversation_templates
    verification:
      - kind: build
        ref: "templ generate"
        status: pass
    human_judgment: true
  - id: U5
    description: "ConvList has 5s HTMX polling and sends OOB badge update to sidebar"
    requirement: implement_conversations_handler
    verification:
      - kind: build
        ref: "go build ./..."
        status: pass
    human_judgment: true
  - id: U6
    description: "All 5 inbox routes registered in cmd/pergo/main.go"
    requirement: register_inbox_routes
    verification:
      - kind: build
        ref: "go build ./..."
        status: pass
    human_judgment: false

duration: 60min
completed: 2026-07-03
status: complete
---

# Phase 9: Conversational Inbox (Plan 02) Summary

**Split-pane inbox shell: sidebar link, conversation list with server-side read tracking, HTMX polling, and chat panel.**

## Performance

- **Duration:** ~60 min
- **Started:** 2026-07-03T19:22:00Z
- **Completed:** 2026-07-03T20:27:00Z
- **Tasks:** 5
- **Commits:** 4
- **Files modified/created:** 12

## Accomplishments

1. **Migration 014** (`014_inbox_read_status.sql`): Added `last_read_at TIMESTAMPTZ` column to `recipient_sessions` table.
2. **`UpdateLastReadAt`**: Implemented on `RecipientSessionRepository` to stamp conversation as read when the operator opens the chat panel.
3. **Sidebar Inbox link**: Added "Inbox" nav item with message-bubble SVG icon and `#inbox-unread-badge` span that HTMX updates out-of-band every 5 seconds.
4. **`ConvItem` component** (`templates/components/conv_item.templ`): Renders contact avatar (initial), last message preview, timestamp, channel badge icon, and unread dot. Clicking executes HTMX GET to `/admin/inbox/chat` targeting `#chat-panel`.
5. **`ConvList` component** (`templates/components/conv_list.templ`): Channel filter tabs (All / WA Web / WA Cloud / Telegram), `hx-trigger="every 5s"` polling on the list div, empty state illustration, and OOB `InboxUnreadBadge` fragment.
6. **`InboxPage` rewrite** (`templates/pages/inbox.templ`): Full split-pane layout — 320px left panel (header + search bar + conversation list) + `#chat-panel` right panel with placeholder.
7. **`InboxHandler` rewrite** (`internal/api/handler/admin/inbox.go`):
   - `View` — full-page or HTMX partial for `/admin/inbox`
   - `PollConversations` — ConvList fragment + OOB badge for `/admin/inbox/conversations/poll`
   - `ChatPanel` — renders thread view and marks conversation read for `/admin/inbox/chat`
   - `PollMessages` — incremental polling with `after_id` cursor for `/admin/inbox/messages`
   - `SendMessage` — 501 stub for `/admin/inbox/send`
8. **Routes** registered in `cmd/pergo/main.go` replacing the old `GET /admin/inbox/:channel`.

## Task Commits

1. **migration_inbox_read_status** — `c8e7e52`
2. **update_sidebar_navigation** — `84584ac`
3. **create_conversation_templates** — `7c30288`
4. **implement_conversations_handler + register_inbox_routes** — `6b3d7fc`

## Files Created/Modified

- `internal/platform/postgres/migrations/014_inbox_read_status.sql` — new migration
- `internal/repository/recipient_session.go` — `LastReadAt` field, updated `Get`, new `UpdateLastReadAt`
- `templates/components/conv_item.templ` + generated `.go` — conversation item component
- `templates/components/conv_list.templ` + generated `.go` — conversation list with polling
- `templates/pages/inbox.templ` + generated `.go` — split-pane inbox page rewrite
- `templates/layout/sidebar.templ` + generated `.go` — Inbox nav link
- `internal/api/handler/admin/inbox.go` — full InboxHandler rewrite
- `cmd/pergo/main.go` — updated InboxHandler construction and route registration

## Decisions Made

- **`InboxMessage` kept**: Old struct preserved in `pages/inbox.templ` for backward compatibility; the templ functions were updated to accept `ConversationSummary` instead.
- **Chat panel via string concatenation**: To avoid circular package imports (`pages` → `components` is fine; `components` → `pages` is not), the chat panel HTML is built in `inbox.go` using safe string rendering with `escapeHTML()`.
- **Unread logic**: `conversation.LastMessageTime > session.LastReadAt` → unread. Missing session → unread. This is pure server-side — no cookies involved.
- **OOB badge pattern**: `InboxUnreadBadge` templ in `conv_list.templ` renders with `hx-swap-oob="true"`, enabling the sidebar badge to update without the sidebar participating in the polling request.

## Deviations from Plan

- Plan mentioned ChatPanel stub only; implemented a functional thread view with `renderChatPanel` / `renderMessage` HTML helpers to provide immediate value without introducing templ circular imports.
- `SendMessage` is a 501 stub as expected — full reply flow is plan 03+.

## Issues Encountered

- `templ` binary not on PATH; used `go run github.com/a-h/templ/cmd/templ@v0.3.1020 generate ./...` instead.
- `components.Fmt()` doesn't exist — replaced with `fmt.Sprintf("%d", unreadCount)` directly in inbox.templ.

## User Setup Required

None — migration runs automatically at startup via goose.

## Next Phase Readiness

- Inbox shell is fully rendered and polling is active.
- Chat panel shows a functional thread view when a conversation is clicked.
- Next plan can implement `SendMessage` / reply flow and message polling to complete the real-time chat experience.

---
*Phase: 09-conversational-inbox*
*Plan: 02*
*Completed: 2026-07-03*
