---
phase: "19-webhook-messaging-verbs-engine"
plan: "19-P02"
subsystem: "queue"
must-haves:
  - "VerbsEngine is successfully injected into DefaultDispatcher."
  - "DefaultDispatcher.Dispatch method parses 2xx webhook responses and asynchronously executes verbs."
  - "VerbsEngine reply and forward actions publish domain.QueueMessage to messages.outbound on NATS."
  - "Execution results and errors are recorded under action 'webhook.verbs' in user action logs."
  - "DefaultDispatcher is thoroughly tested with mocks, including concurrent and error handling path checks."
  - "All webhook package tests run and pass without race conditions."
---

# Plan 19-P02: Dispatcher Integration, NATS Queue Publishing & Action Logging

## <objective>
Integrate the Messaging Verbs Engine with the webhook dispatcher. Modify `DefaultDispatcher` to parse successful 2xx responses and execute verbs asynchronously. Wire `reply` and `forward` verbs to publish outbound message payloads to NATS. Wire verb execution outcomes to persist audit entries to User Action Logs. Verify the integrated flow with mock integration tests and race-detector checks.
</objective>

## <tasks>

<task>
<id>19-02-01</id>
<objective>Inject VerbsEngine into DefaultDispatcher structure and constructor.</objective>
<read_first>
- internal/webhook/dispatcher.go
- .planning/phases/19-webhook-messaging-verbs-engine/19-RESEARCH.md
</read_first>
<files_modified>
- internal/webhook/dispatcher.go
</files_modified>
<implementation>
- Edit `internal/webhook/dispatcher.go` to add `verbsEngine *VerbsEngine` as a private field in `DefaultDispatcher`.
- Modify `NewDefaultDispatcher` to accept `verbsEngine *VerbsEngine` as the fifth parameter:
  ```go
  func NewDefaultDispatcher(
      subStore SubscriptionStore,
      dlqStore DLQStore,
      wsStore WorkspaceStore,
      client HTTPClient,
      verbsEngine *VerbsEngine,
  ) *DefaultDispatcher { ... }
  ```
- Compile the package to verify syntax:
  ```bash
  go build ./internal/webhook/...
  ```
</implementation>
</task>

<task>
<id>19-02-02</id>
<objective>Update DefaultDispatcher.Dispatch to parse response verbs and execute them asynchronously.</objective>
<read_first>
- internal/webhook/dispatcher.go
- .planning/phases/19-webhook-messaging-verbs-engine/19-RESEARCH.md
</read_first>
<files_modified>
- internal/webhook/dispatcher.go
</files_modified>
<implementation>
- Edit `Dispatch(ctx context.Context, task WebhookDeliveryTask) error` in `internal/webhook/dispatcher.go`.
- After successful response (2xx code, line ~168):
  - Read all bytes from `resp.Body` using `io.ReadAll`.
  - Attempt to unmarshal `bodyBytes` into a `[]Verb`.
  - If unmarshalling succeeds and there are verbs:
    - Spawn a new goroutine using `context.Background()` to decouple execution from the HTTP dispatch context.
    - Inside the goroutine, invoke `d.verbsEngine.Execute(execCtx, task, verbs)`.
    - Log any execution errors using `slog.Error`.
- Ensure imports like `"io"` and `"log/slog"` are present.
- Compile:
  ```bash
  go build ./internal/webhook/...
  ```
</implementation>
</task>

<task>
<id>19-02-03</id>
<objective>Implement NATS publishing for reply and forward verbs in VerbsEngine.</objective>
<read_first>
- internal/webhook/verbs.go
- .planning/phases/19-webhook-messaging-verbs-engine/19-RESEARCH.md
- internal/outbound/processor.go
</read_first>
<files_modified>
- internal/webhook/verbs.go
</files_modified>
<implementation>
- Edit `internal/webhook/verbs.go` to implement `executeReply` and `executeForward` methods:
  - In `executeReply`:
    - Resolve the connection using `e.resolver.GetBySenderIdentity`. If it returns an error, fallback using `e.resolver.GetDefaultChannelConnection` with the task's channel.
    - Construct `domain.QueueMessage` with resolved workspace ID, connection ID, sender identity, trace ID, recipient identity (`evt.From`), channel, body, and current UTC time.
    - Marshal the queue message to JSON and publish to `"messages.outbound"` subject via `e.publisher.Publish`.
  - In `executeForward`:
    - Resolve connection for target channel using `e.resolver.GetDefaultChannelConnection`.
    - Construct `domain.QueueMessage` pointing to target recipient (`to`), target channel, body (`originalBody`), and current UTC time.
    - Marshal and publish to `"messages.outbound"` subject.
