# Roadmap: PerGo

## Overview

PerGo is built as a durable work-queue pipeline: a thin ingestion gateway, NATS JetStream as the durability boundary, stateless channel workers behind a plugin Dispatcher interface, PostgreSQL as the system of record for identity and audit, and a server-rendered admin console. For Milestone 1.2, we integrate key PRD gaps: Multi-Webhook Subscriptions, Omnichannel Contact Merging, and the Webhook Messaging Verbs Engine.

## Phases

- [x] **Phase 17: Multi-Webhook Subscriptions** - Webhook subscriptions database model, wildcard event-type matching, concurrent NATS fan-out, per-subscription DLQs, and dashboard management interface. (completed 2026-07-16)
- [x] **Phase 18: Omnichannel Contact Merging** - Unified contact and identities schemas, auto-resolution on incoming messages, contact merge API/dashboard UI, and unified inbox conversation views. (completed 2026-07-16)
- [x] **Phase 19: Webhook Messaging Verbs Engine** - Decoupled parsing of webhook JSON responses, sequential execution of verbs (reply, wait, forward, tag, close), and operator audit logs. (completed 2026-07-16)

## Progress

**Execution Order:**
Phases execute in numeric order: 17 → 18 → 19

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 12. Campaign Engine | 2/2 | Complete | 2026-07-15 |
| 12.1. Address tech debt: sidebar active highlighting | 1/1 | Complete | 2026-07-15 |
| 13. Deepen media engine | 1/1 | Complete | 2026-07-15 |
| 14. User API action logs | 1/1 | Complete | 2026-07-15 |
| 15. CSS standardization | 1/1 | Complete | 2026-07-15 |
| 16. Deprecate workspace subviews | 1/1 | Complete | 2026-07-15 |
| 17. Multi-Webhook Subscriptions | 2/2 | Complete | 2026-07-16 |
| 18. Omnichannel Contact Merging | 2/2 | Complete | 2026-07-16 |
| 19. Webhook Messaging Verbs Engine | 2/2 | Complete | 2026-07-16 |

---

## Phase Details

### Phase 17: Multi-Webhook Subscriptions

**Goal**: Implement schema and core worker infrastructure for multiple webhook subscriptions, wildcard event matching, concurrent NATS fan-out, and UI dashboard.
**Mode**: standard
**Depends on**: Phase 16
**Requirements**: SUBS-01, SUBS-02, SUBS-03, SUBS-04
**Success Criteria** (what must be TRUE):

  1. Operator can configure, test, and manage multiple webhook subscriptions with wildcard filters (e.g. `message.*`) in the settings UI.
  2. The webhook worker concurrently dispatches webhook events matching event filters using a JetStream fan-out queue.
  3. Per-subscription retry logic, exponential backoff, and DLQ persistence are functional and verified.

### Phase 18: Omnichannel Contact Merging

**Goal**: Implement unified contacts/identities schema, auto-matching on inbound events, profile merging, and unified conversation inbox views.
**Mode**: standard
**Depends on**: Phase 17
**Requirements**: CONT-01, CONT-02, CONT-03, CONT-04
**Success Criteria**:

  1. Incoming messages auto-resolve or instantiate a single contact profile in the `contacts` and `contact_identities` tables.
  2. Dashboard UI permits searching and merge operations on contacts with full transaction rollbacks on failure.
  3. Merged contacts display a single consolidated message thread combining WhatsApp and Telegram chat histories in the Inbox.

### Phase 19: Webhook Messaging Verbs Engine

**Goal**: Implement JSON response verbs executor, sequential scheduling (reply, wait, forward, tag, close), and operator logging.
**Mode**: standard
**Depends on**: Phase 18
**Requirements**: VERB-01, VERB-02, VERB-03
**Success Criteria**:

  1. Webhook dispatcher parses valid declarative messaging verbs returned in webhook response payloads.
  2. Verb sequences are processed sequentially, and replies trigger correct outbound routing queue entries.
  3. Action execution errors are logged as workspace audits and visible to operators.

---

### Phase 12: Campaign Engine (Completed)

*Completed on 2026-07-15*

### Phase 12.1: Address tech debt: sidebar active highlighting (Completed)

*Completed on 2026-07-15*

### Phase 13: Deepen media engine (Completed)

*Completed on 2026-07-15*

### Phase 14: User API action logs (Completed)

*Completed on 2026-07-15*

### Phase 15: CSS standardization (Completed)

*Completed on 2026-07-15*

### Phase 16: Deprecate workspace subviews (Completed)

*Completed on 2026-07-15*

---
*Roadmap created: 2026-07-14*
*Last updated: 2026-07-16 after v1.2 definition*
