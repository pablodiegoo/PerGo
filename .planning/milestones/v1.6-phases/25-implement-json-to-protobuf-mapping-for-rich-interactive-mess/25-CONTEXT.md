# Phase 25: Implement JSON-to-Protobuf mapping for rich interactive messages (hybrid approach) - Context

**Gathered:** 2026-07-20
**Status:** Ready for planning

<domain>
## Phase Boundary

Support rich interactive messages (lists/buttons) with a channel override escape hatch in the `POST /messages` payload, focusing on mapping a unified JSON schema to Protobuf for WABA/WhatsApp Web.

</domain>

<decisions>
## Implementation Decisions

### Validation Strictness
- **D-01:** Defer to adapter: The HTTP gateway only validates that the unified JSON schema is well-formed. The channel adapter enforces specific limits (e.g., max 3 buttons) and fails the dispatch if violated.

### Override Conflict Resolution
- **D-02:** Complete replacement: As a premium CPaaS, predictability is key. If a developer uses an escape hatch, they want total control over the payload for that channel. Deep-merging complex JSON structures leads to unpredictable edge cases. If `channel_overrides.whatsapp` is provided, it completely ignores the unified interactive components (buttons/lists) and sends the override payload as-is.

### Fallback Degradation
- **D-03:** Configurable per-message: Add a `fallback_behavior` flag (e.g., `degrade` or `fail`) to the payload. Some interactive messages are critical (fail if unsupported), while others are just enhancements (degrade to text).

### the agent's Discretion
None

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Architecture
- `.planning/codebase/ARCHITECTURE.md` — Inbound and Outbound architectural diagrams and boundaries.
- `.planning/codebase/INTEGRATIONS.md` — WhatsApp Web (whatsmeow) and WABA adapter specifics.
- `.planning/codebase/STACK.md` — Core tech stack components.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/api/handler/message.go`: The `POST /messages` ingestion gateway where basic schema validation and `fallback_behavior` flag processing will happen.
- `internal/platform/queue/worker.go`: Message queue worker where dispatch to specific adapters occurs.
- Whatsmeow and WABA channel adapters where the validation limits and Protobuf mapping/construction occur.

### Established Patterns
- Outbound messages are serialized to `domain.QueueMessage` and published to NATS JetStream `messages.outbound` subject.
- Validation at gateway is kept lightweight; domain/channel specifics are handled downstream.

### Integration Points
- Public API payload schema for `POST /messages` needs to be extended to accept unified interactive components, `fallback_behavior`, and `channel_overrides.whatsapp`.
- Channel Dispatcher needs to evaluate `fallback_behavior` when choosing the fallback strategy.

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

*Phase: 25-Implement JSON-to-Protobuf mapping for rich interactive messages (hybrid approach)*
*Context gathered: 2026-07-20*
