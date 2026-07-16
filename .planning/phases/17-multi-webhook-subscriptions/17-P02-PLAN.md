---
phase: "17"
plan: "17-P02"
type: "standard"
wave: 2
depends_on: ["17-P01"]
files_modified:
  - "internal/webhook/dispatcher.go"
  - "internal/webhook/dispatcher_test.go"
  - "internal/platform/queue/webhook_worker.go"
  - "internal/platform/queue/webhook_worker_test.go"
  - "internal/api/handler/admin/webhook_dlq.go"
  - "templates/pages/webhooks.templ"
  - "templates/pages/webhooks_templ.go"
  - "cmd/pergo/main.go"
  - "cmd/pergo/admin_webhook_dlq_test.go"
  - "internal/repository/webhook_dlq.go"
  - "internal/repository/webhook_dlq_test.go"
autonomous: true
requirements: ["SUBS-01", "SUBS-02", "SUBS-03", "SUBS-04"]
---

# Phase 17, Plan P02: Dispatcher & Admin Settings UI

## <objective>
Refactor the event dispatching pipeline to support concurrent NATS fan-out, independent exponential backoffs, and per-subscription retry handling. Connect this broker pipeline to a rewritten Workspace Settings Webhooks dashboard, allowing users to configure multiple subscriptions with wildcard event matching filters. Provide a synchronous webhook simulation testing framework to preview headers, signature checks, and response body logs. Clean up any Wave 1 legacy compatibility layers.
</objective>

## <tasks>
<task>
<id>17-02-01</id>
<action>Refactor `internal/webhook/dispatcher.go` and `internal/webhook/dispatcher_test.go`:
1. Redefine or inject `WebhookSubscriptionRepository` to allow retrieving subscriptions by UUID.
2. Modify `Dispatch` method of `DefaultDispatcher` to accept `WebhookDeliveryTask` containing the target `SubscriptionID`, `Payload`, and delivery metadata.
3. In `Dispatch`, retrieve the specific subscription by `SubscriptionID`. If the subscription is inactive or missing, return a terminal error (e.g. `ErrSubscriptionInactive`) so that the worker can Ack and drop the task.
4. Perform AES decryption of the subscription secret using `CredentialProvider`.
5. Redact PII data for inbound events if the workspace has disabled `PIIOptIn`.
6. Construct the HTTP POST request to the subscription's URL with the computed `X-PerGo-Signature` signature header and `X-Trace-ID`.
7. Update `internal/webhook/dispatcher_test.go` to mock these configurations and test single task dispatching, PII compliance, and signature header presence.</action>
<read_first>
- internal/webhook/dispatcher.go
- internal/webhook/dispatcher_test.go
- .planning/phases/17-multi-webhook-subscriptions/17-RESEARCH.md
</read_first>
<acceptance_criteria>
- Code compiles.
- Running `go test -v ./internal/webhook/...` succeeds with all dispatcher tests passing.
</acceptance_criteria>
</task>

<task>
<id>17-02-02</id>
<action>Refactor `WebhookWorker` in `internal/platform/queue/webhook_worker.go` to handle fan-out and delivery execution separately:
1. Update `processEvent` or the raw handlers to run the **Fan-out phase**: Consuming a raw event from `webhooks.events` or `inbound.events.>`, look up all active subscriptions for the workspace. Use `MatchesAny` wildcard utility to check if the event matches. If matching, publish a separate `WebhookDeliveryTask` payload to subject `webhooks.deliveries.<workspace_id>.<subscription_id>` under the `WEBHOOK_DELIVERIES` stream. Once fanned out, Ack the raw event.
2. Create the **Delivery phase consumer**: Set up a durable pull consumer `webhooks-deliveries-consumer` on the `WEBHOOK_DELIVERIES` stream (`webhooks.deliveries.>`).
3. For each delivery task consumed:
   - Call `DefaultDispatcher.Dispatch` to execute the HTTP dispatch.
   - If dispatch succeeds, Ack the delivery message.
   - If dispatch fails, inspect the error: if it is a terminal HTTP error (`400`, `401`, `403`, `404`) or NATS delivery attempt metadata count has hit 10, write a record to `webhook_dlqs` linked to the subscription, log the failure, and Ack.
   - For temporary failures, calculate exponential backoff delay `2^(attempts-1) * 1s` (capped at 10 minutes) and invoke `msg.NakWithDelay(delay)`.
4. Update `internal/platform/queue/webhook_worker_test.go` to verify this fan-out behavior, retry routing, backoffs, and DLQ serialization.</action>
<read_first>
- internal/platform/queue/webhook_worker.go
- internal/platform/queue/webhook_worker_test.go
- .planning/phases/17-multi-webhook-subscriptions/17-RESEARCH.md
</read_first>
<acceptance_criteria>
- Code compiles.
- Running `go test -v ./internal/platform/queue/...` succeeds.
</acceptance_criteria>
</task>

