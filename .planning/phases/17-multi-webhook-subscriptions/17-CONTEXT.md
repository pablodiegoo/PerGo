# Phase 17: Multi-Webhook Subscriptions - Context

**Gathered:** 2026-07-16
**Status:** Ready for planning

<domain>
## Phase Boundary

This phase implements multi-webhook subscriptions per workspace, enabling wildcards event type filtering, concurrent NATS JetStream task fan-out, independent exponential backoffs, and an operator interface to manage these subscriptions and inspect/retry their individual DLQ items.

</domain>

<decisions>
## Implementation Decisions

### Subscription Schema & Event Filtering
- **Database Schema**: Store subscriptions in a new `webhook_subscriptions` table. Event filters are stored as a PostgreSQL native text array (`text[]`).
- **Wildcard Event Matching**: Use in-memory wildcard globbing via Go's `path.Match` (converting event names to glob-safe format if necessary) to evaluate matching subscriptions on incoming events (e.g. `message.*` matches `message.received`).
- **Credential Storage**: Webhook subscription secrets are encrypted at rest using the existing `CredentialProvider` (AES-256-GCM envelope encryption).
- **Supported Event Types**: Standard event types are `message.received`, `message.sent`, `message.failed`, and `connection.status` (plus the global wildcard `*`).

### NATS JetStream Fan-out & Concurrency
- **Concurrent Fan-Out**: When an event occurs, the `WebhookWorker` or event publisher queries all matching subscriptions in the database and publishes separate delivery task messages to a new NATS JetStream stream `WEBHOOK_DELIVERIES`. This decouples delivery and ensures that one failing or slow endpoint doesn't block or slow down others.
- **NATS Subject Design**: Use the NATS subject pattern `webhooks.deliveries.<workspace_id>.<subscription_id>` for delivery task routing.
- **Worker Concurrency**: Implement standard NATS pull consumer queue groups with concurrent worker goroutines.

### DLQ, Retries & Admin UI Dashboard
- **DLQ Database Mapping**: Link `webhook_dlqs` to the specific subscription via a foreign key `subscription_id UUID REFERENCES webhook_subscriptions(id) ON DELETE CASCADE`.
- **DLQ Manual Retries**: Retrying a DLQ item republishes a delivery event back to the NATS `webhooks.deliveries.*` subject, allowing it to go through the standard retry pipeline if it fails again.
- **Admin UI Integration**: Mount the webhook subscription management dashboard as a nested section under Workspace Settings (`/admin/workspaces/:id/settings/webhooks`) in the settings accordion.
- **Simulating Webhooks**: Provide a "Test Webhook" simulation tool in the UI that publishes a mock event for a specific subscription to test connectivity and signature validation.

### the agent's Discretion
- The exact layout of the HTML form and modal components for editing/creating subscriptions.
- The precise configuration constants for NATS JetStream streams (e.g., max age, retention limits).

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- [DefaultDispatcher](file:///home/pablo/Coding/OmniGo/internal/webhook/dispatcher.go): The core webhook delivery logic, client timeout, and signature verification. Needs refactoring to handle multiple subscriptions.
- [WebhookWorker](file:///home/pablo/Coding/OmniGo/internal/platform/queue/webhook_worker.go): The NATS consumer loop. Needs to be split/extended to handle `webhooks.deliveries.*` stream.
- [JetStreamPublisher](file:///home/pablo/Coding/OmniGo/internal/platform/queue/jetstream.go): Publisher wrapper for NATS JetStream.
- [CredentialProvider](file:///home/pablo/Coding/OmniGo/internal/repository/credential_provider.go): Reusable AES-256-GCM envelope encryption/decryption.

### Established Patterns
- Pure SQL raw parameterized queries in repository files.
- Echo handlers returning compiled `templ` layouts and pages.
- Structured logging with trace context (`log/slog` + `"trace_id"`).

### Integration Points
- `internal/api/handler/admin/webhook_dlq.go`: Handles webhook configs and DLQ. Will be replaced/refactored for subscriptions.
- `templates/pages/webhooks.templ`: Webhooks settings template. Needs update to support multiple subscriptions (list, create, edit forms, simulation modal).
- NATS Stream definitions in `internal/platform/queue/jetstream.go`.

</code_context>

<specifics>
## Specific Ideas

- The webhook signature header is computed as `t=<timestamp>,v1=<signature>` using HMAC-SHA256 of `timestamp . payload` with the subscription secret.

</specifics>

<deferred>
## Deferred Ideas

- None — discussion stayed within phase scope.

</deferred>
