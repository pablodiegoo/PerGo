---
gsd_state_version: 1.0
milestone: v1.3
milestone_name: Chatwoot & Typebot Integrations
current_phase: 23
current_phase_name: Stateful Handoff Routing
status: executing
stopped_at: Phase 23 UI-SPEC approved
last_updated: "2026-07-17T21:55:43.835Z"
last_activity: 2026-07-17
last_activity_desc: Phase 22 complete, transitioned to Phase 23
progress:
  total_phases: 3
  completed_phases: 2
  total_plans: 4
  completed_plans: 4
  percent: 67
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-06-25)

**Core value:** A single API request delivers a message through any configured channel with automatic fallback — without per-message markup, without vendor lock-in, and with full custody of transaction data on infrastructure you control.
**Current focus:** Phase 21 — chatwoot-integration

## Current Position

Phase: 23 — Stateful Handoff Routing
Plan: Not started
Status: Ready to execute
Last activity: 2026-07-17 — Phase 22 complete, transitioned to Phase 23

## Performance Metrics

**Velocity:**

- Total plans completed: 25
- Average duration: —
- Total execution time: 0 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 3 | 3 | - | - |
| 04 | 3 | - | - |
| 5 | 4 | - | - |
| 06 | 1 | - | - |
| 7 | 4 | - | - |
| 09 | 3 | - | - |
| 10.1 | 1 | - | - |
| 12.1 | 1 | - | - |
| 13 | 1 | - | - |
| 21 | 2 | - | - |
| 22 | 2 | - | - |

**Recent Trend:**

- Last 5 plans: —
- Trend: — (no execution yet)

*Updated after each plan completion*
| Phase 03 P01 | 2min | 2 tasks | 4 files |
| Phase 07 P01 | - | 5 tasks | - |
| Phase 07 P02 | - | 5 tasks | - |
| Phase 07 P03 | - | 6 tasks | - |
| Phase 07 P04 | - | 4 tasks | - |
| Phase 21 P01 | 15m | 3 tasks | 9 files |
**Per-Plan Metrics:**

| Plan | Duration | Tasks | Files |
|------|----------|-------|-------|
| Phase 21 P02 | 25m | - tasks | - files |
| Phase 21 P02 | 25m | 3 tasks | 11 files |

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table.
Recent decisions affecting current work:

- [Roadmap]: 7-phase decomposition — foundation schema decisions locked in Phase 1 before any message flows (research-validated ordering: expensive-to-retrofit schema goes first)
- [Roadmap]: WhatsApp Web (highest-risk channel) + all durability machinery (dedup, terminal-session, goroutine lifecycle) lands in Phase 4 with the queue — not deferred to official channels phase
- [Roadmap]: Phase 2 (Admin Shell) and Phase 3 (Ingest API & Queue) are independent after Phase 1 and may execute in parallel
- [Phase 03]: Publisher interface defined now for DI; JetStream implementation deferred to Plan 2 — Enables handler tests without real NATS; Plan 2 implements the interface
- [Phase 11]: Context-driven active path routing in PathMiddleware to expand accordion and highlight active settings sub-items.
- [Phase 11]: Context-driven server-side pre-rendering of the workspace selector dropdown to prevent reload flash.
- [Phase 21]: Used a single unified integrations database table for third-party config storage — D-01
- [Phase 21]: Stored integration-specific credentials as encrypted JSON block using KEK AES-256-GCM — D-02
- [Phase 21]: Maintained a local mapping database table chatwoot_mappings referencing connection_id — D-03
- [Phase 21]: Authenticated webhook endpoint using AuthMiddleware query-parameter token validation — D-05
- [Phase 21]: Checks local mapping cache before routing inbound messages to prevent external API searches — D-04
- [Phase 21]: Filters incoming Chatwoot webhook payloads strictly to outgoing, public, user-initiated messages — D-06

### Pending Todos

