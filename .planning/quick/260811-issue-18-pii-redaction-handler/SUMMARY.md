---
status: complete
date: 2026-08-11
issue: 18
---
# Quick Task Summary: Issue #18 PII Redaction Handler for slog

Implemented `RedactingHandler` in `internal/platform/obs/` to redact sensitive log attributes across flat and nested group attributes.

## Key Changes
- Created `RedactingHandler` implementing `slog.Handler` with case-insensitive O(1) key lookup.
- Supported default sensitive key set (`phone`, `phone_number`, `recipient`, `sender`, `contact`, `email`, `body`, `text`, `content`, `message_body`, `payload`).
- Provided functional options `WithSensitiveKeys` and `WithExtraSensitiveKeys` for key set customization.
- Wrapped `slog.NewJSONHandler` by default in `obs.NewLogger`, `obs.NewLoggerWithWriter`, and `obs.LoggerFromContext`.
- Added unit test suite covering flat redaction, group recursion, case-insensitivity, custom keys, non-sensitive passthrough, and handler option behavior.
