# PerGo Broadcaster Engine, Contact Tagging & Developer Gateway Extensions

## Problem Statement

Backend developers, system operators, and marketing teams using commercial CPaaS platforms (such as Twilio, Botconversa, Take Blip, 360dialog, and Wati) face severe financial friction, vendor lock-in, and operational limitations:
- **Twilio & 360dialog**: Impose per-message markups or steep monthly platform fees per connected WhatsApp number without providing an out-of-the-box campaign UI or broadcast scheduler for non-technical operators.
- **Botconversa & Wati**: Charge per-number monthly subscriptions while relying on fragile unofficial WhatsApp Web scrapers or restricting omnichannel routing.
- **PerGo (Pre-v1.8 State)**: While PerGo provides a high-performance Go/Echo/NATS core with WABA Cloud API, WhatsApp Web (whatsmeow), Telegram, Instagram, Email, Chatwoot, and Typebot integrations, it lacked:
  1. A native Admin UI & API Engine for mass message broadcasting (`Campaigns`), scheduling, and rate-controlled dispatch.
  2. Dynamic contact segment filtering via Tags (`contact_tags`), contact custom `attributes`, and campaign-specific CSV variable overriding.
  3. Outbound webhook security signatures (`X-PerGo-Signature` HMAC-SHA256) with replay protection and workspace-scoped secret management.
  4. Comprehensive recipient-level audit logging in `audit_logs` for LGPD/GDPR compliance and delivery dispute verification.

## Solution

Transform PerGo into a full-fledged **Developer CPaaS Gateway + Omnichannel Campaign Broadcaster (v1.8)** by delivering:
1. **Broadcaster Engine**: A resilient, NATS JetStream-backed campaign execution pipeline with a Templ/HTMX admin UI and REST APIs for creating, scheduling, pausing, resuming, and tracking mass message dispatches across WABA, WhatsApp Web, Telegram, Instagram, and Email.
2. **Contact Segmentation, Tagging & Custom Attributes**: Dynamic tag creation, contact-tag association, JSONB `attributes` storage on contacts, CSV/JSON bulk contact import/export, and execution-time tag-filtered campaign targeting with CSV variable merging.
3. **Developer API Security & Outbound Webhook Verification**: HMAC-SHA256 request signing (`X-PerGo-Signature: t=...,v1=...`) with timestamp replay mitigation, per-workspace secret management, and fail-fast circuit breaking.
4. **Compliance & Traceability**: Structured dispatch audit logging (`campaign.dispatch.*`) recorded in partitioned PostgreSQL storage with trace ID correlation and application log PII redaction.

## User Stories

1. As a system operator, I want to create a new message campaign by selecting a connection slug and entering a message template or text payload, so that I can reach my contacts in bulk.
2. As a marketing manager, I want to upload a CSV file of contacts with custom variable columns, so that I can personalize broadcast messages at scale.
3. As a backend developer, I want to trigger a campaign programmatically via `POST /api/v1/campaigns`, so that my CRM/ERP can initiate automated broadcasts.
4. As a backend developer, I want `POST /api/v1/campaigns` and form creation to return HTTP 422 if neither tags nor CSV recipients are provided, so that unexecutable draft campaigns are rejected at ingestion.
5. As a system operator, I want to set a delay range and maximum messages per minute for a campaign, so that dispatches comply with WhatsApp Web anti-ban rate limits and WABA tier limits.
6. As a system operator, I want to schedule a campaign to run at a future date and time, so that dispatches occur automatically during optimal business hours.
7. As a system operator, I want to pause a running campaign mid-dispatch, so that the worker halts enqueuing new batches without corrupting in-flight messages.
8. As a system operator, I want to resume a paused campaign, so that pending recipients continue receiving messages without duplicate dispatches.
9. As a marketing manager, I want to view live campaign progress (total, queued, sent, delivered, failed, skipped) on a Templ/HTMX dashboard with real-time polling, so that I can monitor dispatch delivery live.
10. As a system operator, I want failed campaign dispatches to automatically retry or route to configured fallback channels, so that delivery success reaches >= 99.5%.
11. As a support agent, I want to attach dynamic tags (e.g., `VIP`, `Lead-Hot`, `Churn-Risk`) to contacts, so that I can categorize and segment my customer base.
12. As a marketing manager, I want to select multiple tags via a visual colored pill checkbox grid when launching a campaign, so that contacts in any selected tag receive targeted broadcasts (Union / OR).
13. As a system operator, I want tag-based campaigns to dynamically resolve matching contact recipients at execution time, so that new contacts added after campaign creation are included.
14. As a marketing manager, I want custom attributes stored on Contact entities to populate template variables for tag-filtered campaigns, so that database contacts receive personalized messages.
15. As a marketing manager, I want CSV row variables to merge over stored contact attributes during conflict resolution, so that one-off campaign overrides complement permanent CRM attributes.
16. As a compliance officer, I want contacts lacking a valid channel identity during campaign resolution to be recorded as `skipped` in the database and audit log, so that skipped contacts are auditable and excluded from NATS queues.
17. As a system operator, I want a campaign with an empty evaluated segment (0 matching contacts) to complete normally with `total_recipients: 0` and emit `campaign.dispatch.completed_empty`, so that empty dynamic segments are treated as valid states rather than failures.
18. As a backend developer, I want outbound webhooks from PerGo to contain an `X-PerGo-Signature` header with format `t=<unix_timestamp>,v1=<hex_digest>`, so that my server can verify webhook authenticity and reject payloads older than 5 minutes.
19. As a backend developer, I want to configure and rotate a secret key for outbound webhook signature generation per workspace, so that cryptographic validation is isolated per tenant and can be rotated immediately.
20. As a system operator, I want to export contacts and their tags as a CSV file via a dedicated export endpoint, so that I can analyze segments externally.
21. As a compliance auditor, I want every recipient dispatch state transition to record an event and trace ID in the `audit_logs` table, so that complete auditability and LGPD compliance are guaranteed.
22. As a system operator, I want downstream webhook endpoint circuit breakers to fail-fast with `ErrCircuitOpen` for concurrent requests while in `halfOpen` probe state, so that worker goroutines do not cascade into timeouts.

