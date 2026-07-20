---
wave: 1
depends_on: []
files_modified:
  - internal/integration/typebot/forwarder.go
  - internal/integration/typebot/forwarder_test.go
autonomous: true
---

# Phase 24.2: Close gap TYPE-04 (Populate ConnectionID, SenderIdentity, and TraceID)

## Tasks

Covers decisions: D-01, D-02, D-03
```xml
<task>
  <action>
    Modify `internal/integration/typebot/forwarder.go` in `SyncInboundMessage` to populate `ConnectionID`, `SenderIdentity`, and `TraceID` when constructing the outbound `domain.QueueMessage`. 
    - Set `outMsg.ConnectionID = event.ConnectionID`.
    - Set `outMsg.SenderIdentity = event.To`.
    - Generate `traceID := uuid.New().String()` before constructing `outMsg` (move it up outside the struct initialization).
    - Set `outMsg.TraceID = traceID`.
    - Pass this exact `traceID` to `f.publisher.Publish(ctx, "messages.outbound", b, traceID)`.
  </action>
  <read_first>
    - internal/integration/typebot/forwarder.go
    - .planning/phases/24.2-close-gap-type-04-populate-connectionid-senderidentity-and-t/24.2-RESEARCH.md
  </read_first>
  <acceptance_criteria>
    - `internal/integration/typebot/forwarder.go` inside `SyncInboundMessage` assigns `ConnectionID: event.ConnectionID`, `SenderIdentity: event.To`, and `TraceID: traceID` to `outMsg`.
    - `go build ./...` succeeds without errors.
  </acceptance_criteria>
</task>

<task>
  <action>
    Update `internal/integration/typebot/forwarder_test.go` to verify the routing fields are populated correctly.
    - Update the `mockPublisher` struct to capture `data []byte` and `traceID string` in its `Publish` method.
    - Add a test case (e.g., `TestTypebotForwarder_PopulatesRoutingFields` or by extending an existing test if appropriate) that mocks necessary dependencies, triggers `SyncInboundMessage` so that it reaches the publishing step, unmarshals the captured `mockPublisher.data` into a `domain.QueueMessage`, and asserts that `ConnectionID`, `SenderIdentity`, and `TraceID` are correctly set from the event.
  </action>
  <read_first>
    - internal/integration/typebot/forwarder_test.go
    - .planning/phases/24.2-close-gap-type-04-populate-connectionid-senderidentity-and-t/24.2-RESEARCH.md
  </read_first>
  <acceptance_criteria>
    - `mockPublisher` in `internal/integration/typebot/forwarder_test.go` stores `data` and `traceID`.
    - A test assertion verifies `ConnectionID`, `SenderIdentity`, and `TraceID` on the published `QueueMessage`.
    - `go test ./internal/integration/typebot -run TestTypebotForwarder` passes.
  </acceptance_criteria>
</task>
```

## Verification Criteria
- `ConnectionID` and `SenderIdentity` must be mapped from `event.ConnectionID` and `event.To` into the outbound `QueueMessage`.
- A newly generated UUID `TraceID` must be included in both the `QueueMessage` payload and the `f.publisher.Publish` call.

## Must Haves
```yaml
must_haves:
  truths:
    - "D-01: QueueMessage constructed in TypebotForwarder.SyncInboundMessage contains ConnectionID populated from event.ConnectionID"
    - "D-02: QueueMessage constructed in TypebotForwarder.SyncInboundMessage contains SenderIdentity populated from event.To"
    - "D-03: QueueMessage constructed in TypebotForwarder.SyncInboundMessage contains TraceID populated with a newly generated UUID"
    - "The same TraceID string is used to populate QueueMessage.TraceID and passed to publisher.Publish"
```

## Artifacts this phase produces
- Test function `TestTypebotForwarder_PopulatesRoutingFields` (or similar new test/subtest) in `internal/integration/typebot/forwarder_test.go`.
- Modified `mockPublisher` struct in `internal/integration/typebot/forwarder_test.go` to capture published data.

Addresses decisions: D-01, D-02, D-03
