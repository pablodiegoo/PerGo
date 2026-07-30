# Roadmap: PerGo

## Overview

PerGo is built as a durable work-queue pipeline: a thin ingestion gateway, NATS JetStream as the durability boundary, stateless channel workers behind a plugin Dispatcher interface, PostgreSQL as the system of record for identity and audit, and a server-rendered admin console. 

## Milestones

- ✅ **v1.0 MVP** — Phases 1-11 (shipped 2026-07-14)
- ✅ **v1.1 Campaign Engine** — Phases 12-16 (shipped 2026-07-16)
- ✅ **v1.2 PRD Gaps Integration** — Phases 17-20 (shipped 2026-07-17)
- ✅ **v1.3 Chatwoot & Typebot Integrations** — Phases 21-24.2.1 (shipped 2026-07-20)
- ✅ **v1.4 Omnichannel Integrations** — Phases 25-27 (shipped 2026-07-20)
- ✅ **v1.5 Email Channels & Tracking Engine** — Phase 28 (shipped 2026-07-25)
- ✅ **v1.6 Connection Slugs & API Channel Routing** — Phase 29 (shipped 2026-07-26)

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

<details>
<summary>✅ v1.3 Chatwoot & Typebot Integrations (Phases 21-24.2.1) — SHIPPED 2026-07-20</summary>

- [x] Phase 21: Chatwoot Integration (2/2 plans) — completed 2026-07-17
- [x] Phase 22: Typebot Integration (2/2 plans) — completed 2026-07-17
- [x] Phase 23: Stateful Handoff Routing (2/2 plans) — completed 2026-07-17
- [x] Phase 24: Refactor Webhook Verbs Engine to Polymorphic VerbHandlers (1/1 plan) — completed 2026-07-18
- [x] Phase 24.2: Close gap: TYPE-04 — populate ConnectionID, SenderIdentity, and TraceID in TypebotForwarder queue message (1/1 plan) — completed 2026-07-19
- [x] Phase 24.2.1: Fix Typebot message construction gap (completed 2026-07-19)
- [x] Phase 24.1: Close gap: wire Typebot forwarder and reconcile form schema (1/1 plan) — completed 2026-07-19

</details>

<details>
<summary>✅ v1.4 Omnichannel Integrations (Phases 25-27) — SHIPPED 2026-07-20</summary>

- [x] Phase 25: Implement JSON-to-Protobuf mapping for rich interactive messages (1/1 plan) — completed 2026-07-20
- [x] Phase 26: Implement Telegram Inline Keyboards and Forum Threads mapping (1/1 plan) — completed 2026-07-20
- [x] Phase 27: Implement Instagram Stories handling and Quick Replies mapping (1/1 plan) — completed 2026-07-20

</details>

<details>
<summary>✅ v1.5 Email Channels & Tracking Engine (Phase 28) — SHIPPED 2026-07-25</summary>

- [x] Phase 28: Email Channels & Tracking Engine (SMTP, Amazon SES, Mautic, Open/Click Tracking) — completed 2026-07-25

</details>

<details>
<summary>✅ v1.6 Connection Slugs & API Channel Routing (Phase 29) — SHIPPED 2026-07-26</summary>

- [x] Phase 29: Connection Slugs & Human-Friendly Channel Identifiers for API Routing (4/4 plans) — completed 2026-07-26

</details>

### v1.7 WABA Deep Integration (Phases 30-33)

- [x] Phase 30: Session Window & Inbound Foundation (6/6 plans) — completed 2026-07-26
  - **Success criteria:**
    1. Inbound WABA messages upsert `contact_sessions.last_inbound_at` timestamp
    2. Non-template messages to contacts with expired 24h window receive HTTP 422 `session_window_expired`
    3. WABA worker re-validates window at dispatch time — messages queued near boundary are caught
    4. `session.expiring_soon` event fires at 23h mark for workspace webhook subscribers
    5. Click-to-WhatsApp ad conversations use 72h window instead of 24h