## Implementation Decisions

- **Database Schemas**:
  - `contacts`: `id`, `workspace_id`, `name`, `email`, `attributes` (JSONB DEFAULT '{}'), `created_at`, `updated_at`.
  - `contact_identities`: `id`, `contact_id`, `workspace_id`, `channel`, `sender_identity`, `created_at`. Unique per `(workspace_id, channel, sender_identity)`.
  - `tags`: `id`, `workspace_id`, `name`, `color`, `created_at`.
  - `contact_tags`: `contact_id`, `tag_id`, `created_at` (composite primary key).
  - `workspaces`: includes `webhook_url` and `webhook_secret` columns for tenant HMAC-SHA256 signing.
  - `campaigns`: `id`, `workspace_id`, `connection_id`, `name`, `status` (`draft`, `scheduled`, `running`, `paused`, `completed`, `failed`), `payload_type`, `payload_body`, `template_id`, `tag_ids` (UUID array), `rate_limit_per_min`, `scheduled_at`, `started_at`, `completed_at`, `total_recipients`, `sent_count`, `delivered_count`, `failed_count`, `created_at`, `updated_at`.
  - `campaign_recipients`: `id`, `campaign_id`, `contact_id`, `destination`, `variables` (JSONB), `status` (`pending`, `enqueued`, `sent`, `delivered`, `failed`, `skipped`), `trace_id`, `error_reason`, `processed_at`.

- **Broadcaster Engine & Architecture**:
  - NATS JetStream stream `PERGO_CAMPAIGNS` with subject `campaigns.dispatch.>`.
  - Background `CampaignWorker` goroutine consuming campaign batches, respecting token-bucket rate limiters (`golang.org/x/time/rate`), and invoking `OutboundProcessor.EnqueueMessage`.
  - Cooperative worker pause: When a campaign transitions to `paused`, the worker halts before pulling subsequent batches from the database. In-flight messages in NATS complete normally.
  - Tag resolution: `domain.ResolveTagRecipients` executes dynamically at campaign start, selecting contacts matching the Union of `tag_ids`, extracting channel-specific identities, merging contact attributes with CSV variables, and marking non-matching contacts as `skipped`.
  - Empty dispatches: When 0 contacts match, the campaign completes with `total_recipients: 0` and emits a `campaign.dispatch.completed_empty` audit log.

- **Outbound Webhook Security (`X-PerGo-Signature`)**:
  - Header structure: `X-PerGo-Signature: t=<unix_timestamp>,v1=<hex_digest>` where `v1 = HMAC-SHA256(workspace_secret, "<timestamp>.<payload_bytes>")`.
  - 5-minute replay tolerance window.
  - Immediate rotation via `POST /api/v1/workspaces/webhook-secret`.

- **Resilience & Seams**:
  - Circuit Breaker: `internal/platform/breaker` implements explicit `closed`, `open`, `halfOpen` states. In `halfOpen`, exactly 1 probe request is allowed; all concurrent callers fail fast with `ErrCircuitOpen`.
  - Pure domain CSV serialization: `domain.WriteContactsCSV(w io.Writer, contacts []Contact)` encapsulates CSV formatting without database dependencies.
  - Error discipline: Single `%w` wrapping per layer; idempotency and audit errors logged at `slog.Error` with trace IDs.

- **Admin UI**:
  - Server-rendered Templ components (`templates/admin/campaigns.templ`, `templates/admin/tags.templ`) with HTMX polling (`hx-get`, `hx-trigger="every 2s"`).
  - Campaign creation includes a colored pill checkbox grid for tag selection passing `tag_ids[]`.

## Testing Decisions

- **Testing Seam**: HTTP REST API + Webhook Delivery Level (highest possible seam).
- **Behavior-Focused Validation**: Tests execute end-to-end against the Echo HTTP router (`httptest.NewServer` / `httptest.NewRequest`), verifying HTTP responses (status codes, Problem Details JSON, HTMX fragments), database records in PostgreSQL, audit logs, and NATS JetStream messages without asserting on internal private state.
- **Circuit Breaker Seam**: State machine transition tests verifying multi-cycle accumulation and fail-fast half-open behavior in `breaker_test.go`.
- **Prior Art**: Follows patterns established in `internal/domain/campaign_test.go`, `internal/platform/breaker/breaker_test.go`, `internal/repository/connection_test.go`, and `internal/delivery/http/message_handler_test.go`.

## Out of Scope

- Visual drag-and-drop conversational flow builder inside PerGo (delegated to integrated Typebot).
- Dual-secret rotation grace period windows (immediate cutover suffices for self-hosted operators).
- Full PSTN SIP Voice calling / SMS carrier trunking (handled via external provider adapters if needed).

## Further Notes

- All terminology complies strictly with `CONTEXT.md`.
- Architectural choices follow ADR-0001 (Dynamic Recipient Resolution), ADR-0009 (Minimalist & Deep Module API Design), and ADR-0010 (Broadcaster Resolution Resilience).
