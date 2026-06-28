---
phase: 06
slug: webhook-delivery-dlq
status: complete
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-26
validated: 2026-06-27T11:24:00Z
---

# Phase 06 — Validation Strategy

> Per-phase validation contract for Webhook Delivery & DLQ.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | `go test` (stdlib) |
| **Config file** | none — stdlib testing |
| **Quick run command** | `go test ./internal/platform/queue/... ./internal/repository/...` |
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

### Plan 06-01: Webhook Delivery Worker, DLQ, and Admin UI

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | Test File | Status |
|---------|------|------|-------------|-----------|-------------------|-----------|--------|
| 06-01-01 | 01 | 1 | WHOOK-04 | integration | `go test ./internal/repository/ -run TestWebhookDLQRepository` | webhook_dlq_test.go | ✅ green |
| 06-01-02 | 01 | 2 | WHOOK-01 | integration | `go test ./internal/platform/queue/ -run TestEnsureWebhookStream` | webhook_worker_test.go | ✅ green |
| 06-01-02 | 01 | 2 | WHOOK-01 | integration | `go test ./internal/platform/queue/ -run TestWebhookWorker_Integration` | webhook_worker_test.go | ✅ green |
| 06-01-02 | 01 | 2 | WHOOK-01 | integration | `go test ./internal/platform/queue/ -run TestWebhookWorker_Inbound` | webhook_worker_test.go | ✅ green |
| 06-01-03 | 01 | 2 | WHOOK-02 | unit | `go test ./internal/platform/queue/ -run TestSignPayload` | webhook_worker_test.go | ✅ green |
| 06-01-03 | 01 | 2 | WHOOK-03 | integration | Covered in `TestWebhookWorker_Integration` (JSON envelope) | webhook_worker_test.go | ✅ green |
| 06-01-03 | 01 | 2 | WHOOK-05 | unit | `go test ./internal/platform/queue/ -run TestRetryAttemptParsing` | worker_test.go | ✅ green |
| 06-01-03 | 01 | 2 | WHOOK-05 | unit | `go test ./internal/platform/queue/ -run TestExponentialBackoff` | worker_test.go | ✅ green |
| 06-01-03 | 01 | 2 | WHOOK-05 | integration | `go test ./internal/platform/queue/ -run TestWebhookWorker_TerminalErrorDLQ` | webhook_worker_test.go | ✅ green |
| 06-01-04 | 01 | 3 | WHOOK-04 | integration | `go test ./cmd/pergo/ -run TestAdminWebhookDLQHandlers` | admin_webhook_dlq_test.go | ✅ green |

---

## Requirement Coverage Summary

| Requirement | Description | Tests | Status |
|-------------|-------------|-------|--------|
| **WHOOK-01** | Webhook delivery queue with NATS JetStream | 3 tests (stream setup + worker integration + inbound) | ✅ COVERED |
| **WHOOK-02** | Webhook HMAC-SHA256 payload signature | 1 test (SignPayload) | ✅ COVERED |
| **WHOOK-03** | Webhook JSON status envelope schema | Covered in integration test | ✅ COVERED |
| **WHOOK-04** | DLQ persistent storage, listing, admin UI | 2 tests (repo + admin handlers) | ✅ COVERED |
| **WHOOK-05** | Exponential backoff retry + terminal error DLQ routing | 3 tests (retry parsing + backoff + terminal DLQ) | ✅ COVERED |

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Sidebar badge count updates via HTMX | WHOOK-04 (UI) | Visual rendering — templ + HTMX polling | Navigate to `/admin/webhooks`, trigger failed delivery, verify badge count increments on sidebar refresh |
| Webhook config form saves URL and encrypts secret | WHOOK-04 (UI) | Visual form interaction | Fill webhook URL and secret in admin form, submit, verify config persists |

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
| Total requirements | 5 |
| Automated (COVERED) | 5 |
| Manual-only (UI-only) | 2 (visual rendering, not requirement gaps) |
| Gaps found | 0 |
| Previously pending → resolved | 4 tasks (all upgraded from ⬜ pending to ✅ green) |
| Escalated | 0 |

**Nyquist assessment:** 5/5 requirements have automated verification. All previously `⬜ pending` / `❌ W0` tasks are now `✅ green`. The 2 manual items are purely visual UI concerns, not missing test coverage.
