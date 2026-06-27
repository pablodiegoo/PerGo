# Phase 6: Webhook Delivery & DLQ - Context

**Gathered:** 2026-06-26
**Status:** Ready for planning

<domain>
## Phase Boundary

Durable, HMAC-signed status event webhook delivery pipeline with exponential backoff retries, a dedicated PostgreSQL dead-letter queue (DLQ) for terminal/exhausted failures, and an operator dashboard for inspecting and re-triggering failed deliveries.

</domain>

<decisions>
## Implementation Decisions

### Webhook HMAC Signing Scheme
- Cryptographic algorithm: HMAC-SHA256.
- Format and Header: Hex-encoded signature sent in a custom header `X-OmniGo-Signature`.
- Replay Prevention: Include a timestamp `t=timestamp` in both the header and the signed payload; client validates within a 5-minute threshold.
- Secret Storage: AES-256-GCM encrypted in the PostgreSQL database per workspace using the workspace encryption key, with in-memory caching.

### Webhook Event Payload & Schema
- Trigger Events: All status transitions (`queued`, `sending`, `sent`, `delivered`, `read`, `failed`).
- Payload Structure: Standardized event envelope containing `event` (string), `trace_id` (string), `message_id` (string), `channel` (string), `timestamp` (string), `workspace_id` (string), and optional `error` (string) for failures.
- Webhook URL Scope: Per-workspace URL stored in the database.
- Batching vs Individual: Individual webhook requests (one HTTP call per event) to minimize complexity.

### Retry Policy & Backpressure
- Backoff Implementation: `NakWithDelay` calculated dynamically by the worker based on JetStream redelivery count.
- Max Attempts: 10 attempts before moving to DLQ (per WHOOK-05 requirement).
- Status Classification: Terminal statuses (400, 401, 403, 404) trigger immediate move to DLQ without retry. Transient statuses (429, 5xx, or network timeouts) trigger exponential backoff retry.
- HTTP Request Timeout: 10 seconds timeout.

### DLQ & Admin UI
- DLQ Storage: Dedicated `webhook_dlq` table in PostgreSQL.
- DLQ Fields: `id`, `workspace_id`, `trace_id`, `message_id`, `event_type`, `payload` (JSON), `webhook_url`, `last_attempt_at`, `failure_reason`, and `attempts`.
- Admin Actions: View details, delete (dismiss), and manual retry (re-enqueue).
- Operator Alerts: Badge count in the sidebar next to "Webhooks" and a dedicated Webhooks / DLQ management view.

### the agent's Discretion
All other micro-design details are at the agent's discretion, keeping with the codebase's existing structures.

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/platform/queue` - NATS connection and publisher/consumer utilities.
- `internal/platform/postgres` - Repository patterns and pgx connection wrappers.
- `internal/platform/crypto` - AES-256-GCM encryption helpers.
- `internal/api/middleware` - Auth and workspace context extraction middleware.

### Established Patterns
- Range-partitioned/regular tables with migrations via goose.
- Repository structures using `pgx` without ORM.
- Graceful shutdown handles JetStream consumer cleanup.
- Context-propagated structured logging with `slog`.

### Integration Points
- `internal/platform/queue` - Add webhook publishing and worker/consumer setup.
- `cmd/omnigo/main.go` - Wire the webhook workers, publishers, and DLQ handlers.
- `templates/` - Update UI navigation sidebar and build templates for DLQ management using Templ.

</code_context>

<specifics>
## Specific Ideas

No specific requirements - open to standard approaches.

</specifics>

<deferred>
## Deferred Ideas

None - discussion stayed within phase scope.

</deferred>
