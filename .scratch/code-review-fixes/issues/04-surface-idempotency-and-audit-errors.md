# 04 — Surface idempotency and audit errors instead of swallowing them

**What to build:** Several critical error paths are silently discarded with `_ =`, violating the project's "100% trace-correlated logging" constraint and the architecture doc's error-wrapping standard. This ticket makes those errors visible.

In `message.go`: `checkAndRecordIdempotency` must `slog.Error` when `CheckAndStore` or `RecordLedger` fails (these are not request-fatal, but must be logged with trace ID). Same for `recordIdempotencyCompletion`.

In `campaign_worker.go`: `EmitAuditLog` call sites must log failures. Additionally:
- Rename `EmitAuditLog` → `emitAuditLog` (it's only used within the package)
- Bundle its 8 parameters into a struct (e.g., `auditDispatchEvent`) to reduce the data-clump smell at every call site

**Blocked by:** None — can start immediately.

**Status:** ready-for-agent

- [ ] `checkAndRecordIdempotency`: `CheckAndStore` and `RecordLedger` errors are logged via `slog.Error` with trace ID context
- [ ] `recordIdempotencyCompletion`: `UpdateLedgerStatus` and `UpdateResponse` errors are logged via `slog.Error` with trace ID context
- [ ] `EmitAuditLog` renamed to `emitAuditLog` (unexported)
- [ ] `emitAuditLog` accepts a struct parameter instead of 8 positional args; all call sites updated
- [ ] `emitAuditLog` failures are logged via `slog.Error` at each call site in `processBatch`
- [ ] Existing tests pass (`message_idempotency_challenge_test.go`, `campaign_worker_test.go`)
