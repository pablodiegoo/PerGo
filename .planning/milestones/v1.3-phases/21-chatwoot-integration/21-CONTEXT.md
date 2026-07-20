# Phase 21: Chatwoot Integration - Context

**Gathered:** 2026-07-17
**Status:** Ready for planning

<domain>
## Phase Boundary

Enable human agent messaging synchronization by implementing the Chatwoot connection settings panel, the built-in inbound webhook receiver adapter (`POST /api/integrations/chatwoot`), and outbound message ingestion/dispatch pipelines.

</domain>

<decisions>
## Implementation Decisions

### Credentials & Config Storage
- **D-01:** Implement a unified `integrations` database table to hold configurations for all current and future third-party integrations (Chatwoot, Typebot, etc.) instead of creating dedicated tables per service.
- **D-02:** Store integration-specific credentials (API URL, Access Token, Inbox ID, Account ID) as an encrypted JSON block/envelope inside a single `config` `BYTEA` column, encrypted using PerGo's existing `AES-256-GCM` encryption pipeline.

### Contact & Conversation Mapping
- **D-03:** Maintain a local mapping database table (`chatwoot_mappings` or `chatwoot_conversations`) that maps local `contact_id` to Chatwoot's `chatwoot_contact_id` and `chatwoot_conversation_id`.
- **D-04:** Check the local mapping table before routing inbound messages to Chatwoot. If mapped, post directly to the mapped conversation in a single API call. If not, perform the contact search/create, create conversation, save mapping, and then post.

### Webhook Authentication
- **D-05:** Secure the inbound webhook receiver endpoint (`POST /api/integrations/chatwoot`) using PerGo's existing query-parameter API key validation (`?token=YOUR_API_KEY`).

### Outbound Message Filtering
- **D-06:** Filter Chatwoot webhook payloads using properties from the JSON payload. Process only messages where `message_type == "outgoing"`, `private == false`, and `sender.type == "user"` (human agent reply) to avoid echo loops and system notes.

### the agent's Discretion
- Exact mapping database schema naming.
- Chatwoot client API wrapper implementation details.
- Error handling/retry strategies for Chatwoot API failures.

</decisions>

<canonical_refs>
## Canonical References

### Webhook Subscriptions & Security
- `.planning/milestones/v1.2-REQUIREMENTS.md` — Includes previous milestone security and webhook validation requirements.

</canonical_refs>

<specifics>
## Specific Ideas

- The query parameter authentication `?token=KEY` aligns with PerGo's standard API Key parsing patterns, ensuring operators do not have to configure separate complex signature secrets.

</specifics>

<code_context>
## Existing Code Insights

### Reusable Assets
- `AuthMiddleware` / API Key validation: Reuses standard query parameter token extraction to authenticate the inbound Chatwoot webhook.
- Cryptography helper: Reuses `internal/platform/postgres` encryption helpers for encrypting the `integrations.config` field.

### Established Patterns
- RADIX routing on Echo router.
- Publishing to `messages.outbound` subject via the NATS JetStream publisher interface.

### Integration Points
- `/api/integrations/chatwoot` under public handlers to receive webhook events.
- `/api/admin/workspaces/:id/integrations/chatwoot` under admin settings handlers to manage configuration.

</code_context>

<deferred>
## Deferred Ideas

- None — discussion stayed within phase scope.

</deferred>

---

*Phase: 21-chatwoot-integration*
*Context gathered: 2026-07-17*
