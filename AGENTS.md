# AGENTS.md

## Agent skills

### Issue tracker

Issues and specs live in GitHub Issues using the `gh` CLI. See `docs/agents/issue-tracker.md`.
**CRITICAL AGENT RULE**: Proactively keep the issue tracker in sync. Whenever you start an issue, hit a blocker, make significant progress, or complete the issue, immediately use `gh issue comment`, `edit`, or `close` to reflect the new status before moving on.

### Triage labels

Canonical 5-role triage label vocabulary (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context layout (`CONTEXT.md` + `docs/adr/` at repo root; ecosystem rules in `../CONTEXT-MAP.md`). See `docs/agents/domain.md`.

### Design system

Single source of truth UI contract in `DESIGN.md` at project root. Consult `DESIGN.md` before generating or modifying UI components, HTML templates, or CSS styles.

### Coding standards

Contribution rules in `CONTRIBUTING.md` and review-stage judgement calls in `../CODING_STANDARDS.md`.
