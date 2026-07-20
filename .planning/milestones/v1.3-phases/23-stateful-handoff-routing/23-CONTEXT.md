# Phase 23: Stateful Handoff Routing - Context

**Gathered:** 2026-07-17
**Status:** Ready for planning

<domain>
## Phase Boundary

This phase delivers stateful handoff controls between automated chatbot responses (Typebot) and human operator intervention (Chatwoot & PerGo admin inbox). It prevents crosstalk by introducing a stateful `bot_active` flag per contact, pausing the bot on human replies (webhook or compose form), implementing a `pause_bot` messaging verb, exposing a manual toggle status badge in the chat UI, and resetting bot activity after a 12-hour operator inactivity cooldown.

</domain>

<decisions>
## Implementation Decisions

### Contact Table & Session Control
- **D-01 (HAND-01):** Add `bot_active` (boolean, defaults to `true`) and `bot_paused_at` (timestamp, nullable) columns to the `contacts` table. Update the Go domain model `domain.Contact` and repository methods.
- **D-02 (HAND-02):** In `TypebotForwarder.SyncInboundMessage` (`internal/integration/typebot/forwarder.go`), intercept inbound messages and check if `bot_active` is `true`. Only forward customer messages to Typebot if bot activity is active.

### Human Agent Auto-Pause (Chatwoot & Compose Form)
- **D-03 (HAND-03):** When a human agent reply webhook event is received from Chatwoot, update the contact's `bot_active` flag to `false` and set `bot_paused_at = NOW()`.
- **D-04 (Inbox Compose Form):** When an operator sends a message directly using PerGo's own admin Inbox compose area, automatically toggle `bot_active` to `false` and record `bot_paused_at = NOW()`.

### Messaging Verbs Engine Extension
- **D-05 (HAND-04):** Extend the Messaging Verbs Engine (`internal/webhook/verbs.go`) with the `pause_bot` verb. If the `duration` parameter is omitted, pause the bot indefinitely. If `duration` is specified (e.g. "2h"), parse it, update the contact, and apply the pause.

### Manual UI Toggle Button
- **D-06 (HAND-05):** In the Inbox chat panel header (`templates/components/chat_panel.templ`), display a status badge toggle (e.g., green "Bot Ativo" or grey "Bot Pausado") next to the contact name. Toggling triggers an HTMX request to update the state.

### Inactivity Cooldown Reset
- **D-07 (HAND-06):** Implement auto-reset via lazy evaluation on message ingestion. When a new customer message is processed by `InboundProcessor`, if `bot_active == false` and it has been more than 12 hours since the contact's last human message, automatically set `bot_active` to `true` and forward the message to the bot.

### the agent's Discretion
- The exact color scheme/styling of the status badge toggle to match existing UI brand guidelines.
- The precise database query implementation for fetching the last human message timestamp to calculate the cooldown.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Core Specifications
- `docs/PRD PerGo.md` — Core requirements and provider definitions
- `.planning/REQUIREMENTS.md` — Project requirements and traceability matrix

### Code References
- `internal/repository/contact.go` — Contacts and identities database interactions
- `internal/integration/typebot/forwarder.go` — Inbound message forwarding to Typebot
- `internal/webhook/verbs.go` — Declarative webhook actions execution engine
- `internal/inbound/processor.go` — Central customer messages ingestion pipeline
- `templates/components/chat_panel.templ` — Chat thread pane component and compose form

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `ContactRepository.ResolveContact` and `GetByID` can be updated to include the new columns.
- `VerbsEngine` context execution logic is reused for logging the custom `pause_bot` verb action.

### Established Patterns
- Multi-tenant workspace partitioning via context-aware `tenant.WorkspaceID(ctx)` is strictly maintained in all repository query lookups and API handler endpoints.
- Web UI layouts leverage TailwindCSS utility classes and HTMX attributes (`hx-post`, `hx-target`) for live partial updates.

### Integration Points
- Intercepting inbound messages in `SyncInboundMessage` before executing Typebot API calls.
- Updating `bot_active` inside the Chatwoot webhook handler (`internal/api/handler/chatwoot_webhook.go`).
- Inserting `pause_bot` logic inside the verbs execution loop in `internal/webhook/verbs.go`.

</code_context>

<specifics>
## Specific Ideas

- The status badge in the chat panel header should clearly indicate the bot status and change color depending on state (e.g. `bg-emerald-50 text-emerald-700` vs `bg-zinc-100 text-zinc-600`).
- Cooldown check should compare current time against `bot_paused_at` or the latest outgoing message dispatch timestamp.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>

---

*Phase: 23-Stateful Handoff Routing*
*Context gathered: 2026-07-17*
