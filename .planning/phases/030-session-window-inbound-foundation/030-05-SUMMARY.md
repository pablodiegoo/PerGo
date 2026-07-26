---
phase: 30
plan: 05
subsystem: channel
tags: ["waba", "dispatch", "safety-buffer", "terminal-error"]
key-files:
  - internal/channel/whatsapp/waba.go
  - internal/channel/whatsapp/waba_test.go
metrics:
  tasks_completed: 1
  tasks_total: 1
---

# Plan 030-05 Summary

## Accomplishments
- Defined `WindowChecker` interface in `internal/channel/whatsapp/waba.go` using `IsWindowOpenBool(..., safetyBuffer time.Duration) (bool, error)` to prevent import cycles.
- Added dispatch-time session window re-validation in `WABAAdapter.Dispatch` for non-template messages using a 5-minute safety buffer (`5 * time.Minute`).
- Wrapped expired window errors as `channel.NewTerminalError(ErrSessionWindowExpired)` to fail fast and prevent NATS JetStream retries.

## Commits
- `feat(30-05): dispatch-time session window re-validation in WABA worker`

## Verification
- `go test ./internal/channel/whatsapp/... -v` passed.
- `go build ./...` compiled cleanly.
