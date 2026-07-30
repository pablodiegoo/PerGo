---
phase: 33
slug: commerce-catalogs-order-processing
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-30
---

# Phase 33 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` stdlib (`go test`) |
| **Config file** | `go.mod` |
| **Quick run command** | `go test -v -race ./internal/domain/... ./internal/outbound/... ./internal/channel/whatsapp/...` |
| **Full suite command** | `go test -v -race ./internal/...` |
| **Estimated runtime** | ~10 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test -v -race ./internal/domain/... ./internal/outbound/... ./internal/channel/whatsapp/...`
- **After every plan wave:** Run `go test -v -race ./internal/...`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 15 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 33-01-01 | 01 | 1 | COMM-01, COMM-02 | T-33-01 / — | Product domain structs enforce section/item limits | unit | `go test -v -race ./internal/domain/...` | ❌ W0 | ⬜ pending |
| 33-02-01 | 02 | 1 | COMM-01, COMM-03 | T-33-02 / — | Ingestion rejects missing catalog_id with HTTP 422 | unit | `go test -v -race ./internal/outbound/... ./internal/api/handler/...` | ❌ W0 | ⬜ pending |
| 33-03-01 | 03 | 1 | COMM-01, COMM-02, COMM-05 | T-33-03 / — | Meta API error codes 131009/131084 map to terminal error | unit | `go test -v -race ./internal/channel/whatsapp/...` | ❌ W0 | ⬜ pending |
| 33-04-01 | 04 | 2 | COMM-04 | T-33-04 / — | Inbound order parsing & wamid dedup emit order.created | unit/integration | `go test -v -race ./internal/api/handler/... ./internal/inbound/...` | ❌ W0 | ⬜ pending |
| 33-05-01 | 05 | 2 | COMM-05 | T-33-05 / — | Templ message bubble renders order summary & product cards | unit | `go test -v -race ./internal/ui/view/...` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/domain/message_test.go` — stubs for product payload bounds tests (COMM-01, COMM-02)
- [ ] `internal/outbound/processor_test.go` — stubs for catalog resolution and HTTP 422 checks (COMM-03)
- [ ] `internal/channel/whatsapp/waba_test.go` — stubs for Meta product formatters and error code 131009/131084 classification (COMM-05)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Chat UI Order Bubble Visual Layout | COMM-05 | Visual check of CSS badges and Templ rendering in browser | Open admin Inbox, send order payload, verify summary card layout |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 15s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
