# Handoff Report — Codebase Explorer Survey for Issues #39 and #42

## 1. Observation

### Issue #39: `SecurityHeaders` Middleware & Import Cycle
- **Current File**: `internal/platform/echo/echo.go:9` imports `apimiddleware "github.com/pablojhp.pergo/internal/api/middleware"`.
- **Current Usage**: `internal/platform/echo/echo.go:20` calls `e.Use(apimiddleware.SecurityHeaders())`.
- **Middleware Definition**: `SecurityHeaders()` is defined in `internal/api/middleware/security.go` (along with `SecurityConfig`, `DefaultSecurityConfig`, and `SecurityHeadersWithConfig`).
- **Tests**: `internal/api/middleware/security_test.go` tests default, custom, and omitted header configurations. `internal/platform/echo/echo_test.go` tests `New()` server creation and verifies security headers on HTTP responses.

### Issue #42: Fat Handlers, Error Wrapping & Inline Closures
1. **Telegram Error Wrapping**:
   - `internal/channel/telegram/telegram.go:119`:
     ```go
     return "", fmt.Errorf("%w: telegram media download from S3 failed: %v", ErrTelegramMediaRetryable, err)
     ```
     `err` is formatted using `%v` instead of `%w`, which prevents callers from unwrapping the underlying S3/network error cause via `errors.Is(err, ...)` or `errors.As(...)`.

2. **CSV Export Logic in Fat Handlers**:
   - `internal/api/handler/admin/audit.go:132-162`: `exportCSV` contains manual CSV header configuration and streaming loop over audit log entries using `csv.NewWriter(c.Response())`.
   - `internal/api/handler/admin/tag.go:346-395`: `ExportContactsCSV` fetches contacts, fetches tags for each contact in a loop, and manually writes rows via `csv.NewWriter(c.Response())`.
   - `internal/api/handler/admin/campaign.go:357-384`: `DownloadSkipped` formats headers and writes rejected CSV rows via `csv.NewWriter(c.Response())`.

3. **Idempotency Checks in `SendMessage` Handler**:
   - `internal/api/handler/message.go:88-116`: `SendMessage` manually extracts the `Idempotency-Key` header, computes sha256 hashes, queries `IdempotencyRepo.GetByIdempotencyKey`, calls `CheckAndStore`, and calls `RecordLedger`.
   - `internal/api/handler/message.go:296-299`: `SendMessage` updates ledger status and response in `IdempotencyRepo`.

4. **Inline `/tags` Closure in `main.go`**:
   - `cmd/pergo/main.go:663-680`:
     ```go
     adminGroup.GET("/tags", func(c *echo.Context) error {
         ctx := c.Request().Context()
         cookie, err := c.Cookie("pergo-active-workspace")
         var wsID uuid.UUID
         if err == nil && cookie != nil && cookie.Value != "" {
             wsID, _ = uuid.Parse(cookie.Value)
         }
         if wsID == uuid.Nil {
             list, err := wsRepo.List(ctx, 1)
             if err == nil && len(list) > 0 {
                 wsID = list[0].ID
             }
         }
         if wsID == uuid.Nil {
             return c.String(http.StatusBadRequest, "nenhum workspace encontrado. Crie um workspace primeiro.")
         }
         return c.Redirect(http.StatusFound, fmt.Sprintf("/admin/workspaces/%s/tags", wsID.String()))
     })
     ```
     This inline closure contains workspace resolution and HTTP redirect logic embedded directly inside `main.go`.

### Existing Test Suite & Toolchain
- **Go Binary Path**: `/home/pablodiegoo/.local/go/bin/go` (version `go1.26.4 linux/amd64`).
- **Test Status**: All tests in `./internal/platform/echo`, `./internal/api/middleware`, `./internal/api/handler`, `./internal/api/handler/admin`, and `./internal/channel/telegram` execute cleanly and pass.

---

## 2. Logic Chain

1. **Eliminating the Import Cycle (#39)**:
   - `internal/platform/echo` is a low-level platform package. Importing higher-level `internal/api/middleware` inside `internal/platform/echo` violates clean layering architecture principles.
   - By relocating `security.go` and `security_test.go` from `internal/api/middleware/` to `internal/platform/echo/` (updating package name to `echosrv`), `echo.go` can call `SecurityHeaders()` directly without importing `internal/api/middleware`.
   - Result: `internal/platform/echo/echo.go` will have zero imports from `internal/api/`.

2. **Refactoring Fat Handlers & Error Wrapping (#42)**:
   - **Telegram Error Wrapping**: Updating `internal/channel/telegram/telegram.go:119` to use `%w` for `err` (e.g. `fmt.Errorf("%w: telegram media download from S3 failed: %w", ErrTelegramMediaRetryable, err)`) enables full error chain inspection with `errors.Is`/`errors.As`.
   - **CSV Export Delegation**: Extracting CSV generation/formatting into helper functions (e.g., in `internal/api/handler/admin/` or a dedicated CSV utility) keeps handler logic concise, focused on HTTP request validation and response streaming.
   - **Idempotency Isolation**: Extracting SHA256 key calculation, cache lookup, and ledger recording out of `SendMessage` into a helper method on `MessageHandler` reduces handler complexity and improves readability.
   - **Inline Closure Relocation**: Extracting the inline `/tags` redirect closure from `cmd/pergo/main.go:663-680` into a handler method on `TagAdminHandler` (e.g. `tagAdminHandler.RedirectToWorkspaceTags(c)`) cleans up `main.go` routing definitions.

---

## 3. Caveats
- Moving `security.go` to `internal/platform/echo/` will require updating any external call site expecting `middleware.SecurityHeaders()`, though currently `echo.go` is the only consumer.
- Go toolchain `go` is not in standard `PATH` by default in subshell environments; commands must use `PATH=$PATH:/home/pablodiegoo/.local/go/bin go test ...`.

---

## 4. Conclusion
- **Issue #39**: Ready for implementation by relocating `security.go` & `security_test.go` into `internal/platform/echo/` and updating `echo.go` imports.
- **Issue #42**: Ready for implementation by updating Telegram error wrapping with `%w`, isolating idempotency handling in `MessageHandler`, delegating CSV formatting in admin handlers, and replacing the inline `/tags` closure in `main.go` with a method call on `TagAdminHandler`.

---

## 5. Verification Method

### Test Execution Commands
Run package unit tests with the local Go binary:
```bash
PATH=$PATH:/home/pablodiegoo/.local/go/bin go test ./internal/platform/echo ./internal/api/middleware ./internal/api/handler ./internal/api/handler/admin ./internal/channel/telegram
```

### Import Verification for Issue #39
Verify `internal/platform/echo` has zero imports from `internal/api`:
```bash
PATH=$PATH:/home/pablodiegoo/.local/go/bin go list -f '{{ .Imports }}' ./internal/platform/echo | grep 'internal/api'
```
Expected output: empty (no matches).
