# Codebase Survey & Analysis Report: Issue #41 & Issue #43

## 1. Observation

### Issue #41: Idempotency SQL Positional Placeholders
- **File inspected**: `internal/repository/idempotency.go`
- **Broken Positional Placeholders Observed**:
  1. **Line 55 (`CheckAndStore`)**:
     ```go
     query := `INSERT INTO message_idempotency (workspace_id, key_hash, trace_id, expires_at) VALUES (, , , ) ON CONFLICT (workspace_id, key_hash) DO NOTHING`
     res, err := r.pool.Exec(ctx, query, workspaceID, keyHash, traceID, expiresAt)
     ```
     *Observed issue*: `VALUES (, , , )` is missing `$1, $2, $3, $4`.
  2. **Line 66 (`GetByIdempotencyKey`)**:
     ```go
     query := `SELECT id, workspace_id, key_hash, trace_id, status_code, response_body, provider_message_id, created_at, expires_at FROM message_idempotency WHERE workspace_id =  AND key_hash =  AND expires_at > NOW()`
     err := r.pool.QueryRow(ctx, query, workspaceID, keyHash).Scan(...)
     ```
     *Observed issue*: `WHERE workspace_id =  AND key_hash = ` is missing `$1` and `$2`.
  3. **Line 82 (`UpdateResponse`)**:
     ```go
     query := `UPDATE message_idempotency SET status_code = , response_body = , provider_message_id =  WHERE workspace_id =  AND key_hash = `
     _, err := r.pool.Exec(ctx, query, workspaceID, keyHash, statusCode, responseBody, providerMsgID)
     ```
     *Observed issue*: `SET status_code = , response_body = , provider_message_id =  WHERE workspace_id =  AND key_hash = ` is missing positional placeholders.
     *Parameter binding order*: `r.pool.Exec` receives `(ctx, query, workspaceID, keyHash, statusCode, responseBody, providerMsgID)`.
     Corresponding placeholders:
     - `workspaceID` -> `$1`
     - `keyHash` -> `$2`
     - `statusCode` -> `$3`
     - `responseBody` -> `$4`
     - `providerMsgID` -> `$5`
     Correct SQL: `UPDATE message_idempotency SET status_code = $3, response_body = $4, provider_message_id = $5 WHERE workspace_id = $1 AND key_hash = $2`
  4. **Line 95 (`RecordLedger`)**:
     ```go
     query := `INSERT INTO message_ingress_ledger (id, workspace_id, trace_id, idempotency_key, channel, recipient, status, error_reason, created_at, updated_at) VALUES (, , , , , , , , NOW(), NOW())`
     _, err := r.pool.Exec(ctx, query, entry.ID, entry.WorkspaceID, entry.TraceID, entry.IdempotencyKey, entry.Channel, entry.Recipient, entry.Status, entry.ErrorReason)
     ```
     *Observed issue*: `VALUES (, , , , , , , , NOW(), NOW())` is missing `$1, $2, $3, $4, $5, $6, $7, $8`.
  5. **Line 101 (`UpdateLedgerStatus`)**:
     ```go
     query := `UPDATE message_ingress_ledger SET status = , error_reason = , updated_at = NOW() WHERE workspace_id =  AND trace_id = `
     _, err := r.pool.Exec(ctx, query, workspaceID, traceID, status, errReason)
     ```
     *Observed issue*: `SET status = , error_reason =  WHERE workspace_id =  AND trace_id = ` is missing positional placeholders.
     *Parameter binding order*: `r.pool.Exec` receives `(ctx, query, workspaceID, traceID, status, errReason)`.
     Corresponding placeholders:
     - `workspaceID` -> `$1`
     - `traceID` -> `$2`
     - `status` -> `$3`
     - `errReason` -> `$4`
     Correct SQL: `UPDATE message_ingress_ledger SET status = $3, error_reason = $4, updated_at = NOW() WHERE workspace_id = $1 AND trace_id = $2`

- **Test Suite for Idempotency**:
  - `internal/repository/idempotency_test.go`:
    `TestIdempotencyRepository` tests:
    - `CheckAndStore_And_Get`
    - `UpdateResponse`
    - `Record_And_Update_Ledger`
    - `CleanupExpired`
  - Integration pool runner in `internal/repository/connection_migration_test.go`:
    Reads `PERGO_DATABASE_URL` (default: `postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable`).

---

### Issue #43: Outbound Webhooks HMAC-SHA256
- **Files inspected**:
  - `internal/webhook/dispatcher.go`
  - `internal/webhook/dispatcher_test.go`
  - `internal/repository/workspace.go`
  - `internal/api/handler/admin/workspace.go`
  - `internal/platform/postgres/migrations/035_add_webhook_secret_to_workspaces.sql`

