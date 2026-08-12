## 2026-08-12T13:00:37Z
You are Worker M3 assigned to implement Requirement R4 (Surface idempotency and audit errors instead of swallowing them).
Your working directory is `/home/pablodiegoo/coding/PerGo/.agents/worker_m3`. Create your directory if needed and write all metadata files (progress.md, handoff.md) there.

Authoritative Request: Read `/home/pablodiegoo/coding/PerGo/.agents/ORIGINAL_REQUEST.md` and `/home/pablodiegoo/coding/PerGo/.agents/explorer_survey_3/handoff.md` first.

File Ownership: You exclusively own `internal/api/handler/message.go`, `internal/platform/queue/campaign_worker.go`, and `internal/platform/queue/campaign_worker_test.go`.

Tasks:
1. In `internal/api/handler/message.go`:
   - In `checkAndRecordIdempotency` and `recordIdempotencyCompletion`, check returned errors from `CheckAndStore`, `RecordLedger`, `UpdateLedgerStatus`, and `UpdateResponse`, and log any non-nil error using `slog.Error` with trace ID context (and workspace ID) instead of discarding with `_ =`.
2. In `internal/platform/queue/campaign_worker.go`:
   - Define `auditDispatchEvent` struct bundling the 8 audit parameters.
   - Rename `EmitAuditLog` to unexported `emitAuditLog(event auditDispatchEvent) error`.
   - Update all call sites in `processBatch` to check the returned error and log failures using `slog.Error`.
3. In `internal/platform/queue/campaign_worker_test.go`:
   - Update call site at line 530 to use `emitAuditLog(auditDispatchEvent{...})`.
4. Run builds and tests (`go test -v ./internal/api/handler/... ./internal/platform/queue/...`).
5. Document commands and exact test outputs in `/home/pablodiegoo/coding/PerGo/.agents/worker_m3/handoff.md`.
