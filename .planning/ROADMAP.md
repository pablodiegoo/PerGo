# Roadmap: PerGo

## Overview

PerGo is built as a durable work-queue pipeline: a thin ingestion gateway, NATS JetStream as the durability boundary, stateless channel workers behind a plugin Dispatcher interface, PostgreSQL as the system of record for identity and audit, and a server-rendered admin console. For Milestone 1.1, we introduce the Campaign Engine to support batch sending and automated list cleansing directly from the admin panel.

## Phases

- [ ] **Phase 12: Campaign Engine** - Bulk messaging campaign dashboard with CSV sanitization, flexible regex variables interpolation, batch throttling via NATS JetStream, and enriched outbound logging.

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

**Plans**: TBD
**UI hint**: yes

## Progress

**Execution Order:**
Phases execute in numeric order: 12

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 12. Campaign Engine | 0/1 | Pending | - |

---
*Roadmap created: 2026-07-14*
*Granularity: standard | Mode: mvp | Phase convention: sequential*
