# Inbox UI

## Requirements

Non-negotiable decisions from spike sessions — honor all of these in the real build:

- Conversations must be derived from `audit_logs` GROUP BY `(from, channel)` — no new `conversations` table needed for MVP
- Chat view must use split-pane layout: sidebar | conversation list (300px) | chat panel (flex-1)
- HTMX partial replacement drives all panel transitions — no full page reloads
- Message bubbles: inbound = left-aligned white card, outbound = right-aligned blue (#3b82f6)
- Realtime: chat panel polls every 3s (`hx-swap="beforeend"` with ID cursor), conversation list polls every 5s (full `innerHTML` replace)
- Unread notifications for background conversations via toast (fixed top-center, auto-dismiss 3.5s) — no browser Notification API for MVP
- Use row ID as the `after` cursor for polling — never a timestamp (avoids clock skew race conditions)

## How to Build It

### 1. Conversation List Query

Derive conversations from `audit_logs`. Add this method to `AuditRepository`:

```go
type ConversationSummary struct {
    Contact   string    // payload->>'from'
    Channel   string    // payload->>'channel'
    LastAt    time.Time
    MsgCount  int
    Preview   string    // last inbound message body
}

func (r *AuditRepository) ListConversations(ctx context.Context, workspaceID uuid.UUID, channel string) ([]ConversationSummary, error) {
    q := `
        SELECT
            payload->>'from'    AS contact,
            payload->>'channel' AS channel,
            MAX(created_at)     AS last_at,
            COUNT(*)            AS msg_count,
            (SELECT payload->>'body'
             FROM audit_logs a2
             WHERE a2.workspace_id = a1.workspace_id
               AND a2.payload->>'from'    = a1.payload->>'from'
               AND a2.payload->>'channel' = a1.payload->>'channel'
               AND a2.event_type = 'inbound_message'
             ORDER BY created_at DESC LIMIT 1) AS preview
        FROM audit_logs a1
        WHERE workspace_id = $1
          AND event_type = 'inbound_message'
          AND ($2 = '' OR payload->>'channel' = $2)
        GROUP BY payload->>'from', payload->>'channel'
        ORDER BY MAX(created_at) DESC`
    rows, err := r.pool.Query(ctx, q, workspaceID, channel)
    // ... scan with pgx CollectRows
}
```

**Index needed** for this query to be fast:
```sql
CREATE INDEX IF NOT EXISTS idx_audit_logs_inbound
  ON audit_logs ((payload->>'from'), (payload->>'channel'), workspace_id, created_at DESC)
  WHERE event_type = 'inbound_message';
```

### 2. Message Thread Query

To build the full thread (inbound + outbound), union both directions:

```go
type ThreadMessage struct {
    ID        uuid.UUID
    Direction string    // "inbound" | "outbound"
    Body      string
    CreatedAt time.Time
}

func (r *AuditRepository) ListThread(ctx context.Context, workspaceID uuid.UUID, contact, channel string, afterID *uuid.UUID) ([]ThreadMessage, error) {
    q := `
        SELECT id, 'inbound' AS direction, payload->>'body', created_at
        FROM audit_logs
        WHERE workspace_id = $1
          AND event_type = 'inbound_message'
          AND payload->>'from'    = $2
          AND payload->>'channel' = $3
          AND ($4::uuid IS NULL OR id > $4)
        UNION ALL
        SELECT id, 'outbound' AS direction, payload->>'body', created_at
        FROM audit_logs
        WHERE workspace_id = $1
          AND event_type = 'outbound_message'
          AND payload->>'to'      = $2
          AND payload->>'channel' = $3
          AND ($4::uuid IS NULL OR id > $4)
        ORDER BY created_at ASC`
    // ...
}
```

> ⚠️ **Gap to verify:** confirm `audit_logs` outbound events have `payload->>'to'` field in the real dispatch audit writer before implementing. Check `internal/channel/*/dispatcher.go` where audit events are written.

### 3. HTMX Route Handlers

Add to `InboxHandler`:

```go
// GET /admin/inbox/conversations[?channel=whatsapp]
// Returns full page or HTMX fragment (conv list only)
func (h *InboxHandler) ListConversations(c *echo.Context) error { ... }

// GET /admin/inbox/chat?contact=+5511...&channel=whatsapp
// Returns chat panel HTML fragment
func (h *InboxHandler) ChatPanel(c *echo.Context) error { ... }

// GET /admin/inbox/messages?contact=+5511...&channel=whatsapp&after=<uuid>
// Returns ONLY new messages as HTML fragments (for polling append)
// Returns empty body if no new messages — HTMX appends nothing
func (h *InboxHandler) PollMessages(c *echo.Context) error { ... }

// GET /admin/inbox/conversations/poll[?channel=whatsapp]
// Returns full conv list HTML fragment (for list polling)
func (h *InboxHandler) PollConversations(c *echo.Context) error { ... }
```

### 4. Template Structure (templ)

```
templates/pages/inbox.templ          ← full page (conv list + empty chat placeholder)
templates/components/conv_list.templ ← conversation list fragment (reused by polling)
templates/components/conv_item.templ ← single conversation row
templates/components/chat_panel.templ← full chat panel fragment
templates/components/message_bubble.templ ← single message bubble (inbound|outbound)
templates/components/inbox_toast.templ    ← new-message toast
```

### 5. HTMX Wiring in Templates

**Conversation list item** (triggers chat panel load):
```html
<div hx-get="/admin/inbox/chat?contact={{.Contact}}&channel={{.Channel}}"
     hx-target="#chat-panel"
     hx-swap="innerHTML"
     hx-push-url="/admin/inbox?conv={{.Contact}}&ch={{.Channel}}">
```

**Chat panel messages area** (polling for new messages):
```html
<div id="messages-area"
     hx-get="/admin/inbox/messages?contact={{.Contact}}&channel={{.Channel}}&after={{.LastMsgID}}"
     hx-trigger="every 3s"
     hx-swap="beforeend scroll:bottom">
```

**Conversation list** (polling for unread updates):
```html
<div id="conv-list"
     hx-get="/admin/inbox/conversations/poll?channel={{.ActiveChannel}}"
     hx-trigger="every 5s"
     hx-swap="innerHTML">
```

**Sidebar unread badge** update: include it inside the `conv-list` poll fragment so it refreshes together.

### 6. Sidebar Link

Add to `templates/layout/sidebar.templ` after "Conexões":

```html
<li>
  <a href="/admin/inbox" class="nav-item flex items-center gap-3 px-3 py-2 text-sm font-medium rounded-md text-zinc-600 hover:bg-zinc-200/50 hover:text-zinc-900 transition-all">
    <svg class="h-4 w-4" ...chat-bubble icon.../>
    <span>Inbox</span>
    <span id="inbox-unread-badge" class="ml-auto bg-blue-500 text-white text-xs rounded-full px-1.5 py-0.5 font-bold hidden">0</span>
  </a>
</li>
```

### 7. Toast for Background Conversations

When the polling endpoint detects a new message in a conversation other than the currently open one, include a toast trigger in the response header:

```go
// Server sets HX-Trigger header to fire a JS event
c.Response().Header().Set("HX-Trigger", `{"showToast":{"text":"Nova msg de +5511..."}}`)
```

Listen with:
```html
<body hx-on:show-toast="showToast(event.detail.text)">
```

Or simpler: check the `conv-list` poll response for a `data-has-new` attribute and fire the toast client-side.

## What to Avoid

- **Don't use a timestamp for the `after` cursor** — use the audit_log UUID/row ID. Timestamps have clock skew risk when multiple workers write concurrently.
- **Don't re-render the full messages area on each poll** — use `hx-swap="beforeend"`. Full replace causes scroll position jump.
- **Don't add a `conversations` table for MVP** — GROUP BY on `audit_logs` is sufficient and avoids a migration. Add a dedicated table only if query performance becomes measurable.
- **Don't use SSE/WebSockets for MVP** — HTMX polling at 3s is sufficient for an operator console. SSE requires persistent connection lifecycle management on the server.
- **Don't show contact names from a contacts DB that doesn't exist** — display the `from` field (phone/username) directly. Names are a phase 2 concern.
- **Don't use browser Notification API** — requires permission prompt, overkill for an admin tool. Use the in-page toast.

## Constraints

- **outbound audit field gap:** Must verify `audit_logs` outbound events have `payload->>'to'` before implementing the thread query. If absent, only inbound messages can be shown in the thread for now.
- **HTMX `beforeend` + scroll:** Use `scroll:bottom` modifier on the poll target so new messages auto-scroll into view.
- **Index requirement:** The GROUP BY conversation query is expensive without the index on `(payload->>'from', payload->>'channel')`. Add in migration before deploying.
- **Admin auth:** All `/admin/inbox/*` routes must go through the existing admin auth middleware — same as all other admin routes.
- **Workspace scoping:** All queries must be scoped to `workspace_id` — never query cross-workspace.

## Origin

Synthesized from spikes: 004, 005, 006

Source files available in:
- `sources/004-inbox-conversation-list/` — conversation list prototype with channel filter and search
- `sources/005-inbox-chat-view/` — full split-pane chat with message bubbles and send
- `sources/006-inbox-realtime-polling/` — realtime polling simulation with activity log and toasts
