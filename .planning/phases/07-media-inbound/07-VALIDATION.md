---
phase: 07
slug: media-inbound
status: complete
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-27
validated: 2026-06-27T11:26:00Z
---

# Phase 07 — Validation Strategy

> Per-phase validation contract for Media & Inbound.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | `go test` (stdlib) |
| **Config file** | none — stdlib testing |
| **Quick run command** | `go test ./internal/domain/... ./internal/channel/... ./internal/platform/storage/... ./internal/api/handler/...` |
| **Full suite command** | `go test ./... -count=1` |
| **Estimated runtime** | ~6 seconds |

---

## Sampling Rate

- **After every task commit:** Run quick command
- **After every plan wave:** Run full suite command
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 6 seconds

---

## Per-Task Verification Map

### Plan 07-01: Media Payload Validation & Storage

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | Test File | Status |
|---------|------|------|-------------|-----------|-------------------|-----------|--------|
| 07-01-01 | 01 | 1 | MEDIA-01 | unit | `go test ./internal/domain/ -run TestValidateMessageMedia` | message_test.go | ✅ green |
| 07-01-01 | 01 | 1 | MEDIA-01 | unit | `go test ./internal/domain/ -run TestValidateMessageEmptyBodyAndMedia` | message_test.go | ✅ green |
| 07-01-02 | 01 | 1 | MEDIA-03 | unit | `go test ./internal/platform/storage/ -run TestDownloadAndValidate` | s3_test.go | ✅ green |
| 07-01-02 | 01 | 1 | MEDIA-03 | subtest | `TestDownloadAndValidate/exactly_at_the_limit_(25MB)` | s3_test.go | ✅ green |
| 07-01-02 | 01 | 1 | MEDIA-03 | subtest | `TestDownloadAndValidate/exceeds_the_limit_(25MB_+_1)` | s3_test.go | ✅ green |
| 07-01-02 | 01 | 1 | MEDIA-03 | subtest | `TestDownloadAndValidate/not_found_returns_error` | s3_test.go | ✅ green |
| 07-01-02 | 01 | 1 | MEDIA-03 | subtest | `TestDownloadAndValidate/context_cancellation_/_timeout` | s3_test.go | ✅ green |
| 07-01-03 | 01 | 1 | MEDIA-03 | integration | `go test ./internal/platform/storage/ -run TestS3ClientUploadAndDownload` | s3_test.go | ✅ green |
| 07-01-03 | 01 | 1 | MEDIA-03 | subtest | `TestS3ClientUploadAndDownload/upload_and_download_roundtrip` | s3_test.go | ✅ green |
| 07-01-03 | 01 | 1 | MEDIA-03 | subtest | `TestS3ClientUploadAndDownload/download_not_found_returns_NoSuchKey` | s3_test.go | ✅ green |

### Plan 07-02: Media Support in Channel Adapters

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | Test File | Status |
|---------|------|------|-------------|-----------|-------------------|-----------|--------|
| 07-02-01 | 02 | 2 | MEDIA-02 | unit | `go test ./internal/channel/whatsapp/ -run TestWhatsAppAdapter_Media` | adapter_media_test.go | ✅ green |
| 07-02-02 | 02 | 2 | MEDIA-02 | integration | `go test ./internal/channel/telegram/ -run TestTelegramDispatch` | telegram_test.go | ✅ green |
| 07-02-03 | 02 | 2 | MEDIA-02 | integration | `go test ./internal/channel/whatsapp/ -run TestWABADispatch` | waba_test.go | ✅ green |
| 07-02-01 | 02 | 2 | MEDIA-01 | integration | `go test ./internal/api/handler/ -run TestMessageHandler_CreateWithMedia` | message_test.go | ✅ green |
| 07-02-01 | 02 | 2 | MEDIA-01 | subtest | `TestMessageHandler_CreateWithMedia/successful_media_download_and_ingest` | message_test.go | ✅ green |
| 07-02-01 | 02 | 2 | MEDIA-01 | subtest | `TestMessageHandler_CreateWithMedia/media_size_exceeded_rejection` | message_test.go | ✅ green |
| 07-02-01 | 02 | 2 | MEDIA-01 | subtest | `TestMessageHandler_CreateWithMedia/media_download_failure_rejection` | message_test.go | ✅ green |

