---
phase: "20-waba-read-receipts-status"
plan: "20-P02"
subsystem: "inbound-and-ui"
requirements: ["STAT-01", "STAT-02", "STAT-03", "STAT-04"]
depends-on:
  - "20-P01"
must-haves:
  - "InboundProcessor processes status_update events by retrieving/updating message_dispatches, bypassing standard contact resolution and audit logging."
  - "InboundProcessor publishes status update events to NATS on subject messages.status_updated."
  - "AuditRepository ListThreadByContact selects md.status using LEFT JOIN message_dispatches."
  - "templates/components/message_bubble.templ renders checkmarks (single check, double check, blue double check, or red error) next to timestamps based on status."
  - "Integration tests in cmd/pergo/waba_status_receipts_test.go verify end-to-end webhook receipt, DB status update, and thread rendering."
---

# Plan 20-P02: Inbound Processing & Inbox UI

## <objective>
Integrate WABA read receipts and status updates end-to-end. Refactor the inbound processor to intercept status update events, locate their corresponding dispatch records, update the dispatch status, and publish real-time events to NATS while bypassing contact resolution and normal message thread creation. Update the conversation audit repository to join dispatch statuses on thread queries. Finally, update the chat inbox templates to display custom visual indicators next to outbound message timestamps. Verify end-to-end flows with integration tests.
</objective>

## <tasks>

<task>
<id>20-02-01</id>
<objective>Refactor InboundProcessor to handle status_update events, update database status, and bypass contact resolution.</objective>
<read_first>
- internal/inbound/processor.go
- cmd/pergo/main.go
- cmd/pergo/admin_contact_merge_test.go
- internal/api/handler/telegram_webhook_test.go
- internal/api/handler/waba_webhook_test.go
- internal/inbound/processor_test.go
- .planning/phases/20-waba-read-receipts-status/20-RESEARCH.md
</read_first>
<action>
- Edit `internal/inbound/processor.go`:
  - Add `dispatchRepo *repository.MessageDispatchRepository` to the `InboundProcessor` struct.
  - Update `NewInboundProcessor` signature and calls to accept `dispatchRepo`.
  - In `Process`, add an early check at the top of the function:
    - If `ev.Metadata["type"] == "status_update"`:
      - Call `p.dispatchRepo.GetByProviderMessageID(ctx, ev.MessageID)`.
      - If error is `repository.ErrDispatchNotFound`, log a warning and return `nil`.
      - If another error occurs, return it.
      - Call `p.dispatchRepo.UpdateDispatchStatus(ctx, dispatch.ID, ev.Body, dispatch.CurrentChannel, dispatch.FallbackIndex, nil)` to update status (to `sent`, `delivered`, `read`, or `failed`).
      - Marshal and publish a status update payload to NATS subject `messages.status_updated` using `p.publisher.Publish`.
      - Return `nil` directly, bypassing contact resolution, sessions, deduplication, S3 upload, and normal audit logging.
- Update constructor calls in:
  - `cmd/pergo/main.go`
  - `cmd/pergo/admin_contact_merge_test.go`
  - `internal/api/handler/telegram_webhook_test.go`
  - `internal/api/handler/waba_webhook_test.go`
  - `internal/inbound/processor_test.go`
- Verification command:
  ```bash
  go build ./cmd/pergo/...
  ```
</action>
<acceptance_criteria>
- The codebase compiles successfully.
- All constructor references to `NewInboundProcessor` are updated and compile cleanly.
</acceptance_criteria>
</task>

<task>
<id>20-02-02</id>
<objective>Write unit tests for InboundProcessor status update logic.</objective>
<read_first>
- internal/inbound/processor_test.go
- internal/inbound/processor.go
</read_first>
<action>
- Add unit tests in `internal/inbound/processor_test.go` (e.g. `TestProcess_StatusUpdate`):
  - Mock `MessageDispatchRepository` or use test pool.
  - Feed an inbound event with `Metadata["type"] = "status_update"`.
  - Verify that the dispatch record is updated in the database.
  - Verify that contact resolution is not executed (mock is not called).
  - Verify the status update event is published to NATS subject `messages.status_updated`.
- Verification command:
  ```bash
  go test -v ./internal/inbound -run TestProcess_StatusUpdate
  ```
</action>
<acceptance_criteria>
- The unit test `TestProcess_StatusUpdate` compiles and runs successfully.
- Verification command yields 0 errors.
</acceptance_criteria>
</task>

<task>
<id>20-02-03</id>
<objective>Update AuditRepository to join message_dispatches in ListThreadByContact and return dispatch status.</objective>
<read_first>
- internal/repository/audit.go
- .planning/phases/20-waba-read-receipts-status/20-RESEARCH.md
</read_first>
<action>
- Edit `internal/repository/audit.go`:
  - Add `Status *string` to the `ThreadMessage` struct definition.
  - Update `ListThreadByContact` query:
    - Select `NULL::VARCHAR AS status` in the first half of the UNION ALL query (inbound).
    - Select `md.status AS status` in the second half of the UNION ALL query (outbound).
    - Add `LEFT JOIN message_dispatches md ON md.trace_id = al.trace_id` to the second half of the query.
    - Update `Scan(&m.ID, &m.TraceID, &m.Direction, &m.Body, &m.CreatedAt)` to include scanning `&m.Status`.
