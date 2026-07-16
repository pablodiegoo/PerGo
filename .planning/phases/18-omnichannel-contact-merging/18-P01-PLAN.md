---
phase: "18"
plan: "18-P01"
type: "standard"
wave: 1
depends_on: []
files_modified:
  - internal/platform/postgres/migrations/024_omnichannel_contact_merging.sql
  - internal/domain/contact.go
  - internal/repository/contact.go
  - internal/repository/contact_test.go
  - internal/repository/telegram_contact.go
  - internal/repository/telegram_contact_test.go
  - internal/inbound/processor.go
  - internal/inbound/processor_test.go
  - internal/platform/queue/orchestrator.go
  - internal/platform/queue/worker_test.go
  - internal/channel/telegram/inbound.go
  - internal/api/handler/telegram_webhook.go
  - internal/api/handler/telegram_webhook_test.go
  - cmd/pergo/main.go
autonomous: true
requirements: ["CONT-01", "CONT-02", "CONT-03", "CONT-04"]
---

# Plan 18-P01: Schema & Backend Foundations

## <objective>
Establish the core data models, schema migrations, and database access repository for omnichannel contacts and identities. Refactor the inbound processing pipeline and Telegram adapters to resolve and map inbound identities concurrently. Refactor the queue orchestrator to translate Telegram identities from the unified registry. Maintain backwards-compatibility compilation by temporarily adapting `TelegramContactRepository` on top of the new contacts schema.
</objective>

## <tasks>

<task>
<id>18-01-01</id>
<action>
Create database migration file `internal/platform/postgres/migrations/024_omnichannel_contact_merging.sql`.
Define the `contacts` table (workspace_id reference, name, email, timestamps).
Define the `contact_identities` table with a composite UNIQUE constraint `UNIQUE (workspace_id, channel, sender_identity)` and appropriate indexes.
Implement a `DO` block to migrate existing `telegram_contacts` records into contacts and identities (linking primary telegram chat ID, telegram_username without '@', and phone identities).
Implement a second `DO` block to backfill contacts and identities from `audit_logs` where `event_type = 'inbound_message'`.
Drop the legacy `telegram_contacts` table.
Implement the `-- +goose Down` block to restore `telegram_contacts` and drop the new tables.
</action>
<read_first>
- internal/platform/postgres/migrations/015_create_telegram_contacts.sql
- .planning/phases/18-omnichannel-contact-merging/18-RESEARCH.md
</read_first>
<acceptance_criteria>
- Migration SQL passes syntax checking and runs successfully under goose.
- Test migrations successfully apply Up and Down without data loss or key violations.
</acceptance_criteria>
</task>

<task>
<id>18-01-02</id>
<action>
Create `internal/domain/contact.go`.
Define domain structs `Contact` (ID, WorkspaceID, Name, Email, CreatedAt, UpdatedAt, Identities []ContactIdentity) and `ContactIdentity` (ID, ContactID, WorkspaceID, Channel, SenderIdentity, CreatedAt).
</action>
<read_first>
- .planning/phases/18-omnichannel-contact-merging/18-RESEARCH.md
- internal/domain/message.go
</read_first>
<acceptance_criteria>
- Structs successfully compile.
- Fields mapped with correct JSON tags and type markers matching PostgreSQL column definitions.
</acceptance_criteria>
</task>

<task>
<id>18-01-03</id>
<action>
Create `internal/repository/contact.go`.
Implement `ContactRepository` with a `pgxpool.Pool` reference.
Implement transaction-safe functions:
- `GetByID(ctx, workspaceID, contactID)`: fetches a contact and all its identities.
- `ResolveContact(ctx, workspaceID, channel, senderIdentity, name, username, phone)`: maps identity to an existing contact. If not found, attempts cross-linking via username or phone. If still not matched, creates a new contact and links primary, username, and phone identities. Enforce concurrent upsert handling using a `pgx.Tx` transaction block.
- `MergeContacts(ctx, workspaceID, primaryID, secondaryID)`: merges contact identities, handles UNIQUE constraint collisions by deleting secondary duplicates, updates the foreign keys, deletes the secondary contact, and handles transaction rollback on errors.
- `SearchContacts(ctx, workspaceID, query, excludeID, limit)`: searches contacts using `LIKE` on name, email, or identity.
- `ResolveTelegramChatID(ctx, workspaceID, identifier)`: maps telegram username or phone identifier to a numeric chat ID.
</action>
<read_first>
- .planning/phases/18-omnichannel-contact-merging/18-RESEARCH.md
- internal/repository/workspace.go
</read_first>
<acceptance_criteria>
- Repository successfully compiles.
- Methods correctly parameterized to guard against SQL injection.
</acceptance_criteria>
</task>

<task>
<id>18-01-04</id>
<action>
Create `internal/repository/contact_test.go`.
Write unit and integration tests using `getTestPool`:
- Verify `ResolveContact` inserts contacts and identities.
- Verify `ResolveContact` handles concurrent execution gracefully without duplicate key failures.
- Verify `ResolveContact` successfully performs cross-linking matches.
- Verify `MergeContacts` consolidates identities and rolls back on constraint violations or errors.
- Verify `SearchContacts` searches effectively.
- Verify `ResolveTelegramChatID` resolves identifiers.
</action>
<read_first>
- internal/repository/connection_test.go
</read_first>
<acceptance_criteria>
- All tests pass when running `go test -v ./internal/repository -run TestContactRepository`.
</acceptance_criteria>
</task>