<task>
<id>17-02-03</id>
<action>Refactor the webhooks and DLQ handlers in `internal/api/handler/admin/webhook_dlq.go`:
1. Inject `WebhookSubscriptionRepository` alongside `WebhookDLQRepository`.
2. Update the `Page` method to query and render all subscriptions for the workspace, alongside workspace DLQ logs.
3. Implement `CreateSubscription` (`POST /admin/workspaces/:workspace_id/webhooks/subscriptions`) to create a subscription (encrypting the secret).
4. Implement `UpdateSubscription` (`POST /admin/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id`) to edit URL, active status, event types checklist, and updating secret if provided.
5. Implement `DeleteSubscription` (`DELETE /admin/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id`).
6. Implement `TestSubscription` (`POST /admin/workspaces/:workspace_id/webhooks/subscriptions/:subscription_id/test`): Parse the test payload, run a synchronous HTTP request to the subscription URL with the correct HMAC signature, measure duration, and return a rendered HTML modal fragment containing the status code, latency, headers, and first 1000 characters of the response body.
7. Update `RetryDLQ` to extract the task payload, publish the delivery task directly back to NATS subject `webhooks.deliveries.<workspace_id>.<subscription_id>`, and delete the DLQ log on success.
8. Clean up legacy single-config handling code from the handler.</action>
<read_first>
- internal/api/handler/admin/webhook_dlq.go
- .planning/phases/17-multi-webhook-subscriptions/17-RESEARCH.md
</read_first>
<acceptance_criteria>
- Webhook settings handler compiles.
- HTMX routes render clean HTML fragments and send appropriate redirect/headers.
</acceptance_criteria>
</task>

<task>
<id>17-02-04</id>
<action>Refactor dashboard settings templates in `templates/pages/webhooks.templ`:
1. Update `WorkspaceWebhooksContent` to render a list of active subscriptions in a table showing URL, event filters (rendered as badges), active flag, and action buttons.
2. Design modals for adding a new subscription and editing an existing one (with checkbox fields for standard event types: `*`, `message.received`, `message.sent`, `message.failed`, `connection.status`).
3. Refactor the DLQ table to show subscription/URL details for each dead-lettered item.
4. Implement the "Test Webhook" modal layout, containing a payload selector (or editable textarea), showing the execution logs, HTTP status code, response headers, and response body snippet after submission.
5. Run `templ generate` to compile updated layouts.</action>
<read_first>
- templates/pages/webhooks.templ
</read_first>
<acceptance_criteria>
- Running `templ generate` finishes with zero syntax errors.
- Compiled `templates/pages/webhooks_templ.go` is generated.
</acceptance_criteria>
</task>

<task>
<id>17-02-05</id>
<action>Update routing configurations in `cmd/pergo/main.go` and integration tests:
1. In `cmd/pergo/main.go`, replace `/webhooks/config` legacy routes with the new `/webhooks/subscriptions` CRUD routes and testing routes.
2. In `cmd/pergo/admin_webhook_dlq_test.go`, rewrite configuration tests to match the new multi-subscription API routes.
3. Clean up the deprecated legacy config database methods (`SaveConfig`, `GetConfig`, `DeleteConfig`) from `internal/repository/webhook_dlq.go` and `internal/repository/webhook_dlq_test.go`.</action>
<read_first>
- cmd/pergo/main.go
- cmd/pergo/admin_webhook_dlq_test.go
- internal/repository/webhook_dlq.go
- internal/repository/webhook_dlq_test.go
</read_first>
<acceptance_criteria>
- Main application compiles.
- Running `go test -v ./cmd/pergo/...` succeeds with zero errors.
</acceptance_criteria>
</task>
</tasks>

## <verification>
Execute the following verification steps:
1. Re-generate all templates:
   ```bash
   templ generate
   ```
2. Compile and run all tests in the codebase to ensure no regressions:
   ```bash
   go test -v ./...
   ```
3. Start the application (`go run cmd/pergo/main.go`) and test webhook settings in the admin dashboard:
   - Verify multiple subscriptions can be created, updated, and deleted.
   - Verify that clicking "Test Webhook" fires a signed POST request synchronously and updates the UI with response body, status, and headers.
   - Verify that simulated webhooks match signature validations.
</verification>

## <success_criteria>
1. Operators can define, update, list, and delete multiple webhook subscriptions per workspace via the UI (SUBS-01).
2. Outbound and inbound events match subscriptions dynamically based on wildcard glob filters (SUBS-02).
3. The broker concurrently routes event delivery tasks to all matching subscriber endpoints using NATS fan-out (SUBS-03).
4. Deliveries handle backoffs independently, and permanently failed webhooks write detailed error logs to the subscription-linked DLQ (SUBS-04).
5. Synchronous testing displays complete HTTP roundtrip diagnostics inline.
</success_criteria>

## Artifacts this phase produces
- `internal/webhook/dispatcher.go` (Webhook delivery logic refactored for tasks)
- `internal/platform/queue/webhook_worker.go` (Refactored for NATS fan-out and worker queues)
- `internal/api/handler/admin/webhook_dlq.go` (Subscription CRUD & simulation HTTP routes)
- `templates/pages/webhooks.templ` (Settings management page templ)
- `templates/pages/webhooks_templ.go` (Generated template file)
