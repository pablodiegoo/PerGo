# Phase 7: Media & Inbound — Specification

**Created:** 2026-06-27
**Ambiguity score:** 0.11 (gate: ≤ 0.20)
**Requirements:** 8 locked

## Goal

`POST /messages` accepts a unified media field (image, document, audio, video) that PerGo downloads, stores in an S3-compatible object store, and delivers through per-channel adapter paths; inbound messages from all three providers (WhatsApp Web, WABA, Telegram) are ingested with full content extraction (text, media, location, contacts), forwarded to consumer webhooks via durable NATS delivery, and audit-logged with Trace-ID correlation.

## Background

Today, zero media code exists across the entire codebase. All three channel adapters (WhatsApp Web, WABA, Telegram) send text-only or template messages. `CreateMessageRequest` has no media fields. `QueueMessage` has no media fields. `MessagePayload` has no media fields. No S3 client exists. No media storage infrastructure exists.

For inbound, partial foundations exist:
- **Telegram webhook handler** (`telegram_webhook.go`) validates `X-Telegram-Bot-Api-Secret-Token` and upserts `recipient_sessions` for 24h window tracking, but extracts zero message content and forwards nothing to consumers.
- **WhatsApp Web event handler** (`session/manager.go:156-174`) receives `*waEvents.Message` and upserts `recipient_sessions` with sender JID, but extracts zero message content and forwards nothing.
- **WABA inbound webhook**: does not exist at all.
- **Webhook delivery infrastructure** (`WebhookWorker`, HMAC signing, DLQ) exists from Phase 6 but handles only outbound status events — no inbound message forwarding path.
- **Recipient session tracking** and **24h window checker** (`session/window.go`) work correctly.
- **Audit logs table** exists (migration 001) but has no inbound message logging code.

## Requirements

1. **Unified media field**: `POST /messages` accepts an optional `media` object with `media_url` (string, required), `media_type` (enum: `image|document|audio|video`, required), `filename` (string, optional — required for `document`), and `caption` (string, optional).
   - Current: `CreateMessageRequest` has no media fields; only `Body` carries message content
   - Target: `CreateMessageRequest`, `QueueMessage`, and `MessagePayload` all carry a `Media` struct; validation requires either `Body` or `Media` (or both); `media_type` must be a valid enum value; `media_url` must be a valid HTTP/HTTPS URL
   - Acceptance: `POST /messages` with `media` object and valid fields returns 202; `POST /messages` with neither `body` nor `media` returns 422; `POST /messages` with invalid `media_type` returns 422

2. **Per-channel media delivery**: Each adapter delivers media using the channel's native API.
   - Current: WhatsApp Web sends `waE2E.Message{Conversation: &body}` only; WABA sends `type: "text"` or `type: "template"` only; Telegram calls `sendMessage` only
   - Target: WhatsApp Web sends `waE2E.ImageMessage`/`DocumentMessage`/`AudioMessage`/`VideoMessage` with downloaded bytes; WABA sends `type: "image"|"document"|"audio"|"video"` with media URL; Telegram calls `sendPhoto`/`sendDocument`/`sendAudio`/`sendVideo` with multipart upload
   - Acceptance: A message with `media_type: "image"` and a valid image URL is delivered successfully through each of the three channels; delivery shows channel-native media rendering (not a text link)

3. **S3-compatible media storage**: PerGo downloads media from `media_url`, stores it in an S3-compatible object store (e.g., MinIO), and serves it via proxy URL.
   - Current: No S3 client, no media storage, no download pipeline
   - Target: Media downloaded once from source URL; stored as `{workspace_id}/{content_hash}.{ext}` in the configured S3 bucket; 25MB max file size enforced (inclusive: ≤ 25,000,000 bytes accepted, > 25,000,000 bytes rejected with HTTP 422); content-type validated against declared `media_type`
   - Acceptance: Media file at exactly 25MB is accepted; media file at 25MB+1 byte is rejected with HTTP 422 and error message `"media_size_exceeded"`; media stored in S3 with correct key format; if source URL returns 404/timeout/DNS failure, the message is rejected with HTTP 422 before entering the NATS queue

