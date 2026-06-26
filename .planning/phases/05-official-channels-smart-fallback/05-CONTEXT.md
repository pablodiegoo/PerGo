# Phase 5: Official Channels & Smart Fallback - Context

**Gathered:** 2026-06-26
**Status:** Ready for planning
**Mode:** Auto-generated (smart discuss — autonomous mode, recommended defaults applied)

<domain>
## Phase Boundary

Official Channels (WABA and Telegram Bot) are added to the platform as stateless REST channel adapters implementing the `Dispatcher` interface. An ordered `fallback_channels` array allows fallback routing on failure, with terminal error classification to bypass retries and fallback-aware deduplication. The 24-hour customer service window is tracked to restrict WABA outbound messages to templates outside the window, triggering fallback if the window is expired. Template management CRUD (create, list, track Meta approval status) is added to the admin panel.

</domain>

<decisions>
## Implementation Decisions

### WABA REST Integration & Credentials Storage
- **D-01:** WABA adapter calls Meta Graph API `/v18.0/{phone_number_id}/messages`.
- **D-02:** Workspace credentials (API token, Phone Number ID, WABA Account ID) will be stored in a new `channel_credentials` table (encrypted using existing AES-256-GCM infrastructure).

### WABA Template Management
- **D-03:** Templates will be persisted in a `waba_templates` table (fields: `id`, `workspace_id`, `name`, `language`, `status` [pending/approved/rejected], `category`, `components` [JSONB]). The admin panel provides a simple UI to list templates, create templates (triggering Meta Graph API submission), and poll Meta for approval status.
- **D-04:** Template-based message sending is enabled by checking message payload fields: `template_name`, `language`, `components`.

### 24-Hour Customer Service Window Tracking
- **D-05:** Track the last inbound message timestamp per recipient. For WABA, if the last inbound message is > 24 hours ago, reject non-template messages with `ErrTerminal` (triggering fallback) or rewrite to a configured template. We will save inbound timestamps in a new `recipient_sessions` table (`workspace_id`, `recipient_phone`, `channel`, `last_inbound_at`).

### Telegram Bot Integration
- **D-06:** Telegram adapter calls Bot API `sendMessage`. Credentials (Bot Token) stored in `channel_credentials` table. We register webhooks using `setWebhook` with a random `secret_token` stored in config, and verified on incoming webhooks at `/webhooks/telegram/:workspace_id` to route inbound messages.

### Fallback Mechanism & Ordered Fallback
- **D-07:** The message payload includes an optional `fallback_channels` array (e.g. `["telegram"]`). The dispatcher will process the primary channel, and on failure:
  - If the error is terminal (classified via `errors.Is(err, ErrTerminal)` or `IsTerminalError()`), it immediately triggers fallback to the next channel in `fallback_channels`.
  - If the error is retriable (network failure), NATS JetStream handles retry.
- **D-08:** Dedup: A message status/dispatch log records which channel successfully sent the message to prevent duplicate delivery across different fallback channels during worker redelivery.

### the agent's Discretion
- Database schema details: Use standard migrations.
- Inbound webhook authentication/secret: Store a generated UUID per workspace for webhook validation.
- Template payload format: Support text-only templates first (headers/body/footer with variables), media templates deferred.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Project Specifications
- `.planning/PROJECT.md` — Core value, constraints, stack decisions
- `.planning/REQUIREMENTS.md` — Requirement traceability and status
- `.planning/STATE.md` — Project state tracker

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/channel/dispatcher.go` — `Dispatcher` and `MessagePayload` definitions
- `internal/channel/registry.go` — Channel registry for resolving adapters
- `internal/platform/queue/worker.go` — Worker dispatch loop and retry logic
- `internal/repository/device.go` — Pattern for DB-based repositories in postgres

### Established Patterns
- Echo Handlers and Middleware
- Templ layouts and components (sidebar navigation, pages)
- Graceful shutdown orchestration in `main.go`

### Integration Points
- Add WABA and Telegram adapters to the `channel.Registry` in `main.go`
- Webhook routes registered under `/webhooks/telegram/:workspace_id`
- Admin routes for WABA templates registered under `/admin/workspaces/:workspace_id/templates`

</code_context>

<specifics>
## Specific Ideas

- The fallback logic is clean, iterative, and relies on typed errors (`ErrTerminal`) to bypass JetStream retries.
- WABA templates should start simple (text-only) and grow as needed.

</specifics>

<deferred>
## Deferred Ideas

- Media support within WABA templates (deferred to Phase 7)
- Multi-user admin authentication with roles (MVP uses single-operator model)

</deferred>

---

*Phase: 05-official-channels-smart-fallback*
*Context gathered: 2026-06-26*
