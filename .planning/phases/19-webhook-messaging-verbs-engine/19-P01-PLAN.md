---
phase: "19-webhook-messaging-verbs-engine"
plan: "19-P01"
subsystem: "database"
must-haves:
  - "Postgres schema migration 025_webhook_verbs_engine.sql compiles and applies cleanly Up and Down."
  - "Contact domain model matches database table columns including tags and closed_at."
  - "ContactRepository GetByID, SearchContacts, ResolveContact, AddTags, and CloseThread are updated/created and fully tested."
  - "ResolveContact resets closed_at = NULL whenever a contact is resolved or matched."
  - "VerbsEngine parses and executes verbs: reply, wait, forward, tag, and close sequentially."
  - "VerbsEngine enforces a 10-second cap on wait durations and a 30-second cap on execution timeout."
  - "Unit tests in internal/webhook/verbs_test.go verify sequential execution and time cap controls."
---

# Plan 19-P01: Schema Migrations & Verbs Engine Core

## <objective>
Establish database schema updates to support Contact tagging and threading/closing. Update the domain structures and the Contact repository with tags/close management. Implement the core Messaging Verbs Engine that parses, validates, and sequentially executes the `reply`, `wait`, `forward`, `tag`, and `close` verbs. Enforce strict timeouts and limits: 30-second total execution context timeout and a 10-second cap on individual wait durations. Validate the core engine with unit tests.
</objective>

## <tasks>

<task>
<id>19-01-01</id>
<objective>Create the database schema migration to add tags and closed_at to the contacts table.</objective>
<read_first>
- .planning/phases/19-webhook-messaging-verbs-engine/19-RESEARCH.md
- internal/platform/postgres/migrations/024_omnichannel_contact_merging.sql
</read_first>
<files_modified>
- internal/platform/postgres/migrations/025_webhook_verbs_engine.sql
</files_modified>
<implementation>
- Create a goose migration file `internal/platform/postgres/migrations/025_webhook_verbs_engine.sql`.
- In `+goose Up`:
  - Run `ALTER TABLE contacts ADD COLUMN tags TEXT[] NOT NULL DEFAULT '{}';`
  - Run `ALTER TABLE contacts ADD COLUMN closed_at TIMESTAMPTZ;`
  - Run `CREATE INDEX idx_contacts_tags ON contacts USING gin(tags);`
- In `+goose Down`:
  - Run `DROP INDEX IF EXISTS idx_contacts_tags;`
  - Run `ALTER TABLE contacts DROP COLUMN IF EXISTS tags;`
  - Run `ALTER TABLE contacts DROP COLUMN IF EXISTS closed_at;`
- Verification command:
  ```bash
  go test -v ./internal/repository -run TestContactRepository
  ```
</implementation>
</task>

<task>
<id>19-01-02</id>
<objective>Update the Contact domain model to support tags and closed_at fields.</objective>
<read_first>
- internal/domain/contact.go
- .planning/phases/19-webhook-messaging-verbs-engine/19-RESEARCH.md
</read_first>
<files_modified>
- internal/domain/contact.go
</files_modified>
<implementation>
- Edit `internal/domain/contact.go` to add `Tags` and `ClosedAt` fields to the `Contact` struct.
- Definition:
  ```go
  Tags     []string   `json:"tags"`
  ClosedAt *time.Time `json:"closed_at,omitempty"`
  ```
- Compile the package to verify:
  ```bash
  go build ./internal/domain/...
  ```
</implementation>
</task>

<task>
<id>19-01-03</id>
<objective>Update ContactRepository queries and add tag/close support.</objective>
<read_first>
- internal/repository/contact.go
- .planning/phases/19-webhook-messaging-verbs-engine/19-RESEARCH.md
</read_first>
<files_modified>
- internal/repository/contact.go
</files_modified>
<implementation>
- Update `GetByID` to select and scan `tags` and `closed_at`.
- Update `SearchContacts` to select and scan `tags` and `closed_at`.
- Update `ResolveContact` to reset `closed_at = NULL` and set `updated_at = NOW()` on match/resolve:
  - Run `UPDATE contacts SET closed_at = NULL, updated_at = NOW() WHERE id = $1 AND closed_at IS NOT NULL` in the fast first-read check (when identity matches) and inside the transaction check / link blocks before commit.