- **Signature Generation & Header**:
  - Implementation in `internal/webhook/dispatcher.go`:
    - Lines 260-267:
      ```go
      func SignPayload(payload []byte, secret []byte, timestamp string) string {
          mac := hmac.New(sha256.New, secret)
          mac.Write([]byte(timestamp))
          mac.Write([]byte("."))
          mac.Write(payload)
          signature := hex.EncodeToString(mac.Sum(nil))
          return fmt.Sprintf("t=%s,v1=%s", timestamp, signature)
      }
      ```
    - Lines 173-182:
      ```go
      timestamp := fmt.Sprintf("%d", time.Now().Unix())
      signature := SignPayload(payloadBytes, secret, timestamp)
      ...
      req.Header.Set("X-PerGo-Signature", signature)
      ```
  - **Secret Storage & Fallback**:
    - Lines 166-171 in `dispatcher.go`: Secret resolves to subscription's `sub.Secret`; if empty/nil, falls back to workspace secret `ws.WebhookSecret` fetched via `wsStore.GetByID(ctx, task.WorkspaceID)`.
    - `Workspace` struct (`internal/repository/workspace.go:20`) includes `WebhookSecret *string json:"webhook_secret,omitempty"`.
    - `WorkspaceRepository` methods: `Create`, `GetByID`, `SetWebhookSecret`, `GenerateWebhookSecret`, `List`.
    - DB Migration `035_add_webhook_secret_to_workspaces.sql`: `ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS webhook_secret VARCHAR(128);`.
    - Admin UI & HTTP endpoints in `internal/api/handler/admin/workspace.go`: `GetWebhookSecret`, `GenerateWebhookSecret`.

- **Test Suite Results**:
  - Executed: `export PATH=/home/pablodiegoo/.local/go/bin:$PATH; go test ./internal/webhook -v`
  - Result: `PASS` (Unit test `TestDefaultDispatcher_Dispatch/Normal_dispatch_signs_and_sends_HTTP_request` verifies `X-PerGo-Signature` header presence; `TestDispatcher_WorkspaceWebhookSecretFallback` verifies fallback to workspace secret).

---

## 2. Logic Chain

1. **Analysis of Issue #41**:
   - The Go driver `pgx/v5` requires positional placeholders in Postgres SQL strings formatted as `$1, $2, $3, ...`.
   - In `internal/repository/idempotency.go`, five queries contain blank placeholders `, , ,` or missing parameters after `=`, causing `pgx` to parse malformed SQL syntax.
   - Restoring the `$1, $2, ...` positional placeholders matching the argument list passed to `Exec` / `QueryRow` will fix the SQL execution errors.
   - Once fixed, running `TestIdempotencyRepository` against a live Postgres database running migrations up through `038_create_message_ingress_ledger.sql` will validate the repository operations.

2. **Analysis of Issue #43**:
   - The implementation of outbound webhook signature generation (`X-PerGo-Signature` header) via HMAC-SHA256, secret management per workspace, database column/migration (`035_add_webhook_secret_to_workspaces.sql`), and unit tests (`dispatcher_test.go`) already exists and is fully functional in `internal/webhook` and `internal/repository`.
   - All unit tests in `internal/webhook` pass successfully (`PASS` output).
   - Any worker task addressing Issue #43 should verify edge cases, signature formatting consistency, and integration test coverage.

---

## 3. Caveats

- **Issue #41 Postgres environment**: Integration tests in `internal/repository/idempotency_test.go` call `getTestPoolWithMigrations(t)`, which skips when PostgreSQL is unreachable or fails authentication. In local environment testing, PostgreSQL on port 5432 requires valid DB credentials (`PERGO_DATABASE_URL`).
- **Issue #43 Complete status**: The outbound webhook signature implementation, workspace secret storage, and DB migration already exist in the codebase. Verification should ensure no missing requirements remain.

---

## 4. Conclusion

- **Issue #41**: Highly actionable bug fix. Modify `internal/repository/idempotency.go` to add the missing positional parameters (`$1, $2, ...`) across the 5 identified SQL queries (lines 55, 66, 82, 95, 101).
- **Issue #43**: Outbound webhook HMAC-SHA256 signature generation (`X-PerGo-Signature`), per-workspace secret storage (`webhook_secret`), DB migration `035_add_webhook_secret_to_workspaces.sql`, and unit tests are already in place and passing in `internal/webhook`.

---

## 5. Verification Method

To verify independently:

1. **Issue #41 Verification**:
   - Inspect `internal/repository/idempotency.go` lines 55, 66, 82, 95, 101 to verify positional placeholders `$1`, `$2`, etc. match argument indices.
   - Run repository integration test:
     ```bash
     export PATH=/home/pablodiegoo/.local/go/bin:$PATH
     PERGO_DATABASE_URL="postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable" go test ./internal/repository -v -run TestIdempotencyRepository
     ```

2. **Issue #43 Verification**:
   - Inspect `internal/webhook/dispatcher.go` (lines 173-182, 260-267) to confirm `X-PerGo-Signature` header and HMAC-SHA256 calculation.
   - Inspect `internal/repository/workspace.go` and `internal/platform/postgres/migrations/035_add_webhook_secret_to_workspaces.sql` for `webhook_secret` storage.
   - Run webhook unit test suite:
     ```bash
     export PATH=/home/pablodiegoo/.local/go/bin:$PATH
     go test ./internal/webhook -v
     ```
