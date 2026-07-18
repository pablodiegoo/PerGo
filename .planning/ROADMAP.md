# Roadmap: PerGo

## Overview

PerGo is built as a durable work-queue pipeline: a thin ingestion gateway, NATS JetStream as the durability boundary, stateless channel workers behind a plugin Dispatcher interface, PostgreSQL as the system of record for identity and audit, and a server-rendered admin console. 

## Milestones

- ✅ **v1.0 MVP** — Phases 1-11 (shipped 2026-07-14)
- ✅ **v1.1 Campaign Engine** — Phases 12-16 (shipped 2026-07-16)
- ✅ **v1.2 PRD Gaps Integration** — Phases 17-20 (shipped 2026-07-17)
- 🚧 **v1.3 Chatwoot & Typebot Integrations** — Phases 21-23 (in progress)

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

### 🚧 v1.3 Chatwoot & Typebot Integrations (Phases 21-23)

- [x] Phase 21: Chatwoot Integration (2 plans) (completed 2026-07-17)
- [x] Phase 22: Typebot Integration (2 plans) (completed 2026-07-17)
- [x] Phase 23: Stateful Handoff Routing (1/2 plans) (completed 2026-07-17)

---

## Phase Details

### Phase 21: Chatwoot Integration

**Goal**: Enable human agent messaging synchronization by implementing the Chatwoot connection settings panel, the built-in inbound webhook receiver adapter, and outbound message ingestion/dispatch pipelines.
**Mode**: standard
**Depends on**: Phase 20
**Requirements**: CHAT-01, CHAT-02, CHAT-03, CHAT-04
**Success Criteria**:

  1. Operator can save and test a Chatwoot connection configuration in the PerGo connection settings panel.
  2. Incoming WABA/Telegram messages from contacts are automatically sent to the Chatwoot workspace, creating a corresponding contact and message there.
  3. Human agent replies in Chatwoot successfully trigger the `/api/integrations/chatwoot` webhook receiver, enqueuing outbound messages to the target contact in PerGo.

### Phase 22: Typebot Integration

**Goal**: Enable bot automation by implementing the Typebot connection settings panel, the Typebot webhook receiver adapter, and forwarding inbound contact messages to the bot execution engine.
**Mode**: standard
**Depends on**: Phase 21
**Requirements**: TYPE-01, TYPE-02, TYPE-03, TYPE-04
**Success Criteria**:

  1. Operator can save and test a Typebot connection configuration in the settings panel.
  2. Inbound messages from contacts are forwarded to Typebot's execution API, initiating/maintaining session context correctly.
  3. Bot replies in Typebot trigger the `/api/integrations/typebot` webhook, enqueuing outbound messages to the contact via PerGo.

### Phase 23: Stateful Handoff Routing

**Goal**: Prevent bot/human crosstalk by implementing the stateful `bot_active` flag, auto-disabling the bot on human replies, adding the `pause_bot` messaging verb, manually toggling bot status, and implementing inactivity timeouts.
**Mode**: standard
**Depends on**: Phase 22
**Requirements**: HAND-01, HAND-02, HAND-03, HAND-04, HAND-05, HAND-06
**Success Criteria**:

  1. Contacts table includes a boolean `bot_active` column and `bot_paused_at` timestamp.
  2. When a human agent replies via Chatwoot, `bot_active` is automatically toggled to `false`.
  3. Inbound messages are only forwarded to Typebot when `bot_active` is `true`.
  4. A webhook payload returning the `pause_bot` verb correctly sets `bot_active` to `false` for the target duration.
  5. Operators can manually toggle the `bot_active` switch on/off directly from the active contact detail panel in the Inbox UI.
  6. After 12 hours of agent inactivity, the system automatically resets `bot_active` to `true` on the next inbound message.

---

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

### Phase 24: Refactor Webhook Verbs Engine to Polymorphic VerbHandlers

**Goal:** [To be planned]
**Requirements**: TBD
**Depends on:** Phase 23
**Plans:** 1/1 plans complete

Plans:

- [x] 24-PLAN.md

- [x] TBD (run /gsd-plan-phase 24 to break down) (completed 2026-07-18)

---
*Roadmap created: 2026-07-14*
*Last updated: 2026-07-17 after v1.3 definition*