- [x] Phase 31: Template CRUD, Meta Graph API Sync & Local Cache (4/4 plans) — completed 2026-07-26
  - **Success criteria:**
    1. Operator can create/edit/delete templates via REST API and admin UI with components synced to Meta
    2. Templates are cached locally in PostgreSQL with in-memory lookup for dispatch
    3. `message_template_status_update` webhooks update local status and invalidate cache
    4. Quality score changes (GREEN→YELLOW→RED) are tracked and alert operators
    5. Admin UI shows visual WhatsApp-style template preview with parameter interpolation

- [x] Phase 32: Template Dispatch, Validation Engine & Meta Flows — `POST /messages` support for `type: "template"` with automatic parameter binding by name + language, local validation engine (parameter counts, character limits, button config, category), dispatch block for non-APPROVED templates, smart session-window fallback (auto-upgrade freeform to default template), `type: "flow"` dispatch transformer, HMAC-signed `flow_token` generation, `nfm_reply` two-stage JSON decoding, Data Exchange endpoint middleware with RSA/AES encryption. Implements DISP-01, DISP-02, DISP-03, DISP-04, FLOW-01, FLOW-02, FLOW-03, FLOW-04.
  - **Success criteria:**
    1. `POST /messages` with `type: "template"` resolves template by name+language and sends with parameter binding
    2. Invalid template parameters (wrong count, exceeded limits, bad category) are rejected before Meta API call
    3. Non-APPROVED template dispatch returns HTTP 422 with clear status explanation
    4. Freeform messages outside 24h window auto-upgrade to configured default template
    5. Meta Flows dispatch works with signed flow_token and nfm_reply responses are decoded into structured events

- [x] Phase 032.1: Close gap: DISP-02 strict variable count validation (1/1 plan) — completed 2026-07-30
- [x] **Phase 33: Commerce Catalogs & Order Processing** (5/5 plans) — completed 2026-07-30
  - **Success criteria:**
    1. `POST /messages` with `type: "product"` sends single-product interactive message via WABA
    2. `POST /messages` with `type: "product_list"` sends multi-product list with sections
    3. `default_catalog_id` is configurable per WABA connection and auto-injected
    4. Inbound order webhooks are parsed into `order.created` events with idempotent processing
    5. Invalid `catalog_id` or missing SKU returns pre-flight validation error

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
| 24.1. Close gap: wire Typebot forwarder and reconcile form schema | v1.3 | 1/1 | Complete | 2026-07-19 |
| 24.2. Close gap: TYPE-04 (TraceID) | v1.3 | 1/1 | Complete    | 2026-07-19 |
| 25. Implement JSON-to-Protobuf mapping for rich interactive messages | v1.4 | 1/1 | Complete    | 2026-07-20 |
| 26. Implement Telegram Inline Keyboards and Forum Threads mapping | v1.4 | 1/1 | Complete    | 2026-07-20 |
| 27. Implement Instagram Stories handling and Quick Replies mapping | v1.4 | 1/1 | Complete    | 2026-07-20 |
| 28. Email Channels & Tracking Engine | v1.5 | 2/2 | Complete | 2026-07-25 |
| 29. Connection Slugs & API Channel Routing | v1.6 | 4/4 | Complete    | 2026-07-26 |
| 30. Session Window & Inbound Foundation | v1.7 | 6/6 | Complete | 2026-07-26 |
| 31. Template CRUD, Meta Sync & Cache | v1.7 | 4/4 | Complete | 2026-07-26 |
| 32. Template Dispatch, Validation & Meta Flows | v1.7 | 5/5 | Complete | 2026-07-26 |
| 32.1. Close gap: DISP-02 strict variable count validation | v1.7 | 1/1 | Complete | 2026-07-30 |
| 33. Commerce Catalogs & Order Processing | v1.7 | 5/5 | Complete | 2026-07-30 |

---
*Roadmap created: 2026-07-14*
*Last updated: 2026-07-26 after v1.7 milestone start*
