---
phase: 01-Foundation
plan: 04
subsystem: infra
tags: [pprof, expvar, shutdown, orchestrator, graceful-shutdown, debug-server]

requires:
  - phase: 01-Foundation
    provides: "Echo v5 server scaffold, pgxpool dual-access PostgreSQL, auth middleware, audit batch writer"
provides:
  - "pprof debug server on localhost:6060 with /debug/pprof/ and /debug/vars"
  - "expvar metrics: memstats, audit_drops counter, custom RegisterCounter API"
  - "Graceful shutdown orchestrator with LIFO cleanup order"
  - "Composition root wiring all components with ordered shutdown sequence"
  - "Health endpoints (/healthz, /readyz) respond during shutdown drain"
affects: [02-Admin-Shell, 03-Ingest-API, 04-WhatsApp-Web]

tech-stack:
  added: [net/http/pprof, expvar]
  patterns: [debug-server-localhost, orchestrator-lifo, sync-once-idempotency]

key-files:
  created:
    - internal/platform/obs/pprof.go
    - internal/platform/obs/expvar.go
    - internal/platform/shutdown/orchestrator.go
    - cmd/pergo/obs_test.go
  modified:
    - cmd/pergo/main.go

key-decisions:
  - "Debug server on localhost only (127.0.0.1:6060) — never exposed on public port"
  - "Orchestrator uses sync.Once for idempotency — second Shutdown() call is a no-op"
  - "Shutdown order matches CONTEXT.md: Echo → debug → audit → NATS → pgxpool → sqlDB"
  - "Custom DebugServer wrapper to expose listener address for tests"

patterns-established:
  - "Debug server: separate http.Server with pprof + expvar on localhost"
  - "Shutdown orchestrator: Register() + Shutdown(ctx) with LIFO execution"
  - "Composition root: all cleanup functions registered before server start"

requirements-completed: [INFRA-05, OBS-01, OBS-02, OBS-04]

coverage:
  - id: D1
    description: "pprof debug server starts on localhost:6060, GET /debug/pprof/ returns 200"
    requirement: OBS-02
    verification:
      - kind: unit
        ref: "cmd/pergo/obs_test.go#TestPprofServer"
        status: pass
    human_judgment: false
  - id: D2
    description: "expvar handler at /debug/vars returns JSON with memstats"
    requirement: OBS-04
    verification:
      - kind: unit
        ref: "cmd/pergo/obs_test.go#TestExpvarHandler"
        status: pass
    human_judgment: false
  - id: D3
    description: "Custom expvar counter (audit_drops) increments and is queryable via /debug/vars"
    requirement: OBS-04
    verification:
      - kind: unit
        ref: "cmd/pergo/obs_test.go#TestAuditDropCounter"
        status: pass
    human_judgment: false
  - id: D4
    description: "Orchestrator executes cleanup in reverse registration order (LIFO)"
    requirement: INFRA-05
    verification:
      - kind: unit
        ref: "cmd/pergo/obs_test.go#TestOrchestratorOrder"
        status: pass
    human_judgment: false
  - id: D5
    description: "Shutdown completes within deadline with slow cleanup (2s delay)"
    requirement: INFRA-05
    verification:
      - kind: unit
        ref: "cmd/pergo/obs_test.go#TestOrchestratorTimeout"
        status: pass
    human_judgment: false
  - id: D6
    description: "Shutdown handles 30s context with 2s cleanup delay correctly"
    requirement: INFRA-05
    verification:
      - kind: unit
        ref: "cmd/pergo/obs_test.go#TestOrchestratorSlowCleanup"
        status: pass
    human_judgment: false
  - id: D7
    description: "Multiple Shutdown() calls are safe (idempotent via sync.Once)"
    requirement: INFRA-05
    verification:
      - kind: unit
        ref: "cmd/pergo/obs_test.go#TestOrchestratorIdempotent"
        status: pass
    human_judgment: false

duration: 14min
completed: 2026-06-25
status: complete
---

# Phase 1 Plan 4: Observability & Graceful Shutdown Summary

**pprof debug server on localhost:6060, expvar metrics with audit_drops counter, and LIFO shutdown orchestrator wiring Echo → debug → audit → NATS → pgxpool → sqlDB**

## Performance

- **Duration:** 14 min
- **Started:** 2026-06-25T18:40:44Z
- **Completed:** 2026-06-25T18:54:46Z
- **Tasks:** 2 (TDD: RED + GREEN)
- **Files created:** 4, modified: 1

