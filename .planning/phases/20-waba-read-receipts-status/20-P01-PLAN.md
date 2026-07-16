---
phase: "20-waba-read-receipts-status"
plan: "20-P01"
subsystem: "database-and-channels"
requirements: ["STAT-01", "STAT-02", "STAT-03", "STAT-04"]
must-haves:
  - "Postgres schema migration 025_add_provider_message_id_to_dispatches.sql compiles and applies cleanly Up and Down."
  - "MessageDispatchRepository methods UpdateProviderMessageID and GetByProviderMessageID are implemented and fully unit tested."
  - "WABAAdapter Dispatch method parses Meta's HTTP 200 response to extract the first messages[0].id (wamid) and returns it."
  - "DispatchOrchestrator Process updates the provider message ID in the database dispatch record upon successful whatsapp_cloud dispatch."
  - "WABAInboundAdapter Parse parses the statuses payload webhook structure and yields InboundEvent records with Metadata['type'] = 'status_update'."
---

# Plan 20-P01: Schema & Backend Foundations

## <objective>
Establish database schema changes and repository updates to map external provider message IDs (wamids) to local message dispatches. Refactor the outbound WABA channel adapter to parse and extract the external message ID from Meta's API responses, and integrate this with the dispatch orchestrator. Update the inbound WABA adapter to parse status updates (sent, delivered, read) from Meta's webhook payload and translate them into unified InboundEvents.
</objective>

## <tasks>

<task>
<id>20-01-01</id>
<objective>Create the database schema migration to add provider_message_id to message_dispatches.</objective>
<read_first>
- .planning/phases/20-waba-read-receipts-status/20-RESEARCH.md
- internal/platform/postgres/migrations/007_create_message_dispatches.sql
</read_first>
<action>
- Create a goose migration file `internal/platform/postgres/migrations/025_add_provider_message_id_to_dispatches.sql`.
- In `+goose Up`:
  - Run `ALTER TABLE message_dispatches ADD COLUMN provider_message_id VARCHAR(255) UNIQUE;`
  - Run `CREATE INDEX IF NOT EXISTS idx_message_dispatches_provider_message_id ON message_dispatches(provider_message_id) WHERE provider_message_id IS NOT NULL;`
- In `+goose Down`:
  - Run `DROP INDEX IF EXISTS idx_message_dispatches_provider_message_id;`
  - Run `ALTER TABLE message_dispatches DROP COLUMN IF EXISTS provider_message_id;`
- Verification command:
  ```bash
  go test -v ./internal/repository -run TestMessageDispatchRepository
  ```
</action>
<acceptance_criteria>
- The migration file is created at the correct path.
- The repository tests run successfully with the new migration applied, proving the SQL is correct.
- Verification command yields 0 errors.
</acceptance_criteria>
</task>

<task>
<id>20-01-02</id>
<objective>Implement UpdateProviderMessageID and GetByProviderMessageID methods in MessageDispatchRepository.</objective>
<read_first>
- internal/repository/dispatch.go
- .planning/phases/20-waba-read-receipts-status/20-RESEARCH.md
</read_first>
<action>
- Edit `internal/repository/dispatch.go` to add `UpdateProviderMessageID` and `GetByProviderMessageID` functions to `MessageDispatchRepository`:
  - `UpdateProviderMessageID` takes `ctx context.Context, id uuid.UUID, providerMessageID string` and updates the row setting `provider_message_id = $1` and `updated_at = now()`.
  - `GetByProviderMessageID` takes `ctx context.Context, providerMessageID string` and queries the database for the matching `MessageDispatch` record, scanning all columns. Return `ErrDispatchNotFound` if no row matches.
- Verification command:
  ```bash
  go build ./internal/repository/...
  ```
</action>
<acceptance_criteria>
- The code in `internal/repository/dispatch.go` compiles successfully.
- Methods `UpdateProviderMessageID` and `GetByProviderMessageID` are exposed on `MessageDispatchRepository`.
</acceptance_criteria>
</task>

<task>
<id>20-01-03</id>
<objective>Write unit tests for the MessageDispatchRepository updates.</objective>
<read_first>
- internal/repository/dispatch_test.go
- internal/repository/dispatch.go
</read_first>
<action>
- Edit `internal/repository/dispatch_test.go` to add a test function `TestMessageDispatchProviderMessageID`:
  - Create a new dispatch record using `GetOrCreateDispatch`.
  - Call `UpdateProviderMessageID` to associate it with a test provider message ID (e.g., `wamid.test12345`).
  - Retrieve the record using `GetByProviderMessageID` and assert that all fields match, especially `provider_message_id`.
  - Call `GetByProviderMessageID` with a non-existent ID and assert it returns `ErrDispatchNotFound`.
- Verification command:
  ```bash
  go test -v ./internal/repository -run TestMessageDispatchProviderMessageID
  ```
</action>
<acceptance_criteria>
- The unit test `TestMessageDispatchProviderMessageID` compiles and passes.
- Verification command yields 0 errors.
</acceptance_criteria>
</task>

<task>
<id>20-01-04</id>
<objective>Refactor WABAAdapter Dispatch to extract wamid from Meta's API response.</objective>
<read_first>
- internal/channel/whatsapp/waba.go
- .planning/phases/20-waba-read-receipts-status/20-RESEARCH.md
</read_first>
<action>
- Edit `internal/channel/whatsapp/waba.go` `sendRequest` function:
  - On a successful HTTP request, parse the JSON response body to extract the first message ID inside `messages[0].id`.
  - If successfully parsed, return the extracted `id` string (the `wamid`) as the response string.
  - Otherwise, fallback to returning the raw response body string.
- Verification command:
  ```bash
  go build ./internal/channel/whatsapp/...
  ```
