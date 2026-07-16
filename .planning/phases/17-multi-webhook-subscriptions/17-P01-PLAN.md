---
phase: "17"
plan: "17-P01"
type: "standard"
wave: 1
depends_on: []
files_modified:
  - "internal/platform/postgres/migrations/023_create_webhook_subscriptions.sql"
  - "internal/repository/webhook_subscription.go"
  - "internal/repository/webhook_subscription_test.go"
  - "internal/repository/webhook_dlq.go"
  - "internal/repository/webhook_dlq_test.go"
  - "internal/webhook/wildcard.go"
  - "internal/webhook/wildcard_test.go"
  - "internal/webhook/dispatcher.go"
  - "internal/webhook/dispatcher_test.go"
  - "internal/platform/queue/jetstream.go"
  - "cmd/pergo/admin_webhook_dlq_test.go"
autonomous: true
requirements: ["SUBS-01", "SUBS-02", "SUBS-03", "SUBS-04"]
---

# Phase 17, Plan P01: Schema & Broker Foundations

## <objective>
Establish the database schema, repositories, event matching utilities, and NATS JetStream stream definitions to support multi-webhook subscriptions. This plan sets up the database structures, repository access layers, wildcard filtering mechanism, and broker streams while maintaining a temporary compatibility layer so that the project compiles and passes all existing tests.
</objective>

## <tasks>
<task>
<id>17-01-01</id>
<action>Create database migration file `internal/platform/postgres/migrations/023_create_webhook_subscriptions.sql`. This migration must:
1. Create the `webhook_subscriptions` table with columns: `id`, `workspace_id`, `url`, `secret`, `key_id`, `key_version`, `event_types` (`text[]`), `active` (`boolean`), `created_at`, `updated_at`.
2. Add index on `workspace_id`.
3. Migrate existing configurations from `webhook_configs` to `webhook_subscriptions` mapping `event_types` to the wildcard `*`.
4. Add column `subscription_id` (UUID) to `webhook_dlqs` table.
5. Populate `subscription_id` in `webhook_dlqs` by linking `webhook_dlqs` with `webhook_subscriptions` on `workspace_id`.
6. Delete orphan DLQ items and alter `subscription_id` to be `NOT NULL` with a foreign key referencing `webhook_subscriptions(id) ON DELETE CASCADE`.
7. Drop the legacy `webhook_configs` table.
8. Implement the Down migration to reverse all these steps.</action>
<read_first>
- .planning/phases/17-multi-webhook-subscriptions/17-RESEARCH.md
- internal/platform/postgres/migrations/008_create_webhooks_and_dlq.sql
</read_first>
<acceptance_criteria>
- The migration compiles and executes up and down cleanly against the database.
- Verification using the DB query tools confirms that `webhook_subscriptions` exists and `webhook_dlqs` references `subscription_id` with a foreign key constraint.
</acceptance_criteria>
</task>

<task>
<id>17-01-02</id>
<action>Create the webhook subscription repository in `internal/repository/webhook_subscription.go`. Implement:
1. Struct `WebhookSubscription` with decrypted plaintext `Secret` field.
2. Methods: `Create`, `Get`, `ListByWorkspace`, `Update`, `Delete`.
3. AES-256-GCM envelope encryption and decryption on the subscription's secret during inserts/updates and fetches using `CredentialProvider` (similar to how connections and legacy configs are encrypted).</action>
<read_first>
- .planning/phases/17-multi-webhook-subscriptions/17-RESEARCH.md
- internal/repository/credential_provider.go
</read_first>
<acceptance_criteria>
- The code compiles without errors.
- Method signatures match the research requirements.
</acceptance_criteria>
</task>

<task>
<id>17-01-03</id>
<action>Update the dead-letter queue repository in `internal/repository/webhook_dlq.go` to support subscription-based DLQs:
1. Add `SubscriptionID uuid.UUID` field to `WebhookDLQ` struct.
2. Modify `InsertDLQ` function signature to accept `subscriptionID uuid.UUID` as the third parameter, and save it in the database query.
3. Update `ListDLQ`, `ListAllDLQ`, and `GetDLQByID` queries to read and scan the `subscription_id` column.
4. Refactor legacy config functions (`GetConfig`, `SaveConfig`, `DeleteConfig`) to query/update the new `webhook_subscriptions` table for compatibility, treating the first active subscription for the workspace as the fallback configuration. This keeps the application fully functional during this wave.</action>
<read_first>
- internal/repository/webhook_dlq.go
- .planning/phases/17-multi-webhook-subscriptions/17-RESEARCH.md
</read_first>
<acceptance_criteria>
- The code compiles without compile errors.
- The fallback `GetConfig`, `SaveConfig`, and `DeleteConfig` emulate a single config using the first subscription correctly.
</acceptance_criteria>
</task>

