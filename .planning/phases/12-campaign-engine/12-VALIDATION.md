---
phase: 12
slug: campaign-engine
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-14
---

# Phase 12 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test |
| **Config file** | none |
| **Quick run command** | `go test -v ./internal/repository/dispatch_test.go` |
| **Full suite command** | `go test -race ./...` |
| **Estimated runtime** | ~5 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test -v ./internal/repository/dispatch_test.go`
- **After every plan wave:** Run `go test -race ./...`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 5 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 12-01-01 | 01 | 1 | CAMP-08 | — | N/A | integration | `go test -run TestCampaignRepository` | ❌ W0 | ⬜ pending |
| 12-01-02 | 01 | 1 | CAMP-02, CAMP-03, CAMP-04, CAMP-06 | — | N/A | unit | `go test -run TestCampaignHelpers` | ❌ W0 | ⬜ pending |
| 12-01-03 | 01 | 1 | CAMP-05, CAMP-07 | — | N/A | integration | `go test -run TestCampaignWorker` | ❌ W0 | ⬜ pending |
| 12-02-01 | 02 | 2 | CAMP-01, CAMP-08 | — | N/A | unit/integration | `go test -run TestCampaignAPI` | ❌ W0 | ⬜ pending |
| 12-02-02 | 02 | 2 | CAMP-01, CAMP-04, CAMP-05, CAMP-06 | — | N/A | unit | `go test -run TestCampaignTemplates` | ❌ W0 | ⬜ pending |
| 12-02-03 | 02 | 2 | — | — | N/A | integration | `curl http://localhost:3000/admin/login` | ❌ W0 | ⬜ pending |
| 12-02-04 | 02 | 2 | CAMP-01, CAMP-07 | — | N/A | manual | `Visual Check` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/repository/campaign_test.go` — stubs for WABA campaigns DB repository tests
- [ ] `internal/platform/queue/campaign_worker_test.go` — NATS JetStream batch throttling tests

*If none: "Existing infrastructure covers all phase requirements."*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Upload CSV in Admin Dashboard UI | CAMP-01 | Requires browser interaction | Go to `/admin/campaigns`, upload a test CSV, and verify dynamic preview highlights correctly. |
| Cancel Campaign via HTMX Button | CAMP-07 | Requires browser interaction | Click "Cancel" on a running campaign, verify the state updates to cancelled. |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 5s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
