---
phase: 8
slug: multi-instance-connections-dashboard-ui
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-29
---

# Phase 8 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test |
| **Config file** | none |
| **Quick run command** | `go test -v ./internal/repository/...` |
| **Full suite command** | `go test -v ./...` |
| **Estimated runtime** | ~10 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test -v ./internal/repository/...`
- **After every plan wave:** Run `go test -v ./...`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 15 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 08-01-01 | 01 | 1 | D-01 | — | Migration integrity | db | `go test -v ./internal/repository/...` | ❌ W0 | ⬜ pending |
| 08-01-02 | 01 | 1 | D-01 | — | Connection Repository CRUD | unit | `go test -v ./internal/repository/...` | ❌ W0 | ⬜ pending |
| 08-02-01 | 02 | 1 | D-02 | — | Ingest payload routing | unit | `go test -v ./internal/api/...` | ❌ W0 | ⬜ pending |
| 08-02-02 | 02 | 1 | D-02 | — | Dynamic adapter credential fetch | unit | `go test -v ./internal/channel/...` | ❌ W0 | ⬜ pending |
| 08-02-03 | 02 | 1 | D-02 | — | whatsmeow SOCKS5 proxy dialer | unit | `go test -v ./internal/channel/whatsapp/...` | ❌ W0 | ⬜ pending |
| 08-03-01 | 03 | 2 | D-04 | — | Tailwind/daisyUI compilation/styles | visual | `make build` | ❌ W0 | ⬜ pending |
| 08-03-02 | 03 | 2 | D-05 | — | Dynamic onboarding checklist check | unit | `go test -v ./internal/api/...` | ❌ W0 | ⬜ pending |
| 08-03-04 | 03 | 2 | D-03 | — | WhatsApp connection limit check | unit | `go test -v ./internal/api/...` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/repository/connection_migration_test.go` — stubs for D-01 migration validation
- [ ] `internal/channel/whatsapp/proxy_test.go` — SOCKS5 proxy dialer validation test stub

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| QR pairing via HTMX/SSE | D-04 | Requires real phone scan | Open browser to `/admin`, trigger pairing, verify QR rendering and scan callback |
| Webhook incoming message simulation | D-04 | Requires human interaction | Go to active dashboard, click "Simulate Incoming Message", verify log insertion and flash animation |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 15s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
