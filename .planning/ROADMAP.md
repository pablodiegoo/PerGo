# Roadmap: PerGo

## Overview

PerGo is built as a durable work-queue pipeline: a thin ingestion gateway, NATS JetStream as the durability boundary, stateless channel workers behind a plugin Dispatcher interface, PostgreSQL as the system of record for identity and audit, and a server-rendered admin console. 

## Milestones

- ✅ **v1.0 MVP** — Phases 1-11 (shipped 2026-07-14)
- ✅ **v1.1 Campaign Engine** — Phases 12-16 (shipped 2026-07-16)
- ✅ **v1.2 PRD Gaps Integration** — Phases 17-20 (shipped 2026-07-17)
- 🏃 **v1.3 Chatwoot & Typebot Integrations** — Phases 21-24

## Phases

<details>
<summary>✅ v1.0 MVP (Phases 1-11) — SHIPPED 2026-07-14</summary>

- [x] Phase 1: Core Foundation & Trace Logging — completed 2026-07-14
- [x] Phase 2: Multi-Tenant Dashboard Admin Shell — completed 2026-07-14
- [x] Phase 3: Message Ingest API & Rate Limiting — completed 2026-07-14
- [x] Phase 4: WhatsApp Web Adapter (whatsmeow) & Pairing UI — completed 2026-07-14
- [x] Phase 5: Official Channels (WABA/Telegram) & Fallback Engine — completed 2026-07-14
- [x] Phase 6: Outbound Webhook Delivery & Settings UI — completed 2026-07-14
- [x] Phase 7: Conversational View Data Layer & Webhook verification — completed 2026-07-14
- [x] Phase 8: Multi-Instance Connections & Dashboard UI — completed 2026-07-14
- [x] Phase 9: Conversational Inbox Chat UI & Toast Notifications — completed 2026-07-14
- [x] Phase 10: OOB Cursor inbox polling & dynamic layout — completed 2026-07-14
- [x] Phase 11: Settings Configurations accordion nested UI — completed 2026-07-14

</details>

<details>
<summary>✅ v1.1 Campaign Engine (Phases 12-16) — SHIPPED 2026-07-16</summary>

- [x] Phase 12: Campaign Engine (2/2 plans) — completed 2026-07-15
- [x] Phase 12.1: Address tech debt: sidebar active highlighting (1/1 plan) — completed 2026-07-15
- [x] Phase 13: Deepen media engine (1/1 plan) — completed 2026-07-15
- [x] Phase 14: User API action logs (1/1 plan) — completed 2026-07-15
- [x] Phase 15: CSS standardization (1/1 plan) — completed 2026-07-15
- [x] Phase 16: Deprecate workspace subviews (1/1 plan) — completed 2026-07-15

</details>

<details>
<summary>✅ v1.2 PRD Gaps Integration (Phases 17-20) — SHIPPED 2026-07-17</summary>

- [x] Phase 17: Multi-Webhook Subscriptions (2/2 plans) — completed 2026-07-16
- [x] Phase 18: Omnichannel Contact Merging (2/2 plans) — completed 2026-07-16
- [x] Phase 19: Webhook Messaging Verbs Engine (2/2 plans) — completed 2026-07-16
- [x] Phase 20: WABA Read Receipts & Status Updates (2/2 plans) — completed 2026-07-17

</details>

- [x] Phase 21: Chatwoot Integration (2/2 plans) — completed 2026-07-17
- [x] Phase 22: Typebot Integration (2/2 plans) — completed 2026-07-17
- [x] Phase 23: Stateful Handoff Routing (2/2 plans) — completed 2026-07-17
- [x] Phase 24: Refactor Webhook Verbs Engine to Polymorphic VerbHandlers (1/1 plan) — completed 2026-07-18
- [x] Phase 24.2: Close gap: TYPE-04 — populate ConnectionID, SenderIdentity, and TraceID in TypebotForwarder queue message (1/1 plan) — completed 2026-07-19
- [ ] Phase 24.1: Close gap: wire Typebot forwarder and reconcile form schema

## Progress

| Phase | Milestone | Plans Complete | Status | Completed |
|---|---|---|---|---|
| 12. Campaign Engine | v1.1 | 2/2 | Complete | 2026-07-15 |
| 12.1. Sidebar active highlighting | v1.1 | 1/1 | Complete | 2026-07-15 |
| 13. Deepen media engine | v1.1 | 1/1 | Complete | 2026-07-15 |
| 14. User API action logs | v1.1 | 1/1 | Complete | 2026-07-15 |
| 15. CSS standardization | v1.1 | 1/1 | Complete | 2026-07-15 |
| 16. Deprecate workspace subviews | v1.1 | 1/1 | Complete | 2026-07-15 |
| 17. Multi-Webhook Subscriptions | v1.2 | 2/2 | Complete | 2026-07-16 |
| 18. Omnichannel Contact Merging | v1.2 | 2/2 | Complete | 2026-07-16 |
| 19. Webhook Messaging Verbs Engine | v1.2 | 2/2 | Complete | 2026-07-16 |
| 20. WABA Read Receipts & Status | v1.2 | 2/2 | Complete | 2026-07-17 |
| 21. Chatwoot Integration | v1.3 | 2/2 | Complete    | 2026-07-17 |
| 22. Typebot Integration | v1.3 | 2/2 | Complete    | 2026-07-17 |
| 23. Stateful Handoff Routing | v1.3 | 2/2 | Complete    | 2026-07-17 |
| 24. Refactor Webhook Verbs Engine to Polymorphic VerbHandlers | v1.3 | 1/1 | Complete | 2026-07-18 |
| 24.2. Close gap: TYPE-04 (TraceID) | v1.3 | 1/1 | Complete | 2026-07-19 |

---
*Roadmap created: 2026-07-14*
*Last updated: 2026-07-18 after v1.3 completion*
