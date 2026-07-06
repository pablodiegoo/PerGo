# Phase 10: Inbox Refactoring & Connection Unification - Context

**Gathered:** 2026-07-06
**Status:** Ready for planning

<domain>
## Phase Boundary

This phase refactors the conversational inbox to resolve live polling stability bugs, unifies all connection management (WhatsApp Web, WABA Cloud, Telegram Bot) under a single screen with modal-based creation/pairing, decommissions the standalone developer playground by integrating connectivity tests directly into connection rows, and implements Meta WABA 24-hour service window enforcement and template composition inside the inbox interface.

</domain>

<decisions>
## Implementation Decisions

### Polling Stability & Auto-Scroll
- **D-01 (Out-Of-Band Polling Anchor)**: To prevent infinite loops, duplicate event listeners, and query resets, the `PollMessages` handler must return new message bubbles along with an updated `#chat-poll-anchor` div having `hx-swap-oob="true"` and `hx-get` updated with the newest `after_id` cursor.
- **D-02 (Zero-JS Scroll)**: Use `hx-swap="beforeend scroll:bottom"` on the poll anchor and `chat-messages` target. Eliminate all custom DOM scroll or swap event listeners in the `.templ` page script block.

### Unified Connections Dashboard
- **D-03 (Consolidated Connection List)**: Repurpose the `/admin/devices` (or `/admin/connections`) endpoint to render a unified connections dashboard listing all connection records (WhatsApp Web, WABA Cloud, Telegram Bot) with their channel icon, identity, status badge, and connected timestamp.
- **D-04 (Creation & QR Pairing Modal)**: Clicking "Nova Conexão" opens a modal. Selecting the channel type dynamically adjusts the fields via simple JS.
  - *WhatsApp Web*: Starts the Whatsmeow pairing flow via `Manager.StartPairing` and polls/renders the QR code fragment directly inside the modal.
  - *WABA / Telegram Bot*: Submits form to validate credentials synchronously (meta templates sync or Telegram `getMe`), registers webhooks, and creates connection.
- **D-05 (Decommission Playground)**: Remove `/admin/playground` endpoints and references. Add a "Testar" button to each connection row that opens a test send modal, dispatching a message and showing a live NATS trace/debug output inline.

### Inbox WABA Templates & 24h Window
- **D-06 (WABA 24h Blocker Banner)**: If the active conversation uses WABA (`whatsapp_cloud`) and the last customer inbound message is older than 24 hours, disable the text area and submit button. Display a high-contrast banner: *"Janela de 24h fechada. Envie um template para reabrir a conversa."*
- **D-07 (Template Parameter Composer)**: Add a "Templates" button for WABA chats. Clicking it opens a modal containing a template selection dropdown and input fields for each variable (e.g. `{{1}}`, `{{2}}`), sending the structured payload via `POST /admin/inbox/send`.
- **D-08 (New Thread Initiator)**: Implement a "Novo Chat" button above the conversation list. Choosing a WABA connection disables free-text inputs and forces selecting/composing a template, while WhatsApp Web/Telegram allows raw text body.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Spike Findings (Blueprints)
- `.planning/spikes/007-inbox-polling-stability/README.md` — Stable OOB cursor-polling blueprint.
- `.planning/spikes/007-inbox-polling-stability/main.go` — Go mock server verifying all polling, modal connections, QR pairing, WABA 24h window blocking, and template composing flows.
- `.planning/spikes/008-connection-management-unification/README.md` — Connection unification reference.
- `.planning/spikes/009-waba-template-inbox-delivery/README.md` — WABA templates & customer service window reference.

</canonical_refs>

<code_context>
## Existing Code Insights

### Files to Modify
- `internal/api/handler/admin/device.go`: Reorganize to list all connections, handle unified creation, QR polling, and testing.
- `internal/api/handler/admin/inbox.go`: Implement 24h window check, template list resolver, and template send payloads.
- `internal/api/handler/admin/playground.go`: Remove or retire.
- `templates/pages/devices.templ`: Replace with unified connections template.
- `templates/pages/inbox.templ`: Add "Novo Chat", template selectors, and 24h window conditional renders.
- `templates/layout/sidebar.templ`: Remove "Playground" link.
- `cmd/pergo/main.go`: Update route registrations.

</code_context>

---

*Phase: 10-inbox-refactoring-connection-unification*
*Context gathered: 2026-07-06*
