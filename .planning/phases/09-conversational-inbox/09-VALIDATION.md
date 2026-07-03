---
phase: 9
slug: conversational-inbox
status: draft
nyquist_compliant: true
wave_0_complete: false
created: 2026-07-03
---

# Phase 9 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (std testing) |
| **Config file** | none |
| **Quick run command** | `go test -v ./internal/repository/...` |
| **Full suite command** | `go test -v -race ./...` |
| **Estimated runtime** | ~10 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test -v ./internal/repository/...` or specific package tests
- **After every plan wave:** Run `go test -v -race ./...`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 15 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| migration_recipient_sessions_to_column | 01 | 1 | INBD-03 | — | N/A | unit | `go test -v ./internal/repository/...` | ✅ | ⬜ pending |
| enrich_whatsapp_inbound | 01 | 1 | INBD-01, INBD-03 | — | N/A | unit | `go test -v ./internal/session/...` | ✅ | ⬜ pending |
| enrich_telegram_waba_inbound | 01 | 1 | INBD-01, INBD-02, INBD-03 | — | N/A | unit | `go test -v ./internal/api/handler/...` | ✅ | ⬜ pending |
| implement_audit_queries | 01 | 1 | ADMIN-01 | — | N/A | unit | `go test -v ./internal/repository/...` | ✅ | ⬜ pending |
| migration_inbox_read_status | 02 | 2 | ADMIN-01 | — | N/A | unit | `go test -v ./internal/repository/...` | ✅ | ⬜ pending |
| register_inbox_routes | 02 | 2 | ADMIN-01 | — | N/A | unit | `go build ./...` | ✅ | ⬜ pending |
| update_sidebar_navigation | 02 | 2 | ADMIN-01 | — | N/A | unit | `go build ./...` | ✅ | ⬜ pending |
| create_conversation_templates | 02 | 2 | ADMIN-01 | — | N/A | unit | `templ generate` | ✅ | ⬜ pending |
| implement_conversations_handler | 02 | 2 | ADMIN-01 | — | N/A | unit | `go test -v ./internal/api/handler/admin/...` | ✅ | ⬜ pending |
| create_chat_panel_templates | 03 | 3 | ADMIN-01 | — | N/A | unit | `templ generate` | ✅ | ⬜ pending |
| implement_chat_panel_handler | 03 | 3 | ADMIN-01 | — | N/A | unit | `go test -v ./internal/api/handler/admin/...` | ✅ | ⬜ pending |
| implement_live_polling_handler | 03 | 3 | ADMIN-02 | — | N/A | unit | `go test -v ./internal/api/handler/admin/...` | ✅ | ⬜ pending |
| implement_send_message_handler | 03 | 3 | ADMIN-01 | — | N/A | unit | `go test -v ./internal/api/handler/admin/...` | ✅ | ⬜ pending |
| create_inbox_integration_tests | 03 | 3 | ADMIN-01, ADMIN-02 | — | N/A | integration | `go test -v ./internal/api/handler/admin/...` | ✅ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [x] Existing infrastructure covers all phase requirements.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Split-pane dynamic layout scrolling and styling | ADMIN-01 | Visual/UX layout | Boot server, pair device, navigate to `/admin/inbox`, and verify responsive resizing. |
| In-page Toast notifications on background events | ADMIN-02 | Cross-session realtime behavior | Keep Carlos Mendes' chat open, send a simulated webhook to Telegram connection for Pedro Silva, and verify the toast pops up. |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 15s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved 2026-07-03