</action>
<acceptance_criteria>
- The code in `internal/channel/whatsapp/waba.go` compiles successfully.
- `Dispatch` method on `WABAAdapter` returns the extracted `wamid` on successful Meta response.
</acceptance_criteria>
</task>

<task>
<id>20-01-05</id>
<objective>Update WABAAdapter tests to verify wamid extraction.</objective>
<read_first>
- internal/channel/whatsapp/waba_test.go
- internal/channel/whatsapp/waba.go
</read_first>
<action>
- Add test assertions in `internal/channel/whatsapp/waba_test.go` to verify that when Meta returns a successful payload containing a message ID (e.g. `{"messaging_product":"whatsapp","contacts":[{"input":"...","wa_id":"..."}],"messages":[{"id":"wamid.test_id_999"}]}`):
  - The string returned by `Dispatch` is exactly the extracted `wamid` (`wamid.test_id_999`).
- Verification command:
  ```bash
  go test -v ./internal/channel/whatsapp -run TestWABADispatch
  ```
</action>
<acceptance_criteria>
- The updated unit tests pass.
- Verification command yields 0 errors.
</acceptance_criteria>
</task>

<task>
<id>20-01-06</id>
<objective>Refactor DispatchOrchestrator to persist wamid to the database dispatch record on success.</objective>
<read_first>
- internal/platform/queue/orchestrator.go
- .planning/phases/20-waba-read-receipts-status/20-RESEARCH.md
</read_first>
<action>
- Edit `internal/platform/queue/orchestrator.go` inside the `Process` fallback loop:
  - If `channelName == "whatsapp_cloud"` and `dispatchToChannel` succeeds with a non-empty `respStr` (which contains the `wamid`), call `o.dispatchRepo.UpdateProviderMessageID(ctx, dispatch.ID, respStr)`.
  - Wrap the call in an error log statement to log if the repository update fails but continue execution since the message was already sent.
- Verification command:
  ```bash
  go build ./internal/platform/queue/...
  ```
</action>
<acceptance_criteria>
- The orchestrator package compiles cleanly.
- Execution logic successfully attempts to store the `wamid` when the target channel is `whatsapp_cloud`.
</acceptance_criteria>
</task>

<task>
<id>20-01-07</id>
<objective>Refactor WABAInboundAdapter to parse statuses payload and map to InboundEvents.</objective>
<read_first>
- internal/channel/whatsapp/waba_inbound.go
- .planning/phases/20-waba-read-receipts-status/20-RESEARCH.md
</read_first>
<action>
- Edit `internal/channel/whatsapp/waba_inbound.go`:
  - Update `ValueData` struct to type the `Statuses` slice rather than using `[]any`. Define fields:
    ```go
    Statuses []struct {
        ID           string `json:"id"`
        Status       string `json:"status"` // "sent", "delivered", "read", "failed"
        RecipientID  string `json:"recipient_id"`
        Timestamp    string `json:"timestamp"`
    } `json:"statuses,omitempty"`
    ```
  - In `Parse`, check if `len(change.Value.Statuses) > 0`. If so:
    - Do not skip, but iterate over `change.Value.Statuses`.
    - For each status, append a new `inbound.InboundEvent` to `events`:
      - `WorkspaceID`: `conn.WorkspaceID`
      - `MessageID`: `status.ID`
      - `Channel`: `"whatsapp_cloud"`
      - `From`: `status.RecipientID`
      - `To`: `change.Value.Metadata.DisplayPhoneNumber`
      - `Body`: `status.Status`
      - `Metadata`: `map[string]string{"type": "status_update"}`
- Verification command:
  ```bash
  go test -v ./internal/channel/whatsapp -run TestWABAInboundAdapter
  ```
</action>
<acceptance_criteria>
- `internal/channel/whatsapp/waba_inbound.go` compiles cleanly.
- Webhook status payloads parse correctly without being ignored.
- Verification command compiles and passes.
</acceptance_criteria>
</task>

<task>
<id>20-01-08</id>
<objective>Write unit tests for the WABAInboundAdapter status parsing.</objective>
<read_first>
- internal/channel/whatsapp/waba_inbound.go
- .planning/phases/20-waba-read-receipts-status/20-RESEARCH.md
</read_first>
<action>
- Add unit tests in `internal/channel/whatsapp/waba_test.go` that feeds `Parse` with webhook JSON payloads containing `statuses`:
  - Assert that status updates for sent, delivered, and read yield appropriate `InboundEvent` objects with `Metadata["type"] = "status_update"`.
- Verification command:
  ```bash
  go test -v ./internal/channel/whatsapp -run TestWABAInbound
  ```
</action>
<acceptance_criteria>
- The new unit tests compile and run.
- Verification command succeeds.
</acceptance_criteria>
</task>

</tasks>

## Artifacts

The following artifacts are produced/modified by this wave:
- [025_add_provider_message_id_to_dispatches.sql](file:///home/pablo/Coding/OmniGo/internal/platform/postgres/migrations/025_add_provider_message_id_to_dispatches.sql) (Created)
- [dispatch.go](file:///home/pablo/Coding/OmniGo/internal/repository/dispatch.go) (Modified)
- [dispatch_test.go](file:///home/pablo/Coding/OmniGo/internal/repository/dispatch_test.go) (Modified)
- [waba.go](file:///home/pablo/Coding/OmniGo/internal/channel/whatsapp/waba.go) (Modified)
- [waba_test.go](file:///home/pablo/Coding/OmniGo/internal/channel/whatsapp/waba_test.go) (Modified)
- [orchestrator.go](file:///home/pablo/Coding/OmniGo/internal/platform/queue/orchestrator.go) (Modified)
- [waba_inbound.go](file:///home/pablo/Coding/OmniGo/internal/channel/whatsapp/waba_inbound.go) (Modified)
