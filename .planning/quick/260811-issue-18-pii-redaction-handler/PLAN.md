---
task: 02 — PII redaction handler for slog
issue: 18
---
# Quick Plan: Issue #18 PII Redaction Handler for slog

<task>
<files>
- internal/platform/obs/redacting_handler.go
- internal/platform/obs/redacting_handler_test.go
- internal/platform/obs/logging.go
</files>
<action>
1. Create `internal/platform/obs/redacting_handler.go` implementing `slog.Handler` with PII redaction logic.
2. Implement options `WithSensitiveKeys` and `WithExtraSensitiveKeys` with default sensitive keys list (`phone`, `phone_number`, `recipient`, `sender`, `contact`, `email`, `body`, `text`, `content`, `message_body`, `payload`).
3. Wire `NewRedactingHandler` into `NewLogger`, `NewLoggerWithWriter`, and `LoggerFromContext` in `internal/platform/obs/logging.go`.
4. Add unit tests covering flat redaction, group recursion, case-insensitivity, custom keys, non-sensitive passthrough in `internal/platform/obs/redacting_handler_test.go`.
</action>
<verify>
<automated>
go test -v ./internal/platform/obs/...
</automated>
</verify>
<done>
- RedactingHandler implements slog.Handler
- PII redaction works for flat and nested groups with case-insensitive key matching
- NewLogger wraps handlers with RedactingHandler by default
- Unit tests pass for obs package
</done>
</task>