## Accomplishments
- pprof debug server on localhost:6060 with /debug/pprof/ and /debug/vars handlers
- expvar metrics: memstats, audit_drops counter, custom RegisterCounter API
- Shutdown orchestrator with LIFO cleanup order, context deadline support, sync.Once idempotency
- Full composition root wiring all Phase 1 components: Echo, pgxpool, NATS, audit, auth, debug server
- Shutdown order: Echo → debug → audit → NATS → pgxpool → sqlDB (30s timeout)
- 7 unit tests covering debug server, expvar counters, orchestrator order, timeout, and idempotency

## Task Commits

Each task was committed atomically:

1. **Task 1: Failing end-to-end test for pprof, expvar, and graceful shutdown (RED)** - `13e9067` (test)
2. **Task 2: Implement pprof, expvar, shutdown orchestrator, and wire composition root (GREEN)** - `596bf93` (feat)

_TDD tasks may have multiple commits (test -> feat -> refactor)_

## Files Created/Modified
- `internal/platform/obs/pprof.go` - DebugServer with pprof + expvar on localhost, StartDebugServer constructor
- `internal/platform/obs/expvar.go` - AuditDrops package-level counter
- `internal/platform/shutdown/orchestrator.go` - Orchestrator with Register(), Shutdown(ctx), LIFO order, sync.Once idempotency
- `cmd/pergo/obs_test.go` - 7 unit tests covering all observability and shutdown behaviors
- `cmd/pergo/main.go` - Full composition root: config, pgxpool, NATS, audit, debug server, shutdown orchestrator, health handlers, /api/v1/me test route

## Decisions Made
- **Debug server on localhost only:** pprof/expvar bound to 127.0.0.1:6060 — never exposed on public port or via Docker
- **sync.Once for idempotency:** Second Shutdown() call is a safe no-op, preventing double-close panics
- **LIFO shutdown order:** Matches CONTEXT.md spec — HTTP stops first, DB connections last
- **Custom DebugServer wrapper:** Exposes listener address for test verification when using :0 random port

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Removed incorrect context cancellation assertion from TestOrchestratorSlowCleanup**
- **Found during:** Task 2 (GREEN phase verification)
- **Issue:** Test expected `ctx.Err() != nil` after shutdown, but orchestrator doesn't own the context lifecycle — context is only cancelled by the caller's `defer cancel()`
- **Fix:** Removed the assertion that context should be cancelled after shutdown
- **Files modified:** cmd/pergo/obs_test.go
- **Verification:** All 7 tests pass
- **Committed in:** 596bf93

**2. [Rule 3 - Blocking] Added runServer helper for existing TestGracefulShutdown**
- **Found during:** Task 2 (build verification)
- **Issue:** Rewriting main.go removed the `runServer` function that existing test relied on
- **Fix:** Added minimal `runServer(ctx, pool, db)` helper that blocks until ctx is cancelled
- **Files modified:** cmd/pergo/main.go
- **Verification:** go build, go vet, all tests pass
- **Committed in:** 596bf93

---

**Total deviations:** 2 auto-fixed (1 bug, 1 blocking)
**Impact on plan:** Both deviations necessary for correct test behavior. No scope creep.

## Issues Encountered
- http.Server does not have a Listener field — created DebugServer wrapper to expose listener address
- go vet caught unused import after main.go rewrite — removed and re-added as needed

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- Phase 1 Foundation complete: server boots with identity, auth, audit, observability, and graceful shutdown
- pprof available on localhost:6060 for runtime profiling
- expvar counters ready for audit drops and future metrics
- Shutdown orchestrator ready to accept cleanup functions from future components
- Ready for Phase 2 (Admin Shell) or Phase 3 (Ingest API & Queue)

---
*Phase: 01-Foundation*
*Completed: 2026-06-25*

## Self-Check: PASSED

Key files verified on disk:
- [x] internal/platform/obs/pprof.go
- [x] internal/platform/obs/expvar.go
- [x] internal/platform/shutdown/orchestrator.go
- [x] cmd/pergo/obs_test.go
- [x] cmd/pergo/main.go

Commits verified:
- [x] 13e9067 (test(01-04): RED phase)
- [x] 596bf93 (feat(01-04): GREEN phase)

Build: go build OK | Vet: go vet OK | Tests: all pass
