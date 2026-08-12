# Original User Request

## 2026-08-12T09:58:21Z

Implement the 6 code review fixes detailed in the `.scratch/code-review-fixes/issues` directory.

Working directory: `/home/pablodiegoo/coding/PerGo`
Integrity mode: benchmark

## Requirements

### R1. Fix circuit breaker half-open state machine
In `internal/platform/breaker`, `RecordFailure` must reset `consecutiveFailures` when transitioning from open to half-open.

### R2. Extract tag-recipient resolution into a shared domain helper
Extract the tag→contact→phone→dedup logic from form-based `Create` and REST `APICreate` handlers in campaign admin code into a single shared function. Extract tag-ID deduplication loop into a helper or set. Remove the `SanitizePhone(contact.Name)` fallback.

### R3. Add server-side recipient validation to form-based campaign Create
After resolving tags + CSV recipients using the shared helper from R2, if `len(recipientRecords) == 0`, return an HTTP 400 or equivalent HTMX-friendly error fragment. Note: This requires R2 to be completed first.

### R4. Surface idempotency and audit errors instead of swallowing them
In `message.go`, log failures in `checkAndRecordIdempotency` and `recordIdempotencyCompletion` using `slog.Error` with trace ID context. In `campaign_worker.go`, rename `EmitAuditLog` to unexported `emitAuditLog`, bundle its 8 parameters into a struct, and log failures at each call site in `processBatch` using `slog.Error`.

### R5. Fix double `%w` error wrap in Telegram adapter
Restructure the `fmt.Errorf` in the S3 media download path to wrap only `ErrTelegramMediaRetryable` with `%w` and format the inner error with `%v` or a nested wrap.

### R6. Make `wsRepo` a required parameter in `NewTagAdminHandler`
Change its signature to accept `wsRepo *repository.WorkspaceRepository` as a regular parameter (not variadic). Remove the nil-check in `RedirectToWorkspaceTags`. Update call sites in `main.go` and tests.

## Verification Resources
- Use the existing test suites (e.g., `go test ./...`) to verify these fixes. Ensure that no existing tests regress. 

## Acceptance Criteria

### Circuit Breaker
- [ ] New test case in `breaker_test.go` simulates 3+ open→half-open→open cycles and asserts `consecutiveFailures` doesn't grow unboundedly.
- [ ] `RecordSuccess` in half-open correctly transitions to closed with zeroed counters.

### Tag-recipient resolution
- [ ] A single shared function returns deduplicated records and seen phones, and both `Create` and `APICreate` use it.
- [ ] No inline `already` deduplication loops exist, and `SanitizePhone(contact.Name)` fallback is removed.

### Recipient validation
- [ ] `Create` returns a clear user-facing error message when no recipients are resolved.
- [ ] A test case in `campaign_test.go` verifies this error behavior.

### Idempotency and audit errors
- [ ] `slog.Error` with trace ID context is used for errors in `message.go` idempotency checks and `emitAuditLog` call sites.
- [ ] `emitAuditLog` is unexported and takes a single struct parameter.

### Telegram error wrap
- [ ] S3 download error uses a single `%w` for `ErrTelegramMediaRetryable`.
- [ ] `errors.Is(err, ErrTelegramMediaRetryable)` works correctly, and test coverage is maintained.

### Tag handler signature
- [ ] `NewTagAdminHandler` signature updated, nil-guard removed, `main.go` and `tag_test.go` updated, and all tag handler tests pass.
