---
phase: 23
slug: stateful-handoff-routing
status: draft
nyquist_compliant: true
wave_0_complete: false
created: 2026-07-17
---

# Phase 23 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test |
| **Config file** | none |
| **Quick run command** | `go test ./internal/inbound/... ./internal/webhook/...` |
| **Full suite command** | `go test ./...` |
| **Estimated runtime** | ~5 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/inbound/... ./internal/webhook/...`
- **After every plan wave:** Run `go test ./...`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 5 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 23-01-01 | 01 | 1 | HAND-01 | — | N/A | migration | `goose -dir internal/platform/postgres/migrations status` | ❌ W0 | ⬜ pending |
| 23-01-02 | 01 | 1 | HAND-01 | — | N/A | unit | `go test ./internal/repository/...` | ✅ | ⬜ pending |
| 23-01-03 | 01 | 1 | HAND-02 | — | Skip forwarding to bot when inactive | unit | `go test ./internal/integration/typebot/...` | ✅ | ⬜ pending |
| 23-01-04 | 01 | 1 | HAND-06 | — | Bot auto-resets after 12h human inactivity | unit | `go test ./internal/inbound/...` | ✅ | ⬜ pending |
| 23-01-05 | 01 | 1 | HAND-04 | — | Pause bot verb changes state optionally with duration | unit | `go test ./internal/webhook/...` | ✅ | ⬜ pending |
| 23-01-06 | 01 | 1 | HAND-03 | — | Chatwoot agent reply pauses bot | unit | `go test ./internal/api/handler/...` | ✅ | ⬜ pending |
| 23-02-01 | 02 | 2 | HAND-03 | — | Operator Inbox reply pauses bot | unit | `go test ./internal/api/handler/admin/...` | ✅ | ⬜ pending |
| 23-02-02 | 02 | 2 | HAND-05 | — | Manual UI toggle button is secure and displays state | manual | — | ✅ | ⬜ pending |
| 23-02-03 | 02 | 2 | HAND-05 | — | UI HTMX controller handles toggle endpoint | unit | `go test ./internal/api/handler/admin/...` | ✅ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/integration/typebot/forwarder_test.go` — stubs for testing forwarding interception
- [ ] `internal/inbound/processor_test.go` — stubs for testing 12h inactivity cooldown reset
- [ ] `internal/webhook/verbs_test.go` — stubs for testing `pause_bot` verb
- [ ] `internal/api/handler/chatwoot_webhook_test.go` — stubs for testing Chatwoot webhook auto-pause

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Manual UI Toggle badge rendering and interaction | HAND-05 | Requires visual browser rendering and user interaction | 1. Open PerGo Admin Console.<br>2. Select a contact chat thread.<br>3. Click the "Bot Ativo" green status badge. Verify it changes to grey "Bot Pausado".<br>4. Click it again. Verify it changes back to green "Bot Ativo". |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 5s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