4. **Inbound message ingestion**: All three providers' inbound messages are captured with full content extraction.
   - Current: Telegram webhook and WhatsApp Web event handler track `recipient_sessions` only; zero content extraction; no WABA inbound handler
   - Target: WhatsApp Web `*waEvents.Message` handler extracts text, image, document, audio, video, location, and contacts; Telegram webhook handler extracts text, photo, document, audio, video, location, and contact; new WABA inbound webhook handler validates Meta verification challenge, parses inbound message payloads, extracts text/media/location/contacts; WABA status objects (sent/delivered/read) are ignored and do NOT generate inbound events; inbound messages with empty text but non-empty media/location/contacts are forwarded normally; truly empty events (no text, no media, no location, no contact) are silently dropped
   - Acceptance: Inbound text message from each provider creates an inbound event; inbound image message from each provider creates an inbound event with media metadata; WABA verification challenge (`GET` with `hub.verify_token`) returns the `hub.challenge` value; WABA status-only payload does NOT create an inbound event

5. **Inbound webhook forwarding**: Inbound events forwarded to consumer applications via durable retried webhooks.
   - Current: `WebhookWorker` delivers outbound status events only; no inbound forwarding path
   - Target: New NATS JetStream `INBOUND` stream with `inbound.events.{workspace_id}` subjects; durable pull consumer follows existing `WebhookWorker` pattern (MaxDeliver retry, DLQ on terminal failure, HMAC signing identical to outbound); inbound events published with Trace-ID in NATS headers
   - Acceptance: Inbound message triggers consumer webhook delivery within 5s; failed webhook delivery retries per MaxDeliver policy; terminal failure moves to DLQ; webhook payload is HMAC-signed with the workspace's webhook secret

6. **Inbound audit logging**: Every inbound event is audit-logged with Trace-ID correlation.
   - Current: `audit_logs` table exists but has no inbound logging code
   - Target: Each inbound event gets a unique Trace-ID; audit log entry created with `workspace_id`, `channel`, `direction: "inbound"`, `provider_message_id`, `trace_id`, `timestamp`, and full message content; Trace-ID propagated through NATS headers to webhook delivery
   - Acceptance: After receiving an inbound message, an `audit_logs` row exists with `direction = 'inbound'` and matching `trace_id`; the same `trace_id` appears in the consumer webhook delivery log

7. **Inbound deduplication**: Duplicate inbound messages do not produce duplicate consumer webhooks.
   - Current: No dedup mechanism; Telegram retries and whatsmeow reconnection could fire duplicates
   - Target: Dedup key is the provider-specific message ID (Telegram `update_id`, WhatsApp `message_id`, WABA `message_id`); if the same dedup key is seen again within a configurable window, the inbound event is silently dropped before publishing to NATS
   - Acceptance: Replaying a Telegram webhook with the same `update_id` does NOT trigger a second consumer webhook; replaying a whatsmeow event with the same `message_id` does NOT trigger a second consumer webhook

8. **Media captions**: Text alongside media is delivered as a caption.
   - Current: No caption support; messages carry either `Body` (text) or nothing
   - Target: `caption` field in the `Media` struct is propagated to each adapter; WhatsApp Web uses `waE2E.ImageMessage.Caption`; WABA includes caption in the media payload; Telegram includes `caption` parameter in `sendPhoto`/`sendDocument`/`sendAudio`/`sendVideo`; if both `Body` and `Media.Caption` are provided, `Media.Caption` takes precedence for media messages
   - Acceptance: Message with `media_type: "image"` and `caption: "Hello"` shows "Hello" as caption in the channel; message with `media` but no `caption` delivers media without caption text

## Boundaries

**In scope:**
- Unified `media` field on `POST /messages` (image, document, audio, video)
- S3-compatible object store integration (download, store, proxy serve)
- Per-channel media delivery (WhatsApp Web bytes, WABA URL, Telegram multipart)
- Media captions per-channel
- Full inbound content extraction from all three providers (text, media, location, contacts)
- WABA inbound webhook handler with Meta verification challenge
- Inbound event forwarding via durable NATS + webhook delivery (reusing WebhookWorker pattern)
- Inbound audit logging with Trace-ID correlation
- Inbound message deduplication by provider-specific message ID
- 25MB media size limit enforcement
- Media download failure handling (reject before NATS queue)

**Out of scope:**
- Thumbnail generation for images/videos — deferred; adds complexity without core value
- Cross-channel media forwarding (receive on WhatsApp, forward to Telegram) — deferred; requires message routing logic not yet designed
- Media retention/expiry policy (auto-delete after N days) — deferred; needs lifecycle management design
- Resumable/chunked media uploads from API caller to PerGo — deferred; standard URL-based media is sufficient for MVP
- Media transcoding (e.g., HEIC → JPEG) — deferred; channels handle format requirements
- Read receipts / delivery receipts as inbound events — deferred; status events are a separate concern
- Inbound message threading / conversation grouping — deferred; no conversation model exists yet
- Group chat / multi-party inbound messages — deferred; 1:1 messaging only for MVP
- PII masking/hashing in webhook payloads — consumer application's responsibility (see Prohibitions)

