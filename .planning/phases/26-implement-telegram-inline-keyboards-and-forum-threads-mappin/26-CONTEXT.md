# Phase 26: Implement Telegram Inline Keyboards and Forum Threads mapping - Context

**Gathered:** 2026-07-20
**Status:** Ready for planning

<domain>
## Phase Boundary

Map Telegram inline keyboards and forum threads into the unified payload schema, handling inbound button clicks (callback queries) and correctly routing forum thread context.

</domain>

<decisions>
## Implementation Decisions

### Telegram Keyboards
- **D-01:** Only Inline Keyboards for now. They map perfectly to the unified interactive button schema we built in Phase 25. Reply keyboards are rare in modern chat apps.

### Forum Threads Mapping
- **D-02:** Treat them as metadata (`thread_id`) on the existing Group Contact. This keeps the Contact representing the actual group entity, and replies can just specify the `thread_id` to route correctly.

### Callback Query Routing
- **D-03:** Ingest as standard inbound messages but with a specialized `interactive` or `button_reply` structure/flag in the inbound schema so developers can easily match button clicks.

### the agent's Discretion
None
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Architecture
- `.planning/codebase/ARCHITECTURE.md` — Inbound and Outbound architectural diagrams and boundaries.
- `.planning/codebase/INTEGRATIONS.md` — Telegram adapter specifics.
- `.planning/codebase/STACK.md` — Core tech stack components.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/api/handler/message.go`: The `POST /messages` ingestion gateway.
- `internal/channel/telegram/inbound.go` & `telegram.go`: The existing Telegram webhook listener and dispatcher.

### Established Patterns
- Outbound messages are serialized to `domain.QueueMessage` and published to NATS JetStream `messages.outbound` subject.
- Interactive schema already established in Phase 25.

### Integration Points
- Public API payload schema for inbound messages needs the `interactive` or `button_reply` structure.
- Telegram inbound webhook handler needs to parse `CallbackQuery` events and map them into the `button_reply` structure.
- Telegram inbound webhook handler needs to extract `message_thread_id` and attach it as metadata.

</code_context>

<specifics>
## Specific Ideas

No specific requirements — open to standard approaches

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope

</deferred>

---

*Phase: 26-Implement Telegram Inline Keyboards and Forum Threads mapping*
*Context gathered: 2026-07-20*
