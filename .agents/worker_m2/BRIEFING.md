# BRIEFING — 2026-08-12T10:00:37Z

## Mission
Implement Requirements R2 and R3: Tag-recipient resolution domain refactoring and recipient validation in campaign creation handlers.

## 🔒 My Identity
- Archetype: implementer, qa, specialist
- Roles: implementer, qa, specialist
- Working directory: /home/pablodiegoo/coding/PerGo/.agents/worker_m2
- Original parent: 968f7049-c4ec-4ec1-83ea-75394b05aef8
- Milestone: Requirements R2 & R3 Implementation

## 🔒 Key Constraints
- Exclusive file ownership:
  - `internal/domain/campaign.go`
  - `internal/api/handler/admin/campaign.go`
  - `internal/api/handler/admin/campaign_test.go`
- Do not cheat, hardcode test outputs, or create facades.
- Minimal change principle.

## Current Parent
- Conversation ID: 968f7049-c4ec-4ec1-83ea-75394b05aef8
- Updated: 2026-08-12T10:00:37Z

## Task Summary
- **What to build**:
  1. `TagContactLister` interface, `DeduplicateUUIDs`, and `ResolveTagRecipients` in `internal/domain/campaign.go`.
  2. Refactor form `Create` and REST `APICreate` in `internal/api/handler/admin/campaign.go` to use domain functions and add recipient validation on form `Create`.
  3. Add validation unit test in `internal/api/handler/admin/campaign_test.go`.
- **Success criteria**:
  - `go test -v ./internal/domain/... ./internal/api/handler/admin/...` passes.
  - No `already := false` loops or `SanitizePhone(contact.Name)` fallbacks remain in campaign handler.
  - Clear HTTP 400 error message on zero-recipient form creation.
- **Interface contracts**: PROJECT.md / ORIGINAL_REQUEST.md

## Change Tracker
- **Files modified**: none yet
- **Build status**: TBD
- **Pending issues**: none

## Quality Status
- **Build/test result**: TBD
- **Lint status**: TBD
- **Tests added/modified**: TBD

## Loaded Skills
- **spike-findings-pergo**: /home/pablodiegoo/coding/PerGo/.agents/skills/spike-findings-pergo/SKILL.md
- **sketch-findings-pergo**: /home/pablodiegoo/coding/PerGo/.agents/skills/sketch-findings-pergo/SKILL.md
