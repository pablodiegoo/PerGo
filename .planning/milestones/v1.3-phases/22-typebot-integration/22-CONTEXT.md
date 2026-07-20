# Phase 22: Typebot Integration - Context

**Gathered:** 2026-07-17
**Status:** Ready for planning

<domain>
## Phase Boundary

Enable bot automation by implementing the Typebot connection settings panel, the built-in native integration receiver webhook endpoint (`POST /api/integrations/typebot`), and forwarding inbound customer messages to the Typebot execution API.

</domain>

<decisions>
## Implementation Decisions

### Connection Configuration & Credentials Storage
- **D-01:** Reuse the unified `integrations` database table and credentials encryption envelope pattern (AES-256-GCM config block).
- **D-02:** Store multiple bot configurations inside the encrypted JSON `config` envelope in a single `typebot` integration row (allowing the operator to register multiple bots under one workspace).

### Multi-Bot and Multi-Channel Mapping
- **D-03:** Map each Typebot bot configuration specifically to a connection channel (using connection ID) with optional trigger keywords (e.g. "sales", "support") and a fallback default bot flag.
- **D-04:** If a customer sends a trigger keyword while *already* in an active bot session, ignore the trigger keyword and route the message to the active session until it is finished/expired.

### Session Management
- **D-05:** Track active sessions in a dedicated `typebot_sessions` database table using a composite primary key `(workspace_id, contact_id, connection_id)`. Include `updated_at` to enforce local inactivity timeouts, and delete mapping to restart sessions on remote 404/expired API responses.

### Webhook & Message Ingestion Flow
- **D-06:** Use a Hybrid Flow where customer replies are enqueued synchronously when returned in the HTTP response of the Typebot execution API (`startChat`/`continueChat`).
- **D-07:** Expose the receiver webhook endpoint `POST /api/integrations/typebot` to allow manual Webhook blocks configured within the bot flow to trigger asynchronous replies. Secure this endpoint using the standard API key query parameter token validation.

### the agent's Discretion
- Database schema details for `typebot_sessions` table.
- Exact local inactivity timeout threshold (e.g. 30 minutes).
- Formatting and parsing mapping of rich bot elements (such as buttons or links) returned by Typebot API into PerGo outbound payloads.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Databases & Repositories
- `internal/platform/postgres/migrations/027_create_integrations_and_chatwoot_mappings.sql` — Defines the `integrations` table and structure.
- `internal/repository/integration.go` — Integrations repository access and configuration model.

### Inbound Ingestion Pipeline
- `internal/inbound/processor.go` §Process — Entry point for inbound message processing where the bot forwarding engine should trigger.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `AuthMiddleware` / API Key validation: Standard query-based token authentication for `/api/integrations/typebot`.
- `IntegrationRepository`: Retrieve/save Typebot integration credentials.
- `ConnectionRepository`: Resolve incoming connection parameters for bot mapping.

### Established Patterns
- Asynchronous sync: Running the syncer/forwarder in a decoupled goroutine post-media upload.
- NATS outbound: Marshalling `domain.QueueMessage` and publishing to JetStream subject `messages.outbound` for outbound bot messages.

### Integration Points
- `/api/integrations/typebot` — Webhook POST route.
- `/api/admin/workspaces/:id/integrations/typebot` — Admin settings path.
- `InboundProcessor.Process` — Hooking the Typebot forwarder to intercept inbound customer messages.

</code_context>

<specifics>
## Specific Ideas

- Inbound attachments (images, audio) can be forwarded to Typebot by appending their resolved S3 media proxy URLs in the text of the message sent to Typebot.

</specifics>

<deferred>
## Deferred Ideas

- None — discussion stayed within phase scope.

</deferred>

---

*Phase: 22-typebot-integration*
*Context gathered: 2026-07-17*
