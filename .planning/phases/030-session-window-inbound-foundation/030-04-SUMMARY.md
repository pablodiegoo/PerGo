---
phase: 30
plan: 04
subsystem: outbound
tags: ["session-window", "ingestion", "http-422", "pre-flight"]
key-files:
  - internal/outbound/processor.go
  - internal/api/handler/message.go
  - internal/session/window.go
metrics:
  tasks_completed: 2
  tasks_total: 2
---

# Plan 030-04 Summary

## Accomplishments
- Added `SessionWindowError` to `internal/session/window.go` carrying `WindowStatus` and `Source`.
- Updated `OutboundProcessor.Ingest` to perform an ingestion-time pre-flight session window check (`safetyBuffer = 0`) for `whatsapp_cloud` freeform messages when `WindowChecker` is configured.
- Added `WindowChecker` field to `MessageHandler` and mapped `*session.SessionWindowError` in `MessageHandler.Create` to an HTTP 422 `SESSION_WINDOW_EXPIRED` response.

## Commits
- `feat(30-04): ingestion-time session window check and HTTP 422 error mapping`

## Verification
- `go build ./...` compiled cleanly.
- `go test ./internal/outbound/... ./internal/api/handler/...` passed.