- Verification command:
  ```bash
  go test -v ./internal/repository -run TestAudit
  ```
</action>
<acceptance_criteria>
- `internal/repository/audit.go` compiles successfully.
- The `ListThreadByContact` repository tests verify that the dispatch status is correctly returned for outbound messages.
- Verification command yields 0 errors.
</acceptance_criteria>
</task>

<task>
<id>20-02-04</id>
<objective>Update UI templates to render checkmark icons next to timestamps based on status.</objective>
<read_first>
- templates/components/message_bubble.templ
- .planning/phases/20-waba-read-receipts-status/20-RESEARCH.md
</read_first>
<action>
- Edit `templates/components/message_bubble.templ`:
  - Locate the outbound message bubble layout block.
  - Replace the static double checkmark SVG icons with dynamic rendering:
    - Switch/if-else checks on `m.Status`:
      - If `status == "sent"`: render a single checkmark SVG (`text-blue-200` or `text-gray-300`).
      - If `status == "delivered"`: render double checkmark SVGs (`text-blue-200` or `text-gray-300`).
      - If `status == "read"`: render double checkmark SVGs (`text-blue-100` or `text-cyan-200` to indicate read receipt).
      - If `status == "failed"`: render a red warning icon/text (`⚠️` or red text).
      - Else (e.g. nil/queued/sending): render a single checkmark or empty space.
- Run code generation for the templates:
  ```bash
  make generate
  ```
- Verification command:
  ```bash
  go build ./templates/components/...
  ```
</action>
<acceptance_criteria>
- Template compilation with `make generate` completes successfully without syntax errors.
- Visual components compilation completes cleanly.
</acceptance_criteria>
</task>

<task>
<id>20-02-05</id>
<objective>Write integration tests in cmd/pergo/waba_status_receipts_test.go.</objective>
<read_first>
- cmd/pergo/audit_test.go
- internal/api/handler/waba_webhook_test.go
- .planning/phases/20-waba-read-receipts-status/20-RESEARCH.md
</read_first>
<action>
- Create `cmd/pergo/waba_status_receipts_test.go`.
- Implement `TestWABAStatusReceiptsEndToEnd`:
  - Connect to test DB pool and NATS.
  - Setup a Workspace, Connection (with WABA credentials), and Contact.
  - Create a mock message dispatch in the database with `provider_message_id = "wamid.test_status_123"`.
  - Simulate incoming status webhooks by sending HTTP POST requests to `/webhooks/waba/:workspace_id` with statuses payload:
    - Update `wamid.test_status_123` to `delivered`, verify database contains `delivered`.
    - Update `wamid.test_status_123` to `read`, verify database contains `read`.
  - Call `AuditRepository.ListThreadByContact` and verify the returned thread contains the outbound message with status `read`.
- Verification command:
  ```bash
  go test -v ./cmd/pergo -run TestWABAStatusReceiptsEndToEnd
  ```
</action>
<acceptance_criteria>
- The integration test files compile and pass successfully.
- Verification command yields 0 errors.
</acceptance_criteria>
</task>

<task>
<id>20-02-06</id>
<objective>Verify all Phase 20 tasks build and pass tests cleanly.</objective>
<read_first>
- None
</read_first>
<action>
- Run all project tests to ensure everything is verified:
  ```bash
  go test ./...
  ```
</action>
<acceptance_criteria>
- All unit and integration tests compile and run successfully across the entire codebase.
</acceptance_criteria>
</task>

</tasks>

## Artifacts

The following artifacts are produced/modified by this wave:
- [processor.go](file:///home/pablo/Coding/OmniGo/internal/inbound/processor.go) (Modified)
- [processor_test.go](file:///home/pablo/Coding/OmniGo/internal/inbound/processor_test.go) (Modified)
- [audit.go](file:///home/pablo/Coding/OmniGo/internal/repository/audit.go) (Modified)
- [message_bubble.templ](file:///home/pablo/Coding/OmniGo/templates/components/message_bubble.templ) (Modified)
- [waba_status_receipts_test.go](file:///home/pablo/Coding/OmniGo/cmd/pergo/waba_status_receipts_test.go) (Created)
- [main.go](file:///home/pablo/Coding/OmniGo/cmd/pergo/main.go) (Modified)
- [admin_contact_merge_test.go](file:///home/pablo/Coding/OmniGo/cmd/pergo/admin_contact_merge_test.go) (Modified)
- [telegram_webhook_test.go](file:///home/pablo/Coding/OmniGo/internal/api/handler/telegram_webhook_test.go) (Modified)
- [waba_webhook_test.go](file:///home/pablo/Coding/OmniGo/internal/api/handler/waba_webhook_test.go) (Modified)