## Constraints

- **Media size limit**: 25MB (25,000,000 bytes) — matches Telegram's bot API limit as the lowest common denominator across all three channels
- **S3 compatibility**: Must work with any S3-compatible object store (AWS S3, MinIO, DigitalOcean Spaces) — configured via environment variables (`S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_REGION`)
- **Media download timeout**: Source URL download must complete within 30 seconds; timeout → HTTP 422 rejection
- **Staggered dispatch**: WhatsApp Web media messages must respect the existing 1-3s staggered dispatch from `golang.org/x/time/rate` limiter
- **Inbound dedup window**: Configurable, default 5 minutes — matches typical webhook retry window
- **No new heavy dependencies**: Use the Go AWS SDK v2 (`aws-sdk-go-v2`) for S3, which is the standard Go library for S3-compatible stores

## Acceptance Criteria

- [ ] `POST /messages` with `media` object (valid URL, type, optional caption) returns 202 Accepted
- [ ] `POST /messages` with neither `body` nor `media` returns 422
- [ ] `POST /messages` with invalid `media_type` (not image/document/audio/video) returns 422
- [ ] Media file ≤ 25MB is accepted; media file > 25MB is rejected with 422 and `media_size_exceeded` error
- [ ] Media download failure (404, timeout, DNS) rejects the message with 422 before NATS enqueue
- [ ] Media stored in S3 at `{workspace_id}/{content_hash}.{ext}`
- [ ] WhatsApp Web delivers media as native `waE2E.ImageMessage` (not a text link)
- [ ] WABA delivers media with `type: "image"` payload (not text)
- [ ] Telegram delivers media via `sendPhoto` API call (not `sendMessage` with URL)
- [ ] Caption text appears in channel-native caption rendering
- [ ] Inbound text message from each of 3 providers creates an inbound event
- [ ] Inbound media message from each provider includes media metadata in the event
- [ ] WABA verification challenge (`GET` with `hub.verify_token`) returns `hub.challenge`
- [ ] WABA status-only payload does NOT create an inbound event
- [ ] Empty inbound events (no text, no media, no location, no contact) are silently dropped
- [ ] Inbound events trigger consumer webhook delivery within 5 seconds
- [ ] Failed inbound webhook delivery retries per MaxDeliver policy
- [ ] Terminal inbound webhook failure moves to DLQ
- [ ] Inbound webhook payload is HMAC-signed
- [ ] Inbound message creates an `audit_logs` row with `direction = 'inbound'` and `trace_id`
- [ ] Same `trace_id` appears in audit log and consumer webhook delivery
- [ ] Replaying a Telegram webhook with same `update_id` does NOT trigger duplicate consumer webhook
- [ ] Replaying a whatsmeow event with same `message_id` does NOT trigger duplicate consumer webhook
- [ ] MUST NOT forward inbound messages containing personal data (phone numbers, contacts, location) without workspace PII opt-in flag enabled

## Edge Coverage

**Coverage:** 18/21 applicable edges resolved · 0 unresolved

