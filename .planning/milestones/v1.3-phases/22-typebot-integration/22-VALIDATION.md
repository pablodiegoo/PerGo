---
phase: 22
slug: typebot-integration
status: draft
nyquist_compliant: true
wave_0_complete: false
created: 2026-07-17
---

# Phase 22 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test |
| **Config file** | go.mod |
| **Quick run command** | `go test ./internal/integration/typebot/...` |
| **Full suite command** | `go test ./...` |
| **Estimated runtime** | ~5 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/integration/typebot/...`
- **After every plan wave:** Run `go test ./...`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 5 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 22-01-01 | 01 | 1 | TYPE-01 | — | Connection credentials are encrypted | unit | `go test -run TestTypebotConfig ./internal/integration/typebot/...` | ❌ W0 | ⬜ pending |
| 22-01-02 | 01 | 1 | TYPE-04 | — | Inbound customer messages forwarded | integration | `go test -run TestTypebotForwarder ./internal/integration/typebot/...` | ❌ W0 | ⬜ pending |
| 22-02-01 | 02 | 2 | TYPE-02 | — | Webhook receiver handles HTTP request | integration | `go test -run TestTypebotWebhook ./internal/api/handler/...` | ❌ W0 | ⬜ pending |
| 22-02-02 | 02 | 2 | TYPE-03 | — | Typebot responses enqueued to outbound NATS | integration | `go test -run TestTypebotResponses ./internal/integration/typebot/...` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/integration/typebot/client_test.go` — stubs for TYPE-04 client interactions
- [ ] `internal/integration/typebot/forwarder_test.go` — stubs for TYPE-04 forwarder behavior
- [ ] `internal/api/handler/typebot_webhook_test.go` — stubs for TYPE-02 webhook receiver

*If none: "Existing infrastructure covers all phase requirements."*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Settings UI | TYPE-01 | Web UI visual rendering | Navigate to `/workspaces/:id/integrations/typebot`, input configuration, click Save. Check that state is loaded. |

*If none: "All phase behaviors have automated verification."*

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 5s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
