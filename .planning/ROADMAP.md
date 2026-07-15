# Roadmap: PerGo

## Overview

PerGo is built as a durable work-queue pipeline: a thin ingestion gateway, NATS JetStream as the durability boundary, stateless channel workers behind a plugin Dispatcher interface, PostgreSQL as the system of record for identity and audit, and a server-rendered admin console. For Milestone 1.1, we introduce the Campaign Engine to support batch sending and automated list cleansing directly from the admin panel.

## Phases

- [x] **Phase 12: Campaign Engine** - Bulk messaging campaign dashboard with CSV sanitization, flexible regex variables interpolation, batch throttling via NATS JetStream, and enriched outbound logging. (completed 2026-07-15)
- [x] **Phase 12.1: Address tech debt: sidebar active highlighting** - Fix active styling highlighting and Settings accordion expansion for workspace-scoped campaigns page. (INSERTED) (completed 2026-07-15)

## Phase Details

### Phase 12: Campaign Engine

**Goal**: Implement throttled campaign sending via CSV mailings, dynamic variable mapping, NATS batches, and enriched logging.
**Mode**: mvp
**Depends on**: Phase 11
**Requirements**: CAMP-01, CAMP-02, CAMP-03, CAMP-04, CAMP-05, CAMP-06, CAMP-07, CAMP-08
**Success Criteria** (what must be TRUE):

  1. Operator can upload a CSV mailing list via the admin panel, view validation metrics (valid vs duplicate vs formatting errors), and see dynamic columns shortcut buttons.
  2. Operator can configure template variables via input text fields supporting multi-variable interpolation (e.g. `{{nome}} de {{cidade}}`), schedule campaign dispatch, configure batch size, and view estimated campaign duration.
  3. System parses CSV, sanitizes data, schedules batches, and dispatches them in the background via NATS JetStream, with configurable delays and random jitter.
  4. System persists campaign metadata in `outbound_logs` using the Enriched Logs architecture with compound database indexes, verified via tests.

**Plans**: 2/2 plans complete

- [x] 12-01-PLAN.md
- [x] 12-02-PLAN.md

**UI hint**: yes

## Progress

**Execution Order:**
Phases execute in numeric order: 12

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 12. Campaign Engine | 2/2 | Complete   | 2026-07-15 |
| 12.1. Address tech debt: sidebar active highlighting | 1/1 | Complete    | 2026-07-15 |

### Phase 13: deepen-media-engine-to-consolidate-storage-pipelines

**Goal:** [To be planned]
**Requirements**: TBD
**Depends on:** Phase 12
**Plans:** 1/1 plans complete

Plans:

- [ ] 13-01-PLAN.md

- [x] TBD (run /gsd-plan-phase 13 to break down) (completed 2026-07-15)

### Phase 14: user-api-action-logs

**Goal:** Implement administrative action audit logging for dashboard operators and API keys.
**Requirements**: LOG-01, LOG-02, LOG-03
**Depends on:** Phase 13
**Plans:** 1 plans

Plans:

- [ ] 14-01-PLAN.md

### Phase 15: css-standardization

**Goal:** [To be planned]
**Requirements**: TBD
**Depends on:** Phase 14
**Plans:** 0 plans

Plans:

- [ ] TBD (run /gsd-plan-phase 15 to break down)

---
*Roadmap created: 2026-07-14*
*Granularity: standard | Mode: mvp | Phase convention: sequential*

### Phase 12.1: Address tech debt: sidebar active highlighting (INSERTED)

**Goal:** [Urgent work - to be planned]
**Requirements**: TBD
**Depends on:** Phase 12
**Plans:** 1/1 plans complete

Plans:

- [ ] 12.1-01-PLAN.md

- [x] TBD (run /gsd-plan-phase 12.1 to break down) (completed 2026-07-15)