- Implement `AddTags(ctx context.Context, workspaceID, contactID uuid.UUID, tags []string) error` using `ARRAY(SELECT DISTINCT val FROM unnest(array_cat(tags, $3)) val WHERE val IS NOT NULL)` to ensure uniqueness and append new tags.
- Implement `CloseThread(ctx context.Context, workspaceID, contactID uuid.UUID) error` to set `closed_at = NOW()`.
- Compile the repository package to verify:
  ```bash
  go build ./internal/repository/...
  ```
</implementation>
</task>

<task>
<id>19-01-04</id>
<objective>Update contact repository tests to verify tags and closed_at lifecycle.</objective>
<read_first>
- internal/repository/contact_test.go
- .planning/phases/19-webhook-messaging-verbs-engine/19-RESEARCH.md
</read_first>
<files_modified>
- internal/repository/contact_test.go
</files_modified>
<implementation>
- Add test assertions in `internal/repository/contact_test.go` to verify tags and closed_at operations:
  - Check that a newly created contact is initialized with empty tags and nil closed_at.
  - Test `AddTags` appends tags and deduplicates them.
  - Test `CloseThread` sets `closed_at` to a non-nil timestamp.
  - Test that calling `ResolveContact` on a closed contact resets `closed_at` back to `nil`.
- Run tests:
  ```bash
  go test -v ./internal/repository -run TestContactRepository
  ```
</implementation>
</task>

<task>
<id>19-01-05</id>
<objective>Implement the sequential Webhook Messaging Verbs Engine.</objective>
<read_first>
- .planning/phases/19-webhook-messaging-verbs-engine/19-RESEARCH.md
- internal/webhook/dispatcher.go
</read_first>
<files_modified>
- internal/webhook/verbs.go
</files_modified>
<implementation>
- Create `internal/webhook/verbs.go`.
- Define type structures: `Verb`, `ReplyParams`, `WaitParams`, `ForwardParams`, `TagParams`, `CloseParams`.
- Implement `VerbsEngine` struct with dependencies: `outbound.Publisher`, `*repository.ContactRepository`, `*repository.UserActionLogRepository`, and `outbound.RouteResolver`.
- Implement constructor `NewVerbsEngine`.
- Implement `Execute(ctx context.Context, task WebhookDeliveryTask, verbs []Verb) error`:
  - Establish a 30-second execution context timeout.
  - Unmarshal `task.Payload` to parse `InboundEventPayload`.
  - Resolve the `ContactID` using `contactRepo.ResolveContact`.
  - Loop through verbs sequentially. Check `ctx.Done()` at each iteration.
  - Handle `reply` and `forward` placeholder execution methods (which will publish to NATS in P02).
  - Handle `wait` parsing with `time.ParseDuration` and enforce a maximum 10-second cap. Use `select` block over `time.After(d)` and `ctx.Done()`.
  - Handle `tag` by calling `contactRepo.AddTags`.
  - Handle `close` by calling `contactRepo.CloseThread`.
  - Record execution results by logging (mock action logs repository for now if needed, or add logging stub `logActionResults`).
- Verify compilation:
  ```bash
  go build ./internal/webhook/...
  ```
</implementation>
</task>

<task>
<id>19-01-06</id>
<objective>Write unit tests for the Webhook Messaging Verbs Engine.</objective>
<read_first>
- .planning/phases/19-webhook-messaging-verbs-engine/19-RESEARCH.md
- internal/webhook/verbs.go
</read_first>
<files_modified>
- internal/webhook/verbs_test.go
</files_modified>
<implementation>
- Create `internal/webhook/verbs_test.go`.
- Implement tests verifying `VerbsEngine.Execute`:
  - Verify sequential execution of mixed verbs (e.g. tag, wait, close).
  - Verify validation errors return cleanly when duration parsing or JSON params fail.
  - Verify wait duration capping (e.g. 15s wait is capped at 10s).
  - Verify total execution timeout bounds (e.g. sequence exceeding 30s is aborted).
  - Verify context cancellation halts verb execution midway.
- Run tests:
  ```bash
  go test -v ./internal/webhook -run TestVerbsEngine
  ```
</implementation>
</task>

<task>
<id>19-01-07</id>
<objective>Verify all Wave 1 functionality builds and passes tests cleanly.</objective>
<read_first>
- internal/repository/contact_test.go
- internal/webhook/verbs_test.go
</read_first>
<files_modified>
- None
</files_modified>
<implementation>
- Run all repository and webhook unit tests:
  ```bash
  go test -v ./internal/repository -run TestContactRepository
  go test -v ./internal/webhook -run TestVerbsEngine
  ```
</implementation>
</task>

</tasks>
