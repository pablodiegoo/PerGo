# Handoff Report: Milestone 3 (Outbound Webhook HMAC-SHA256 - Issue #43)

## 1. Observation

- **Task Scope**: Verify Milestone 3 (Outbound Webhook HMAC-SHA256 Signature - Issue #43).
- **Files Inspected & Code Lines Verified**:
  1. `internal/webhook/dispatcher.go`:
     - Line 260-267: `SignPayload` function computes HMAC-SHA256 signature using `hmac.New(sha256.New, secret)`, writing `timestamp`, `.`, and `payload`, hex-encoding the sum, and returning formatted signature `"t=%s,v1=%s"`.
     - Line 166-171: Secret resolution fallback checks subscription `sub.Secret`; if empty/nil, queries workspace store `d.wsStore.GetByID(ctx, task.WorkspaceID)` and uses `ws.WebhookSecret`.
     - Line 173-174: Generates UNIX timestamp string and calls `SignPayload(payloadBytes, secret, timestamp)`.
     - Line 182: Sets signature HTTP header on webhook request: `req.Header.Set("X-PerGo-Signature", signature)`.
  2. `internal/repository/workspace.go`:
     - Line 20: `Workspace` struct contains `WebhookSecret *string json:"webhook_secret,omitempty"`.
     - Lines 39, 52, 63, 71, 99: `WorkspaceRepository` supports `Create`, `GetByID`, `SetWebhookSecret`, `GenerateWebhookSecret`, and `List` with `webhook_secret` field persistence.
  3. `internal/platform/postgres/migrations/035_add_webhook_secret_to_workspaces.sql`:
     - Line 2-3: `ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS webhook_secret VARCHAR(128);`
  4. `internal/webhook/dispatcher_test.go`:
     - Lines 102-156 (`TestDefaultDispatcher_Dispatch/Normal_dispatch_signs_and_sends_HTTP_request`): Asserts `X-PerGo-Signature` header presence on outbound HTTP requests.
     - Lines 559-615 (`TestDispatcher_WorkspaceWebhookSecretFallback`): Asserts `X-PerGo-Signature` header is correctly computed when falling back to workspace `WebhookSecret`.

- **Test Executions & Results**:
  - `export PATH=$PATH:/home/pablodiegoo/.local/go/bin; go test -count=1 ./internal/webhook/...`
    -> Result: `ok github.com/pablojhp.pergo/internal/webhook 0.174s` (PASS)
  - `export PATH=$PATH:/home/pablodiegoo/.local/go/bin; go test -count=1 -v ./internal/webhook`
    -> Result: `PASS` across unit test suite (including `TestDefaultDispatcher_Dispatch` and `TestDispatcher_WorkspaceWebhookSecretFallback`).
  - `export PATH=$PATH:/home/pablodiegoo/.local/go/bin; go test -count=1 ./internal/repository -v -run TestWorkspace`
    -> Result: `ok github.com/pablojhp.pergo/internal/repository 0.003s [no tests to run]`
  - `export PATH=$PATH:/home/pablodiegoo/.local/go/bin; go test -count=1 ./internal/repository/...`
    -> Result: `ok github.com/pablojhp.pergo/internal/repository 0.101s` (PASS)

## 2. Logic Chain

1. Requirements for Issue #43 specify:
   - HMAC-SHA256 signature generation (`SignPayload`).
   - Adding `X-PerGo-Signature` header to outbound webhook requests.
   - Workspace-level secret fallback (`ws.WebhookSecret`).
   - Database schema migration for workspace secret (`035_add_webhook_secret_to_workspaces.sql`).
2. Source code verification in `internal/webhook/dispatcher.go` confirms `SignPayload` calculates HMAC-SHA256 over `timestamp.payload` using the secret key, formatting it as `t=<timestamp>,v1=<hex>`.
3. Secret resolution logic checks `sub.Secret` first and falls back to `ws.WebhookSecret`.
4. HTTP POST headers are updated with `req.Header.Set("X-PerGo-Signature", signature)`.
5. Database schema migration `035_add_webhook_secret_to_workspaces.sql` adds `webhook_secret VARCHAR(128)` to `workspaces`.
6. Unit tests in `internal/webhook/dispatcher_test.go` cover signature header generation and workspace fallback, all passing with zero errors.

## 3. Caveats

- `go test -count=1 ./internal/repository -v -run TestWorkspace` returns `no tests to run` because workspace repository operations are tested within DB integration tests requiring a live PostgreSQL instance (`PERGO_DATABASE_URL`). Unit tests in `./internal/webhook/...` execute with mock stores in-memory and pass 100%.

## 4. Conclusion

Milestone 3 (Issue #43 - Outbound Webhook HMAC-SHA256 Signature) is complete, fully implemented, verified, and test-supported.

## 5. Verification Method

Run the following command to independently verify:
```bash
export PATH=$PATH:/home/pablodiegoo/.local/go/bin
go test -count=1 -v ./internal/webhook
```
Inspect lines 166–182 and 260–267 in `internal/webhook/dispatcher.go`, and line 20 in `internal/repository/workspace.go`.
