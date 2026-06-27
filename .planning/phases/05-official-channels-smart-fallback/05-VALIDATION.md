---
phase: 05
slug: official-channels-smart-fallback
status: complete
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-26
validated: 2026-06-27T11:21:00Z
---

# Phase 05 — Validation Strategy

> Per-phase validation contract for Official Channels & Smart Fallback.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | `go test` (stdlib) |
| **Config file** | none — stdlib testing |
| **Quick run command** | `go test ./internal/channel/... ./internal/session/... ./internal/repository/... ./internal/platform/queue/...` |
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

### Plan 05-01: WABA & Telegram Adapters + Credential Storage

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | Test File | Status |
|---------|------|------|-------------|-----------|-------------------|-----------|--------|
| 05-01-01 | 01 | 1 | WABA-01 | integration | `go test ./internal/channel/whatsapp/ -run TestWABADispatch` | waba_test.go | ✅ green |
| 05-01-01 | 01 | 1 | WABA-01 | unit | `go test ./internal/repository/ -run TestCredentialsRepository` | credentials_test.go | ✅ green |
| 05-01-01 | 01 | 1 | WABA-01 | unit | `go test ./internal/repository/ -run TestCredentialsEmpty` | credentials_test.go | ✅ green |
| 05-01-02 | 01 | 1 | TGRAM-01 | integration | `go test ./internal/channel/telegram/ -run TestTelegramDispatch` | telegram_test.go | ✅ green |

### Plan 05-02: WABA Template Management

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | Test File | Status |
|---------|------|------|-------------|-----------|-------------------|-----------|--------|
| 05-02-01 | 02 | 2 | WABA-02 | integration | `go test ./internal/repository/ -run TestWABATemplateRepository` | waba_template_test.go | ✅ green |
| 05-02-01 | 02 | 2 | WABA-02 | unit | `go test ./internal/api/handler/admin/ -run TestWABATemplateHandler` | waba_template_test.go | ✅ green |
| 05-02-02 | 02 | 2 | WABA-03 | unit | `go test ./internal/domain/ -run TestValidateMessageTemplateValid` | message_test.go | ✅ green |
| 05-02-02 | 02 | 2 | WABA-03 | unit | `go test ./internal/domain/ -run TestValidateMessageTemplateInvalidChannel` | message_test.go | ✅ green |
| 05-02-02 | 02 | 2 | WABA-03 | unit | `go test ./internal/domain/ -run TestValidateMessageTemplateMissingLanguage` | message_test.go | ✅ green |

### Plan 05-03: 24h Window & Telegram Webhooks

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | Test File | Status |
|---------|------|------|-------------|-----------|-------------------|-----------|--------|
| 05-03-01 | 03 | 3 | TGRAM-02 | — | — | — | ⚠️ MANUAL |
| 05-03-02 | 03 | 3 | WABA-04 | unit | `go test ./internal/session/ -run TestWindowChecker_IsWindowOpen` | window_test.go | ✅ green |
| 05-03-02 | 03 | 3 | WABA-04 | unit | `go test ./internal/session/ -run TestWindowChecker_IsWindowOpen/Session_not_found` | window_test.go | ✅ green |
| 05-03-02 | 03 | 3 | WABA-04 | unit | `go test ./internal/session/ -run TestWindowChecker_IsWindowOpen/Window_closed` | window_test.go | ✅ green |
| 05-03-02 | 03 | 3 | WABA-04 | integration | `go test ./internal/repository/ -run TestRecipientSessionRepository` | recipient_session_test.go | ✅ green |

### Plan 05-04: Smart Fallback Router

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | Test File | Status |
|---------|------|------|-------------|-----------|-------------------|-----------|--------|
| 05-04-01 | 04 | 4 | FALL-01 | integration | `go test ./internal/platform/queue/ -run TestWorkerFallbackLoop` | worker_test.go | ✅ green |
| 05-04-01 | 04 | 4 | FALL-01 | unit | `go test ./internal/platform/queue/ -run TestRetryAttemptParsing` | worker_test.go | ✅ green |
| 05-04-01 | 04 | 4 | FALL-01 | unit | `go test ./internal/platform/queue/ -run TestExponentialBackoff` | worker_test.go | ✅ green |
| 05-04-02 | 04 | 4 | FALL-02 | unit (subtests) | Covered in `TestWorkerFallbackLoop` | worker_test.go | ✅ green |
| 05-04-03 | 04 | 4 | FALL-03 | unit | `go test ./internal/platform/queue/ -run TestDeliveryDedup` | worker_test.go | ✅ green |
| 05-04-03 | 04 | 4 | FALL-03 | integration | `go test ./internal/repository/ -run TestMessageDispatchRepository` | dispatch_test.go | ✅ green |

---

## Requirement Coverage Summary

| Requirement | Description | Tests | Status |
|-------------|-------------|-------|--------|
| **WABA-01** | WABA REST adapter implementing Dispatcher | 3 tests (dispatch + credentials) | ✅ COVERED |
| **WABA-02** | WABA template CRUD repository and admin endpoints | 2 tests (repo + handler) | ✅ COVERED |
| **WABA-03** | WABA template message sending validation | 3 tests (valid/invalid/missing lang) | ✅ COVERED |
| **WABA-04** | 24-hour customer window checking | 4 tests (open/closed/not-found/error) | ✅ COVERED |
| **TGRAM-01** | Telegram Bot API adapter implementing Dispatcher | 1 test (dispatch) | ✅ COVERED |
| **TGRAM-02** | Telegram webhook secret token validation | — | ⚠️ MANUAL |
| **FALL-01** | Smart ordered fallback dispatch loop | 3 tests (loop + retry + backoff) | ✅ COVERED |
| **FALL-02** | Terminal error bypass → immediate fallback advance | Covered in fallback loop test | ✅ COVERED |
| **FALL-03** | Fallback-aware dedup via message_dispatches | 2 tests (dedup + repo) | ✅ COVERED |

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Telegram webhook secret token validation end-to-end | TGRAM-02 | Requires live Telegram Bot API webhook registration and secret token exchange | 1. Register a Telegram bot webhook with secret token. 2. Send POST to `/webhooks/telegram` with valid `X-Telegram-Bot-Api-Secret-Token`. 3. Verify 200 response and recipient_sessions upsert. 4. Send with invalid token, verify 401. |
| WABA template sync with Meta Graph API | WABA-02 (partial) | Requires real Meta API credentials and approved templates | Call sync endpoint, verify template status updated from Meta API response. |
| Templates admin page visual rendering | WABA-02 (UI) | Visual/UI verification — templ renders static HTML | Navigate to `/admin/templates`, verify CRUD form and sync buttons render correctly. |

---

## Validation Sign-Off

- [x] All tasks have automated verify or manual-only justification
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 6s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** ✅ Validated

---

## Validation Audit 2026-06-27

| Metric | Count |
|--------|-------|
| Total requirements | 9 |
| Automated (COVERED) | 8 |
| Manual-only | 1 (TGRAM-02 webhook end-to-end) |
| Gaps found | 0 |
| Resolved | 0 |
| Escalated | 0 |

**Nyquist assessment:** 8/9 requirements have automated verification. The 1 manual-only item (TGRAM-02 end-to-end webhook validation) requires live Telegram Bot API integration that cannot be mocked at the unit test level; the token validation logic itself is code-reviewed and verified in the VERIFICATION report.
