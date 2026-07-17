---
phase: 21
slug: chatwoot-integration
status: draft
nyquist_compliant: true
wave_0_complete: false
created: 2026-07-17
---

# Phase 21 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` toolchain |
| **Config file** | none |
| **Quick run command** | `go test ./internal/repository/...` |
| **Full suite command** | `go test ./...` |
| **Estimated runtime** | ~10 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/repository/...`
- **After every plan wave:** Run `go test ./...`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 10 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 21-01-01 | 01 | 1 | CHAT-01 | T-21-02 | credentials encrypted, mapping isolated | unit | `go test -run TestIntegrationRepository ./internal/repository/...` | ❌ W0 | ⬜ pending |
| 21-01-02 | 01 | 1 | CHAT-01 | — | integrations settings UI | manual | Verify settings form elements | ❌ W0 | ⬜ pending |
| 21-01-03 | 01 | 1 | CHAT-02 | T-21-01 | token middleware validation | integration | `go test -run TestChatwootWebhookAuth ./internal/api/handler/...` | ❌ W0 | ⬜ pending |
| 21-02-01 | 02 | 2 | CHAT-04 | — | sync msg, 404 remote delete | integration | `go test -run TestChatwootClient ./internal/integration/chatwoot/...` | ❌ W0 | ⬜ pending |
| 21-02-02 | 02 | 2 | CHAT-04 | — | InboundProcessor integration | integration | `go test -run TestInboundProcessor ./internal/inbound/...` | ❌ W0 | ⬜ pending |
| 21-02-03 | 02 | 2 | CHAT-03 | T-21-03 | human replies parse & NATS isolated | system | `go test -run TestChatwootWebhookHandler ./internal/api/handler/...` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/repository/integration_test.go` — stubs for CHAT-01
- [ ] `internal/api/handler/chatwoot_webhook_test.go` — stubs for CHAT-02, CHAT-03
- [ ] `internal/integration/chatwoot/client_test.go` — stubs for CHAT-04

*If none: "Existing infrastructure covers all phase requirements."*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Web UI connection management check | CHAT-01 | Requires browser interaction | Verify connecting settings UI panels save and load correctly. |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 10s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