- Compile:
  ```bash
  go build ./internal/webhook/...
  ```
</implementation>
</task>

<task>
<id>19-02-04</id>
<objective>Implement User Action Logging in VerbsEngine.</objective>
<read_first>
- internal/webhook/verbs.go
- .planning/phases/19-webhook-messaging-verbs-engine/19-RESEARCH.md
- internal/repository/user_action_log.go
</read_first>
<files_modified>
- internal/webhook/verbs.go
</files_modified>
<implementation>
- Edit `internal/webhook/verbs.go` to implement `logActionResults` method.
- Construct `ExecutedVerbLog` array representing status and errors of each verb.
- Construct `VerbsExecutionMetadata` containing trace ID, subscription ID, contact ID, channel, sender, and the verbs status log.
- Marshal metadata to JSON.
- Construct a `repository.UserActionLog` entry:
  - `WorkspaceID`: wsID
  - `ActorType`: `"system"`
  - `ActorID`: `"verbs_engine"`
  - `ActorName`: `"Webhook Verbs Engine"`
  - `Action`: `"webhook.verbs"`
  - `Source`: `"system"`
  - `Metadata`: metaBytes
- Run `e.logsRepo.Insert` inside a background goroutine (`go func() { ... }()`) using `context.Background()` to prevent blocking any worker resources.
- Compile:
  ```bash
  go build ./internal/webhook/...
  ```
</implementation>
</task>

<task>
<id>19-02-05</id>
<objective>Update main entry point and test setups with the modified NewDefaultDispatcher signature.</objective>
<read_first>
- cmd/pergo/main.go
- internal/platform/queue/webhook_worker_test.go
- internal/webhook/dispatcher_test.go
</read_first>
<files_modified>
- cmd/pergo/main.go
- internal/platform/queue/webhook_worker_test.go
- internal/webhook/dispatcher_test.go
</files_modified>
<implementation>
- Edit `cmd/pergo/main.go` to initialize `userActionLogRepo := repository.NewUserActionLogRepository(pool)`, then `verbsEngine := webhook.NewVerbsEngine(publisher, contactRepo, userActionLogRepo, connectionRepo)`, and pass `verbsEngine` as the fifth parameter to `webhook.NewDefaultDispatcher`.
- Edit `internal/platform/queue/webhook_worker_test.go` and `internal/webhook/dispatcher_test.go` calls to `NewDefaultDispatcher` to pass `nil` (or a mocked/fake verbs engine if tested) as the fifth argument.
- Compile all entry points and run tests:
  ```bash
  go build ./cmd/pergo/...
  go test ./internal/platform/queue -run TestWebhookWorker
  ```
</implementation>
</task>

<task>
<id>19-02-06</id>
<objective>Write integration and mock tests for the integrated Webhook Messaging Verbs Dispatcher flow.</objective>
<read_first>
- internal/webhook/dispatcher_test.go
- .planning/phases/19-webhook-messaging-verbs-engine/19-RESEARCH.md
</read_first>
<files_modified>
- internal/webhook/dispatcher_test.go
</files_modified>
<implementation>
- Edit `internal/webhook/dispatcher_test.go` to add tests for verbs execution inside the dispatch lifecycle:
  - Mock the HTTP client to return a 2xx response containing a JSON array of verbs.
  - Mock the publisher, route resolver, contact repository, and user action log repository.
  - Assert that dispatching a webhook task parses the HTTP response and schedules verbs execution in a background goroutine.
  - Verify that `reply` and `forward` verbs publish correct messages to NATS on `"messages.outbound"`.
  - Verify that execution outcomes (successes and errors) are logged as `"webhook.verbs"` in the action logs repository.
  - Verify that PII compliance redaction does not mutate or interfere with `task.Payload` passed to the verbs engine (meaning the unredacted payload is executed).
- Run tests:
  ```bash
  go test -v ./internal/webhook -run TestDefaultDispatcher
  ```
</implementation>
</task>

<task>
<id>19-02-07</id>
<objective>Run the full verification checks for Wave 2 with race detection.</objective>
<read_first>
- internal/webhook/dispatcher_test.go
- internal/webhook/verbs_test.go
</read_first>
<files_modified>
- None
</files_modified>
<implementation>
- Run all webhook package unit and integration tests:
  ```bash
  go test -v ./internal/webhook
  ```
- Run the full race detector test suite:
  ```bash
  go test -race -v ./internal/webhook/...
  ```
</implementation>
</task>

</tasks>