### Plan 07-03: Inbound Message Ingestion & Deduplication

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | Test File | Status |
|---------|------|------|-------------|-----------|-------------------|-----------|--------|
| 07-03-01 | 03 | 3 | INBD-01 | integration | `go test ./internal/repository/ -run TestInboundDeduplicate` | inbound_dedup_test.go | ✅ green |
| 07-03-02 | 03 | 3 | INBD-01 | integration | `go test ./internal/api/handler/ -run TestTelegramWebhookHandler` | telegram_webhook_test.go | ✅ green |
| 07-03-03 | 03 | 3 | INBD-01 | integration | `go test ./internal/api/handler/ -run TestWABAWebhook_Inbound` | waba_webhook_test.go | ✅ green |
| 07-03-04 | 03 | 3 | INBD-01 | integration | `go test ./internal/session/ -run TestWhatsAppInbound` | inbound_test.go | ✅ green |

### Plan 07-04: Inbound Webhook Forwarding & Audit Logging

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | Test File | Status |
|---------|------|------|-------------|-----------|-------------------|-----------|--------|
| 07-04-01 | 04 | 4 | INBD-02 | integration | `go test ./internal/platform/queue/ -run TestWebhookWorker_Inbound` | webhook_worker_test.go | ✅ green |
| 07-04-02 | 04 | 4 | INBD-03 | — | — | — | ⚠️ MANUAL |

---

## Requirement Coverage Summary

| Requirement | Description | Tests | Status |
|-------------|-------------|-------|--------|
| **MEDIA-01** | Unified media field in message payload | 5 tests (validation + handler subtests) | ✅ COVERED |
| **MEDIA-02** | Per-channel media upload/download paths | 3 tests (WA Web + WABA + Telegram) | ✅ COVERED |
| **MEDIA-03** | Media storage policy (download, size limits, S3) | 8 tests (download + S3 roundtrip subtests) | ✅ COVERED |
| **INBD-01** | Inbound message ingestion from all providers | 4 tests (dedup + WABA + Telegram + WA Web inbound) | ✅ COVERED |
| **INBD-02** | Forward inbound messages via webhook | 1 test (WebhookWorker_Inbound) | ✅ COVERED |
| **INBD-03** | Inbound audit logging with Trace-ID | — | ⚠️ MANUAL |

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Inbound audit log rows created with direction='inbound' and trace_id | INBD-03 | Requires DB inspection after full message flow; audit writer is a background batch process | 1. Send inbound message via any channel. 2. Query `audit_logs WHERE direction='inbound'`. 3. Verify trace_id correlation. |
| PII opt-in redaction in webhook payload | INBD-02 (partial) | Compliance verification — requires inspecting actual webhook payload content | 1. Set workspace `pii_opt_in=false`. 2. Send inbound with contacts/location. 3. Verify webhook payload has hashed sender and no PII. |

---

## Validation Sign-Off

- [x] All tasks have automated verify or manual-only justification
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references — all test files now exist
- [x] No watch-mode flags
- [x] Feedback latency < 6s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** ✅ Validated

---

## Validation Audit 2026-06-27

| Metric | Count |
|--------|-------|
| Total requirements | 6 |
| Automated (COVERED) | 5 |
| Manual-only | 1 (INBD-03 audit logging DB verification) |
| Gaps found | 0 |
| Previously pending → resolved | 12 tasks (all upgraded from ⬜ pending to ✅ green) |
| Escalated | 0 |

**Nyquist assessment:** 5/6 requirements have automated verification. The 1 manual-only item (INBD-03 audit logging) requires database inspection after a full end-to-end message flow through the batch audit writer, which cannot be isolated in a unit test. All previously `❌ W0` test files now exist and pass.