<task>
<id>17-01-04</id>
<action>Update/implement repository tests:
1. Create `internal/repository/webhook_subscription_test.go` to test subscription CRUD and encryption.
2. Update `internal/repository/webhook_dlq_test.go` to pass a valid `subscription_id` to `InsertDLQ` and verify the retrieved DLQ item contains the correct subscription reference.</action>
<read_first>
- internal/repository/webhook_dlq_test.go
</read_first>
<acceptance_criteria>
- Running `go test -v ./internal/repository/...` passes with zero failures.
</acceptance_criteria>
</task>

<task>
<id>17-01-05</id>
<action>Implement wildcard matching utilities for event types:
1. Create `internal/webhook/wildcard.go` with functions:
   - `MatchEvent(pattern, eventName string) bool`
   - `MatchesAny(patterns []string, eventName string) bool`
   Both functions must utilize Go's `path.Match` standard library.
2. Create `internal/webhook/wildcard_test.go` verifying the glob matching functionality (e.g. `*` matches any event, `message.*` matches `message.sent`, etc.).</action>
<read_first>
- .planning/phases/17-multi-webhook-subscriptions/17-RESEARCH.md
</read_first>
<acceptance_criteria>
- Running `go test -v ./internal/webhook/...` passes with wildcard matching verified.
</acceptance_criteria>
</task>

<task>
<id>17-01-06</id>
<action>Refactor NATS stream setup in `internal/platform/queue/jetstream.go` and dispatcher interfaces:
1. Update `EnsureWebhookStream` to define subjects as `[]string{"webhooks.events"}` instead of the overlapping wildcard `webhooks.>`.
2. Implement `EnsureWebhookDeliveryStream` to configure the new `WEBHOOK_DELIVERIES` stream listening on `webhooks.deliveries.>` with `WorkQueuePolicy` retention, file storage, and safe message discard settings.
3. Update `ConfigStore` interface in `internal/webhook/dispatcher.go` and mock implementation in `internal/webhook/dispatcher_test.go` to match the updated `InsertDLQ` signature. Update caller in `internal/webhook/dispatcher.go` to fetch the subscription ID from the configuration record and pass it to `InsertDLQ`.</action>
<read_first>
- internal/platform/queue/jetstream.go
- internal/webhook/dispatcher.go
- internal/webhook/dispatcher_test.go
</read_first>
<acceptance_criteria>
- Code compiles without error.
- All existing tests in `internal/webhook/` and `internal/platform/queue/` compile and pass.
</acceptance_criteria>
</task>

<task>
<id>17-01-07</id>
<action>Update integration test references to `InsertDLQ` in other modules:
1. Update `cmd/pergo/admin_webhook_dlq_test.go` to pass a valid subscription ID or dummy UUID when calling `InsertDLQ`.</action>
<read_first>
- cmd/pergo/admin_webhook_dlq_test.go
</read_first>
<acceptance_criteria>
- Running `go test -v ./cmd/pergo/...` compiles and passes.
</acceptance_criteria>
</task>
</tasks>

## <verification>
Execute the following verification steps:
1. Run all unit and integration tests:
   ```bash
   go test -v ./internal/repository/...
   go test -v ./internal/webhook/...
   go test -v ./internal/platform/queue/...
   go test -v ./cmd/pergo/...
   ```
2. Verify that NATS streams are initialized correctly by starting the server or running integration tests, confirming that no subject overlap errors occur.
</verification>

## <success_criteria>
1. The new migration successfully creates the `webhook_subscriptions` table and updates `webhook_dlqs` with a foreign key.
2. Webhook subscription configuration is successfully encrypted at rest and decrypted upon retrieval.
3. In-memory wildcard glob matching logic for events functions correctly and is fully unit-tested.
4. Separate `WEBHOOKS` and `WEBHOOK_DELIVERIES` streams are defined in NATS JetStream without overlapping subjects.
</success_criteria>

## Artifacts this phase produces
- `internal/platform/postgres/migrations/023_create_webhook_subscriptions.sql` (Database migration script)
- `internal/repository/webhook_subscription.go` (Repository logic)
- `internal/repository/webhook_subscription_test.go` (Repository tests)
- `internal/webhook/wildcard.go` (Glob pattern matching utilities)
- `internal/webhook/wildcard_test.go` (Glob matching unit tests)
