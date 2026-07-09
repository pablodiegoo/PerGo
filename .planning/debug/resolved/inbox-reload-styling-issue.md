---
status: resolved
trigger: "inbox page style breakage on reload when chat is open"
created: 2026-07-09
updated: 2026-07-09
symptoms:
  expected: "The inbox page should reload normally, maintaining CSS styling, sidebar structure, and showing the open chat."
  actual: "The page loads a raw HTML fragment without CSS or sidebar layout, displaying massive icons."
  error_messages: "None"
  timeline: "Always occurred since inbox was implemented."
  reproduction: "1. Navigate to /admin/inbox\n2. Open any conversation (URL pushes to /admin/inbox/chat?from=...)\n3. Refresh the page"
resolution:
  root_cause: "The `/admin/inbox/chat` endpoint always rendered only the chat-panel HTML fragment. Because HTMX pushed this URL to the browser history (`hx-push-url='true'`), refreshing the page forced the browser to make a direct GET request to this endpoint. Without the base layout, it loaded raw HTML lacking CSS links, resulting in huge icons and layout breakage."
  fix: "Modified `InboxHandler.ChatPanel` to check if the request is an HTMX request. If it is a direct request (HTMX is false), the handler loads the conversation list and injects the active conversation pre-rendered component into the full `pages.InboxPage` layout. Modified `pages.InboxPage` and `pages.InboxContent` templates to accept and render this preloaded chat component."
  verification: "Ran `make test` successfully. A direct reload of the chat panel URL now serves the complete HTML layout with the target chat preloaded and styling preserved."
  files_changed:
    - "internal/api/handler/admin/inbox.go"
    - "templates/pages/inbox.templ"
    - "templates/pages/inbox_templ.go"
---

# Debug Session: inbox-reload-styling-issue

## Current Focus
- **hypothesis**: The `/admin/inbox/chat` route renders only the chat-panel fragment component. When visited directly via page reload, it serves this fragment directly instead of wrapping it in the base page layout.
- **test**: Check if `mw.IsHTMX(c)` is used in `InboxHandler.ChatPanel`. If false, it should load conversations and render the full `pages.InboxPage`.
- **expecting**: Page reloads on `/admin/inbox/chat` will render the full layout with the chat pre-opened.
- **next_action**: Completed.

## Evidence
- Direct page requests to `/admin/inbox/chat` without HTMX headers bypassed the base layout entirely, causing the raw fragment template (`components.ChatPanel`) to be served alone.

## Eliminated
- CSS file loading failure: The CSS files are properly served, but the browser never requested them because the fragment template did not include `<link>` tags.