<task>
<id>18-01-05</id>
<action>
Modify `internal/repository/telegram_contact.go` and `internal/repository/telegram_contact_test.go`.
Instead of querying the dropped `telegram_contacts` table, refactor the `TelegramContactRepository` methods (`Upsert`, `Resolve`, and `Get`) to use the new `contacts` and `contact_identities` tables behind the scenes. This serves as a backward-compatibility layer so the rest of the application compiled states are preserved in Wave 1.
</action>
<read_first>
- internal/repository/telegram_contact.go
- internal/repository/telegram_contact_test.go
</read_first>
<acceptance_criteria>
- Refactored repository compiles.
- Tests in `telegram_contact_test.go` are updated to align with the new schema and pass.
</acceptance_criteria>
</task>

<task>
<id>18-01-06</id>
<action>
Modify `internal/inbound/processor.go` and `internal/inbound/processor_test.go`.
Add `contactRepo *repository.ContactRepository` field to `InboundProcessor`.
Inject this repository in `NewInboundProcessor`.
Update `InboundEvent` struct to include fields: `SenderName string` and `Metadata map[string]string`.
In `Process(ctx, ev)`, invoke `ResolveContact` to associate/create the contact profile on inbound event ingestion.
Update existing test suites in `processor_test.go` to provide the repository or a mock structure.
</action>
<read_first>
- internal/inbound/processor.go
- internal/inbound/processor_test.go
</read_first>
<acceptance_criteria>
- Inbound processor compiles.
- Test suites pass when executing `go test -v ./internal/inbound/...`.
</acceptance_criteria>
</task>

<task>
<id>18-01-07</id>
<action>
Modify `internal/channel/telegram/inbound.go`, `internal/api/handler/telegram_webhook.go`, and `internal/api/handler/telegram_webhook_test.go`.
In `TelegramInboundAdapter.Parse`, extract the sender's details (First Name, Last Name, Username, Phone Number) and populate the `SenderName` and `Metadata` fields of the returned `InboundEvent`.
Remove or bypass the local contact upsert on `telegramContactRepo` since `InboundProcessor.Process` now performs identity resolution.
Modify the dependencies of `TelegramWebhookHandler` and its constructor accordingly.
</action>
<read_first>
- internal/channel/telegram/inbound.go
- internal/api/handler/telegram_webhook.go
- internal/api/handler/telegram_webhook_test.go
</read_first>
<acceptance_criteria>
- Telegram adapter and webhook handlers compile successfully.
- Webhook handler tests pass.
</acceptance_criteria>
</task>

<task>
<id>18-01-08</id>
<action>
Modify `internal/platform/queue/orchestrator.go` and `internal/platform/queue/worker_test.go`.
Replace the `telegramContactRepo *repository.TelegramContactRepository` field with `contactRepo *repository.ContactRepository`.
Update constructor and setter (`SetContactRepository(repo *repository.ContactRepository)`).
In `dispatchToChannel`, for Telegram messages, call `contactRepo.ResolveTelegramChatID` to resolve handle/phone identifiers.
Update test invocations in `worker_test.go`.
</action>
<read_first>
- internal/platform/queue/orchestrator.go
- internal/platform/queue/worker_test.go
</read_first>
<acceptance_criteria>
- Queue orchestrator compiles successfully.
- Dispatcher tests pass.
</acceptance_criteria>
</task>

<task>
<id>18-01-09</id>
<action>
Modify `cmd/pergo/main.go`.
Initialize `ContactRepository` using the db pgx pool.
Inject it into `InboundProcessor`, `DispatchOrchestrator`, and `TelegramWebhookHandler`.
Ensure compilation success.
</action>
<read_first>
- cmd/pergo/main.go
</read_first>
<acceptance_criteria>
- Composition root compiles successfully.
- Local server runs and starts migrations correctly.
</acceptance_criteria>
</task>

</tasks>

## <verification>
Execute the following verification steps:
1. Run database migrations on a local dev database and ensure schema tables `contacts` and `contact_identities` are correctly created.
2. Run standard backend test suite:
   ```bash
   go test -v ./internal/repository/...
   go test -v ./internal/inbound/...
   go test -v ./internal/platform/queue/...
   go test -v ./internal/api/handler/...
   ```
3. Run the compiler check on the entire project:
   ```bash
   go build ./cmd/pergo
   ```
</verification>

## <success_criteria>
1. System maps a channel identity to contacts successfully.
2. Legacy `telegram_contacts` records are correctly migrated to the new table structure.
3. Pipeline handles concurrent identity resolution without duplicate key failures.
4. Orchestrator translates Telegram handles/phone numbers using `ContactRepository`.
</success_criteria>

## Artifacts this phase produces
- `internal/platform/postgres/migrations/024_omnichannel_contact_merging.sql`
- `internal/domain/contact.go`
- `internal/repository/contact.go`
- `internal/repository/contact_test.go`
