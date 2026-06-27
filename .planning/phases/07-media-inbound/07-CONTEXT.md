# Phase 7: Media & Inbound - Context

**Gathered:** 2026-06-27
**Status:** Ready for planning
**Mode:** Auto-generated (smart discuss — autonomous mode, recommended defaults applied)

<domain>
## Phase Boundary

`POST /messages` accepts a unified media field (image, document, audio, video) that OmniGo downloads, stores in an S3-compatible object store, and delivers through per-channel adapter paths; inbound messages from all three providers (WhatsApp Web, WABA, Telegram) are ingested with full content extraction (text, media, location, contacts), forwarded to consumer webhooks via durable NATS delivery, and audit-logged with Trace-ID correlation.

</domain>

<decisions>
## Implementation Decisions

### S3 Storage & Media Proxy Implementation
- **S-01:** S3 Client: Use standard AWS SDK v2 (`github.com/aws/aws-sdk-go-v2/service/s3`).
- **S-02:** S3 storage key structure: `{workspace_id}/{content_hash}.{ext}`.
- **S-03:** MIME type detection: Standard `http.DetectContentType` on first 512 bytes of downloaded bytes, fallback to `Content-Type` header.
- **S-04:** Media proxy endpoint: Echo route `GET /media/:workspace_id/:hash` streams bytes from S3 with appropriate Content-Type; authorized via API key or session.

### Inbound Ingestion & NATS Routing
- **I-01:** NATS Stream Configuration: Stream `INBOUND`, subjects `inbound.events.*` (where `*` is `workspace_id`).
- **I-02:** Inbound Deduplication Storage: PostgreSQL table `inbound_dedups` with TTL index/cleanup query.
- **I-03:** Webhook Worker Routing: Extend existing `WebhookWorker` to consume from both `WEBHOOKS` and `INBOUND` streams.
- **I-04:** PII Opt-In storage: Add `pii_opt_in` boolean column to `workspaces` table.

### Provider Webhooks & Event Parsers
- **P-01:** whatsmeow event integration: Extend session manager event loop in `internal/session/manager.go` to listen to `*waEvents.Message`, build standard payload, and publish to NATS.
- **P-02:** WABA inbound webhook auth: Meta verification token stored per-workspace in `channel_credentials` (Meta sends `hub.verify_token` to verify).
- **P-03:** Telegram webhook handler reuse: Extend existing `telegram_webhook.go` Echo endpoint to parse full content type and publish to NATS.

### the agent's Discretion
- Database schema details: Use standard migrations (e.g. `009_media_and_inbound.sql`).
- JSON payload schema for inbound webhooks.
- Inbound dedup cleanup interval: periodic check or query-level cleanups.
- MinIO dev integration (docker-compose MinIO container setup).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Project Specifications
- `.planning/phases/07-media-inbound/07-SPEC.md` — Goal, requirements, edge cases, prohibitions
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
- `internal/platform/queue/webhook_worker.go` — Webhook forwarding loop with HMAC and DLQ
- `internal/platform/crypto` — AES-256-GCM encryption helpers
- `internal/repository/recipient_session.go` — Recipient session tracking

### Established Patterns
- Range-partitioned/regular tables with migrations via goose
- Repository structures using `pgx` without ORM
- Echo Handlers and Middleware
- Templ layouts and components
- Context-propagated structured logging with `slog`

### Integration Points
- Add Media fields to `CreateMessageRequest` and `QueueMessage` in `internal/domain/message.go`
- Register S3 client and wire `WebhookWorker` to consume `INBOUND` streams in `cmd/omnigo/main.go`
- Extend whatsmeow handler in `internal/session/manager.go` to parse and publish messages
- Extend Telegram webhook handler in `internal/api/handler/telegram_webhook.go`
- Add WABA webhook endpoint `/webhooks/waba/:workspace_id` in `internal/api/handler/`
- Register media proxy route `GET /media/:workspace_id/:hash`

</code_context>

<specifics>
## Specific Ideas

- WABA templates with media variables will query the stored proxy/S3 URL or direct Meta upload IDs.
- Deduplication should run before enqueuing to NATS to avoid duplicate processing at the broker level.

</specifics>

<deferred>
## Deferred Ideas

- Thumbnail generation for images/videos
- Cross-channel media forwarding
- Media retention/expiry policy
- Resumable/chunked media uploads to OmniGo
- Media transcoding
- Read receipts / delivery receipts as inbound events
- Inbound message threading / conversation grouping
- Group chat / multi-party inbound messages
- PII masking/hashing in webhook payloads (handled by consumer application)

</deferred>

---

*Phase: 07-media-inbound*
*Context gathered: 2026-06-27*
