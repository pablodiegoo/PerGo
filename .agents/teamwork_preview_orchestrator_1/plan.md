# Execution Plan — PerGo Open Issues (#39, #41, #42, #43, #44, #45)

## Overview
This plan orchestrates the implementation and verification of open issues #39, #41, #42, #43, #44, and #45 in PerGo. Issue #40 is explicitly skipped per user instructions.

## Phases & Strategy

### Phase 0: Initial Survey & Architecture Mapping
- **Goal**: Map codebase state, existing tests, interfaces, and exact locations for each issue.
- **Action**: Dispatch 3 parallel Explorers / Spec Miners (`explorer_1`, `explorer_2`, `spec_miner_1`).
- **Output**: `PROJECT.md` created at repository root with Feature Inventory, Milestones, and Interface Contracts.

### Phase 1: Milestone 1 — Refactoring & Import Cycle Fixes (#39, #42)
- **Goal**:
  - Issue #39: Move `SecurityHeaders` middleware to `internal/platform/echo/` to eliminate imports from `internal/api/` in `echo.go`.
  - Issue #42: Refactor fat handlers (extract error wrapping in Telegram using `%w`, delegate CSV export logic, isolate idempotency checks, move inline `/tags` closure in `main.go`).
- **Iteration Loop**: Explorer → Worker → 2 Reviewers + 2 Challengers + Forensic Auditor gate.

### Phase 2: Milestone 2 — Idempotency SQL Fixes (#41)
- **Goal**: Fix broken positional placeholders (`$1, $2`, etc.) in SQL queries within `internal/repository/idempotency.go`.
- **Iteration Loop**: Explorer → Worker → 2 Reviewers + 2 Challengers + Forensic Auditor gate.

### Phase 3: Milestone 3 — Outbound Webhooks HMAC-SHA256 (#43)
- **Goal**: Implement HMAC-SHA256 signature generation (`X-PerGo-Signature`) using workspace secret; add storage & migrations for the secret.
- **Iteration Loop**: Explorer → Worker → 2 Reviewers + 2 Challengers + Forensic Auditor gate.

### Phase 4: Milestone 4 — Campaign Tag Filtering & Selector (#44)
- **Goal**: Add `tag_ids` filtering to `POST /api/v1/campaigns`, update recipient enrollment query to filter by tags, update HTMX admin UI tag selector.
- **Iteration Loop**: Explorer → Worker → 2 Reviewers + 2 Challengers + Forensic Auditor gate.

### Phase 5: Milestone 5 — Campaign Worker Audit Log Emissions (#45)
- **Goal**: Wire campaign worker to emit audit logs (`workspace_id`, `trace_id`, `event_type`, `payload`) on state changes (`sent`, `delivered`, `failed`).
- **Iteration Loop**: Explorer → Worker → 2 Reviewers + 2 Challengers + Forensic Auditor gate.

### Phase 6: Final Integration Verification & Hardening
- **Goal**: Run complete test suite and forensic integrity verification across all changes.
- **Output**: Completion report sent to Sentinel.
