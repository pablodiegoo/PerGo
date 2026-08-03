# SPEC-v1.8: PerGo Broadcaster Engine, Contact Tagging & Developer Gateway Extensions

## Problem Statement

Backend developers, system operators, and marketing teams using commercial CPaaS platforms (such as Twilio, Botconversa, Take Blip, 360dialog, and Wati) face severe financial friction, vendor lock-in, and operational limitations:
- **Twilio & 360dialog**: Impose per-message markup ($/msg) or steep monthly fees (€49-99 per WhatsApp number) without providing an out-of-the-box campaign UI or broadcast scheduler for non-technical operators.
- **Botconversa & Wati**: Charge per-number monthly subscriptions (R$ 189-297/month per connected number) while relying on non-official WhatsApp Web infrastructure or restricting omnichannel messaging.
- **PerGo (v1.7 Current State)**: While PerGo v1.7 features a high-performance Go/Echo/NATS gateway, WABA Cloud API, WhatsApp Web (whatsmeow), Telegram, Instagram, Email, Chatwoot, and Typebot integrations, it lacks:
  1. A native Admin UI & API Engine for mass message broadcasting (`Campaigns`), scheduling, and rate-controlled dispatch.
  2. Contact tag management and segment filtering (`contact_tags`).
  3. Outbound webhook security signatures (`X-PerGo-Signature` HMAC) and developer client SDK primitives.

## Solution

Transform PerGo into a full-fledged **Developer CPaaS Gateway + WhatsApp SMB Specialist Platform (v1.8)** by delivering:
1. **Broadcaster Engine**: A resilient, NATS JetStream-backed campaign execution pipeline with Templ/HTMX admin UI and REST APIs for creating, scheduling, pausing, resuming, and tracking mass message dispatches across WABA, WhatsApp Web, Telegram, Instagram, and Email.
2. **Contact Segmentation & Tagging**: Dynamic tag creation, contact-tag association, CSV/JSON bulk contact import/export, and tag-filtered campaign targeting.
3. **Developer API Security & Outbound Webhook Verification**: HMAC-SHA256 request signing (`X-PerGo-Signature`) for outbound webhooks, enhanced OpenAPI documentation, and client SDK contracts.

## User Stories

1. As a system operator, I want to create a new message campaign by selecting a connection slug and a message template or text payload, so that I can reach my contacts in bulk.
2. As a marketing manager, I want to upload a CSV file of contacts with custom variable columns, so that I can personalize broadcast messages at scale.
3. As a backend developer, I want to trigger a campaign programmatically via `POST /api/v1/campaigns`, so that my CRM/ERP can initiate automated broadcasts.
4. As a system operator, I want to set a delay range and maximum messages per minute for a campaign, so that I can comply with WhatsApp Web anti-ban rate limits and WABA tier limits.
5. As a system operator, I want to schedule a campaign to run at a future date and time, so that dispatches occur automatically during optimal business hours.
6. As a system operator, I want to pause a running campaign mid-dispatch, so that I can halt message delivery if an issue is detected.
7. As a system operator, I want to resume a paused campaign, so that pending recipients continue receiving messages without duplicate dispatches.
8. As a marketing manager, I want to view live campaign progress (total, queued, sent, delivered, failed, read) on a Templ/HTMX dashboard, so that I can monitor dispatch delivery in real time.
9. As a system operator, I want failed campaign dispatches to automatically retry or route to configured fallback channels, so that delivery success rate reaches >= 99.5%.
10. As a support agent, I want to attach dynamic tags (e.g., `VIP`, `Lead-Hot`, `Churn-Risk`) to contacts, so that I can categorize and segment my customer base.
11. As a marketing manager, I want to filter contacts by one or more tags when launching a campaign, so that only relevant segments receive targeted broadcasts.
12. As a backend developer, I want outbound webhooks from PerGo to contain an HMAC-SHA256 signature header (`X-PerGo-Signature`), so that my server can verify webhook authenticity.
13. As a backend developer, I want to configure a secret key for outbound webhook signature generation per workspace, so that cryptographic validation is isolated per tenant.
14. As a system operator, I want to export contacts and their tags as a CSV file, so that I can back up or analyze contact segments locally.
15. As a system operator, I want campaign execution logs to record every recipient dispatch state and trace ID in the `audit_logs` table, so that complete auditability and LGPD compliance are guaranteed.

## Implementation Decisions

- **Database Schemas**:
  - `campaigns`: `id`, `workspace_id`, `connection_id`, `name`, `status` (`draft`, `scheduled`, `running`, `paused`, `completed`, `failed`), `payload_type`, `payload_body`, `template_id`, `rate_limit_per_min`, `scheduled_at`, `started_at`, `completed_at`, `total_recipients`, `sent_count`, `delivered_count`, `failed_count`, `created_at`, `updated_at`.
  - `campaign_recipients`: `id`, `campaign_id`, `contact_id`, `destination`, `variables` (JSONB), `status` (`pending`, `enqueued`, `sent`, `delivered`, `failed`), `trace_id`, `error_reason`, `processed_at`.
  - `tags`: `id`, `workspace_id`, `name`, `color`, `created_at`.
  - `contact_tags`: `contact_id`, `tag_id`, `created_at` (composite primary key).
- **Architecture & Queuing**:
  - NATS JetStream stream `PERGO_CAMPAIGNS` with subject `campaigns.dispatch.>`.
  - Background `CampaignWorker` goroutine consuming campaign batches, respecting token-bucket rate limiters (`golang.org/x/time/rate`), and calling `OutboundProcessor.EnqueueMessage`.
- **API Contracts**:
  - `POST /api/v1/campaigns` — Create campaign
  - `POST /api/v1/campaigns/{id}/start` — Start campaign
  - `POST /api/v1/campaigns/{id}/pause` — Pause campaign
  - `GET /api/v1/campaigns/{id}` — Campaign progress telemetry
  - `POST /api/v1/tags` & `GET /api/v1/tags` — Tag CRUD
  - `POST /api/v1/contacts/import` — CSV contact import
- **Admin UI**:
  - Server-rendered Templ components (`templates/admin/campaigns.templ`, `templates/admin/tags.templ`) with HTMX polling (`hx-get`, `hx-trigger="every 2s"`) for real-time progress bars.
- **Outbound Webhook Security**:
  - `X-PerGo-Signature`: `sha256=HMAC_SHA256(secret, payload_bytes)` injected into `OutboundWebhookWorker`.

## Testing Decisions

- **Testing Seam**: HTTP REST API + Webhook Delivery Level (highest possible seam).
- **Execution**: Tests run against Echo router (`httptest.NewServer` / `httptest.NewRequest`), executing SQL queries on PostgreSQL pool and publishing/consuming messages through NATS JetStream test instances.
- **Prior Art**: Follows table-driven patterns in `internal/repository/connection_test.go` and `internal/delivery/http/message_handler_test.go`.

## Out of Scope

- Native visual drag-and-drop flow builder inside PerGo (handled by integrated Typebot).
- Full PSTN SIP Voice calling / SMS carrier trunking (handled by external provider adapters if needed).

## Further Notes

- Operates under 100% self-hosted zero-markup paradigm.
- Compatible with Go 1.25+, Echo v5, Templ + HTMX, NATS JetStream 2.10+.