- Phase 3 execution: Run `/gsd-execute-phase 03` to begin
- docker-compose.yml updated with NATS `-js` flag for JetStream

### Blockers/Concerns

- [Research flag]: Phase 4 — WhatsApp ban-risk + whatsmeow version drift surface needs phase-specific research during planning (whatsmeow issue #810 still open)
- [Research flag]: Phase 5 — WABA template + 24h window specifics need primary-source verification against Meta docs (Facebook docs blocked during research)
- [Research flag]: Phase 6 — Webhook HMAC signing scheme choice (HMAC-SHA1 a la Twilio vs secret_token header a la Telegram) needs brief design pass

### Roadmap Evolution

- Phase 07.1 inserted after Phase 7: Close gap: v1.0 audit gaps (URGENT)
- Phase 8 added: Multi-Instance Connections & Dashboard UI
- Phase 9 added: Conversational Inbox
- Phase 10.1 inserted after Phase 10: Close gaps (URGENT)
- Phase 12.1 inserted after Phase 12: Address tech debt: sidebar active highlighting (URGENT)

## Deferred Items

Items acknowledged and carried forward from previous milestone close:

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| *(none)* | | | |

## Quick Tasks Completed

| Quick ID | Date | Description | Status |
|---|---|---|---|
| GS5 | 2026-06-28 | criar-tela-configuracao-credenciais-canais | complete ✓ |
| GS6 | 2026-06-28 | criar-tela-playground-testes-websocket | complete ✓ |
| GS7 | 2026-06-28 | auto-carregar-templates-meta | complete ✓ |
| GS8 | 2026-06-28 | validar-credenciais-canais-tela | complete ✓ |
| GS9 | 2026-06-28 | enviar-templates-playground | complete ✓ |
| GS10 | 2026-06-28 | automatizar-registro-webhooks | complete ✓ |
| GS11 | 2026-06-28 | corrigir-hot-reload-nats | complete ✓ |
| GS12 | 2026-06-28 | configurar-docker-compose-producao | complete ✓ |
| GS13 | 2026-06-28 | renomear-projeto-para-pergo | complete ✓ |
| GS14 | 2026-06-28 | documentar-configuracao-provedores | complete ✓ |
| GS15 | 2026-06-29 | guias-interativos-dashboard | complete ✓ |
| GS16 | 2026-06-29 | atualizar-documentacao-projeto | complete ✓ |
| GS17 | 2026-06-29 | configuracao-verify-token | complete ✓ |
| GS18 | 2026-06-29 | implementar-inbox-canais-e-logs-outbound | complete ✓ |
| GS19 | 2026-07-06 | 20260706-devices-modal-inboxes-fix | complete ✓ |
| GS20 | 2026-07-07 | 260706-uzz-quando-clico-em-testar | complete ✓ |
| GS21 | 2026-07-09 | fix-connections-decrypt-and-layout-tweak | complete ✓ |
| 260713-tsm | 2026-07-14 | implement-socks5-proxy-support-whatsmeow | complete ✓ |
| 260713-uuo | 2026-07-14 | implement-dynamic-onboarding-logic | complete ✓ |
| 260715-eg9 | 2026-07-15 | implement-connections-list-in-campaign-f | complete ✓ |
| 260715-f3q | 2026-07-15 | review-and-update-gitattributes-and-gitignore | complete ✓ |
| 260715-ixv | 2026-07-15 | landing-page | complete ✓ |
| 260716-upv | 2026-07-17 | implement-mcp-server | complete ✓ |
| 260716-v4e | 2026-07-17 | quando-um-usuario-tenta-enviar-uma-mensa | complete ✓ |

## Session Continuity

Last session: 2026-07-17T21:55:43.821Z
Stopped at: Phase 23 UI-SPEC approved
Resume file: .planning/phases/23-stateful-handoff-routing/23-UI-SPEC.md

## Operator Next Steps

- Start Phase 21 with /gsd-discuss-phase 21