| Category | Requirement | Status | Resolution / Reason |
|----------|-------------|--------|---------------------|
| idempotency | R1 | 🧪 backstop | Test that duplicate POST /messages with same media_url doesn't create duplicate S3 objects |
| concurrency | R1 | 🧪 backstop | Test concurrent media downloads for the same URL don't corrupt the stored object |
| concurrency | R2 | 🧪 backstop | Test concurrent adapter media deliveries for the same message don't interfere |
| boundary | R3 | ✅ covered | AC: media ≤ 25MB accepted, > 25MB rejected with 422 `media_size_exceeded` |
| precision | R3 | ✅ covered | AC: boundary is inclusive (≤ 25,000,000 bytes); 25MB+1 byte → rejected |
| idempotency | R3 | 🧪 backstop | Test re-uploading same content produces same S3 key (content-addressed hash) |
| concurrency | R3 | ✅ covered | AC: concurrent downloads of same URL — at-least-once S3 write; content-hash key ensures idempotent storage |
| adjacency | R4 | ✅ covered | AC: WABA status objects (sent/delivered/read) ignored; only message objects generate events |
| empty | R4 | ✅ covered | AC: empty text with media/location/contacts forwarded normally; truly empty events silently dropped |
| encoding | R4 | ⛔ dismissed | Encoding details are implementation-level, not spec-level; all channels use UTF-8 natively |
| ordering | R4 | ⛔ dismissed | Ordering not guaranteed for inbound events; consumers handle out-of-order delivery |
| unclassified | R5 | ✅ covered | AC: follows existing WebhookWorker pattern — durable pull consumer, MaxDeliver retry, DLQ, HMAC signing |
| adjacency | R6 | ✅ covered | AC: each inbound event gets unique trace_id; same trace_id in audit log and webhook delivery |
| empty | R6 | ✅ covered | AC: audit log created for every non-empty inbound event; empty events dropped before logging |
| ordering | R6 | ⛔ dismissed | Ordering not guaranteed for inbound events; consumers handle out-of-order delivery |
| empty | R7 | ✅ covered | AC: dedup key is provider-specific message ID; empty/missing ID → event processed (no dedup possible) |
| encoding | R7 | ✅ covered | AC: dedup key compared as raw bytes — provider message IDs are ASCII/numeric |
| idempotency | R7 | ✅ covered | AC: same provider message ID within dedup window → silently dropped |
| concurrency | R7 | ✅ covered | AC: concurrent arrival of same dedup key → at most one event published |
| empty | R8 | ✅ covered | AC: media without caption delivers media with no caption text |
| encoding | R8 | ⛔ dismissed | Encoding details are implementation-level; channels handle UTF-8 natively |

## Prohibitions (must-NOT)

**Coverage:** 1/1 applicable prohibitions resolved · 0 unresolved

| Prohibition (must-NOT statement) | Requirement | Status | Verification / Reason |
|----------------------------------|-------------|--------|------------------------|
| MUST NOT forward inbound messages containing personal data (phone numbers, contacts, location) to consumer webhooks without the workspace having explicitly opted in to receiving PII | R4, R5 | resolved | verification: judgment — requires manual review of webhook payload format and workspace PII opt-in flag |
| ~~MUST NOT include raw phone numbers or email addresses in default inbound webhook payload~~ | R5 | dismissed | Consumer application is responsible for handling PII, not the messaging gateway; PerGo is a self-hosted infrastructure component, not a SaaS |
| ~~MUST NOT store full message content in audit_logs table~~ | R6 | dismissed | Audit logs are internal and should contain whatever's needed for debugging; PII in audit logs is acceptable for a self-hosted product where the operator controls infrastructure |

**Canon-referral breadcrumbs:**
- SSRF via `media_url` → canon OWASP; owned by /gsd-secure-phase + input validation; not minted here
- S3 bucket exposure → canon infrastructure security; not minted here
- WABA webhook token hardcoded → canon secret management; not minted here
- XSS via caption in admin UI → canon OWASP; not minted here

## Ambiguity Report

| Dimension          | Score | Min  | Status | Notes                              |
|--------------------|-------|------|--------|------------------------------------|
| Goal Clarity       | 0.90  | 0.75 | ✓      | Media types, storage policy, inbound scope all locked |
| Boundary Clarity   | 0.85  | 0.70 | ✓      | Explicit in/out scope lists with reasoning |
| Constraint Clarity | 0.70  | 0.65 | ✓      | Size limit, S3 config, download timeout, dedup window specified |
| Acceptance Criteria| 0.70  | 0.70 | ✓      | 24 pass/fail criteria |
| **Ambiguity**      | **0.11** | ≤0.20 | ✓ | Gate passed after 3 interview rounds |

## Interview Log

| Round | Perspective | Question summary | Decision locked |
|-------|-------------|-----------------|-----------------|
| 1 | Researcher | Which media types for MVP? | Image, document, audio, video (all four) |
| 1 | Researcher | URL pass-through vs blob storage? | Full blob storage in S3-compatible object store |
| 2 | Researcher + Simplifier | Where to store media blobs? | S3-compatible object store (MinIO for self-hosted) |
| 2 | Simplifier | Inbound content extraction scope? | Full extraction: text, media, location, contacts from day one |
| 2 | Simplifier | Maximum media file size? | 25MB (Telegram's bot API limit — lowest common denominator) |
| 3 | Boundary Keeper | Adjacent features in scope? | Captions + dedup in scope; thumbnails, cross-channel forwarding, retention deferred |
| 3 | Boundary Keeper | Explicit out of scope? | Resumable uploads, transcoding, read receipts, threading, group chat all excluded |

---

*Phase: 07-media-inbound*
*Spec created: 2026-06-27*
*Next step: /gsd-discuss-phase 7 — implementation decisions (how to build what's specified above)*
