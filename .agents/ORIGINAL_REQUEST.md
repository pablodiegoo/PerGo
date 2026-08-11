# Original User Request

## Initial Request — 2026-08-11T18:15:00Z

# Teamwork Project Prompt — Draft

> Status: Launched
> Goal: Craft prompt → get user approval → delegate to teamwork_preview
> Requested team: [none — teamwork routes from the description]

Implement open issues #39, #41, #42, #43, #44, and #45 in the PerGo repository, skipping #40.

Working directory: /home/pablodiegoo/coding/PerGo
Integrity mode: development

## Requirements

### R1. Refactoring and Fixing Import Cycles (#39, #42)
Move the `SecurityHeaders` middleware to `internal/platform/echo/` to break the import cycle in `echo.go`. Refactor fat handlers by extracting error wrapping in Telegram, delegating CSV export logic, isolating idempotency checks, and moving the inline `/tags` closure in `main.go`.

### R2. Idempotency SQL Fixes (#41)
Fix the broken positional placeholders (using `$1, $2`, etc.) in the SQL queries within `internal/repository/idempotency.go`.

### R3. Outbound Webhooks (#43)
Implement HMAC-SHA256 signature generation for outbound webhooks using the `X-PerGo-Signature` header, compute the signature using a workspace-specific secret, and add storage/migration for this secret.

### R4. Campaign Features (#44, #45)
Add campaign tag filtering to `POST /api/v1/campaigns` and wire the campaign worker to emit audit logs (with `workspace_id`, `trace_id`, `event_type`, and `payload`) on recipient dispatch state changes.

## Acceptance Criteria

### Issue Resolution
- [ ] Issue #39: `internal/platform/echo/echo.go` has zero imports from `internal/api/` and `SecurityHeaders` still runs.
- [ ] Issue #41: `TestIdempotencyRepository` passes against a real Postgres database after fixing placeholders.
- [ ] Issue #42: Handlers are thin, Telegram error wrapping uses `%w`, and existing tests pass.
- [ ] Issue #43: Outbound webhook requests include `X-PerGo-Signature`, and unit/integration tests verify signature logic.
- [ ] Issue #44: `POST /api/v1/campaigns` accepts `tag_ids`, recipient enrollment filters by tags, and the admin UI form includes a tag selector.
- [ ] Issue #45: The campaign worker correctly writes audit events for each dispatch state change (`sent`, `delivered`, `failed`).
