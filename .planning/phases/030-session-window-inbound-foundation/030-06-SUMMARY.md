---
phase: 30
plan: 06
subsystem: session
tags: ["session-ticker", "expiration-warning", "webhooks", "background-job"]
key-files:
  - internal/session/expiring_ticker.go
  - internal/session/expiring_ticker_test.go
  - cmd/pergo/main.go
metrics:
  tasks_completed: 2
  tasks_total: 2
---

# Plan 030-06 Summary

## Accomplishments
- Implemented `SessionTicker` in `internal/session/expiring_ticker.go` running every 5 minutes to query standard (24h) and CTWA (72h) sessions nearing window expiration.
- Formatted and published `session.expiring_soon` webhook events to NATS subject `"webhooks.events"`.
- Called `RecipientSessionRepository.MarkNotifiedExpiring` to prevent duplicate expiration events until a new user message arrives.
- Wired `SessionTicker` in `cmd/pergo/main.go` tied to application lifecycle context.

## Commits
- `feat(30-06): background SessionTicker and session.expiring_soon webhook events`

## Verification
- `go test ./internal/session/... -v` passed all tests.
- `go build ./...` compiled cleanly across the entire codebase.
