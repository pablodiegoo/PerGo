---
phase: "18"
plan: "18-P02"
type: "standard"
wave: 2
depends_on: ["18-P01"]
files_modified:
  - internal/repository/audit.go
  - internal/repository/audit_test.go
  - internal/api/handler/admin/inbox.go
  - templates/pages/inbox.templ
  - templates/components/chat_panel.templ
  - templates/components/conv_item.templ
  - templates/components/conv_list.templ
  - cmd/pergo/main.go
  - cmd/pergo/admin_contact_merge_test.go
  - internal/repository/telegram_contact.go
  - internal/repository/telegram_contact_test.go
autonomous: true
requirements: ["CONT-01", "CONT-02", "CONT-03", "CONT-04"]
---

# Plan 18-P02: API, UI & Thread Consolidation

## <objective>
Consolidate conversation histories and thread logs under unified contact profiles. Add contact search and merge API endpoints in the admin server. Implement the Inbox UI changes to display contact cards, support channel selection in the compose editor, and embed the type-ahead contacts search-and-merge overlay component. Finally, delete the deprecated `telegram_contacts` files and write full end-to-end integration tests.
</objective>

## <tasks>

<task>
<id>18-02-01</id>
<action>
Modify `internal/repository/audit.go` and `internal/repository/audit_test.go`.
Refactor `ConversationSummary` structure to include `ContactID uuid.UUID` and `ContactName string`.
Rewrite `ListConversations` using a SQL query that groups messages by `contact_id` based on the new `contacts` and `contact_identities` mappings.
Rewrite `ListThread` (renamed to or supplemented by `ListThreadByContact`) to perform a UNION between inbound and outbound events matching ANY identity mapped to the contact ID.
Update the tests in `audit_test.go` to match the new signature and verify grouping behaviors.
</action>
<read_first>
- internal/repository/audit.go
- internal/repository/audit_test.go
- .planning/phases/18-omnichannel-contact-merging/18-RESEARCH.md
</read_first>
<acceptance_criteria>
- Audit repository refactoring compiles.
- Updated tests pass when running `go test -v ./internal/repository -run TestAuditRepository`.
</acceptance_criteria>
</task>

<task>
<id>18-02-02</id>
<action>
Modify `internal/api/handler/admin/inbox.go` and route mappings in `cmd/pergo/main.go`.
Mount GET `/admin/contacts/search` to implement contacts type-ahead matching.
Mount POST `/admin/contacts/merge` to merge contact IDs (calling `ContactRepository.MergeContacts` in a transaction) and log the operation via `UserActionLogRepository` (action: "contact.merge").
Rewrite inbox handlers (`View`, `PollConversations`, `ChatPanel`, `PollMessages`, `SendMessage`, `NewMessageSend`) to load, fetch, and structure conversations based on contact ID (UUID) instead of raw `from` identifiers.
Replace `TelegramContacts *repository.TelegramContactRepository` dependency with `ContactRepo *repository.ContactRepository`.
</action>
<read_first>
- internal/api/handler/admin/inbox.go
- cmd/pergo/main.go
- .planning/phases/18-omnichannel-contact-merging/18-RESEARCH.md
</read_first>
<acceptance_criteria>
- Endpoints compile.
- Audit action logs are written on contact merging.
</acceptance_criteria>
</task>

<task>
<id>18-02-03</id>
<action>
Modify templates `templates/pages/inbox.templ`, `templates/components/chat_panel.templ`, `templates/components/conv_item.templ`, and `templates/components/conv_list.templ`.
Update `ConvItem` to take `ContactID` and link routes to load chat panels via the contact ID.
Update `ChatPanel` to accept contact details and show a contact details card in the header.
Implement a reply channel picker dropdown in the text editor (allowing operators to select which linked channel and identity to send a reply on).
Embed the inline HTMX-powered type-ahead contacts merging input overlay in the ChatPanel header.
Run `templ generate` to compile the templates.
</action>
<read_first>
- templates/pages/inbox.templ
- templates/components/chat_panel.templ
- templates/components/conv_item.templ
- templates/components/conv_list.templ
- .planning/phases/18-omnichannel-contact-merging/18-RESEARCH.md
</read_first>
<acceptance_criteria>
- `templ generate` compiles code successfully without warnings.
- HTML elements have correct unique IDs and HTMX tags.
</acceptance_criteria>
</task>

<task>
<id>18-02-04</id>
<action>
Delete deprecated files `internal/repository/telegram_contact.go` and `internal/repository/telegram_contact_test.go`.
Remove remaining references from `cmd/pergo/main.go`.
</action>
<read_first>
- cmd/pergo/main.go
</read_first>
<acceptance_criteria>
- Build compiles completely without errors after deleting the files.
</acceptance_criteria>
</task>

<task>
<id>18-02-05</id>
<action>
Create `cmd/pergo/admin_contact_merge_test.go`.
Write end-to-end integration tests using real test containers (PostgreSQL + NATS) to verify:
- Simulating inbound events creates distinct contacts.
- Merging secondary contact into primary succeeds.
- Conversation thread unifies histories correctly under the primary contact ID.
- Check that error conditions (e.g. merging contacts from different workspaces) are rejected.
- Verify transaction rollback on merge failures.
</action>
<read_first>
- cmd/pergo/admin_test.go
- cmd/pergo/admin_audit_test.go
</read_first>
<acceptance_criteria>
- Integration test suite passes (`go test -v ./cmd/pergo -run TestAdminContactMerge`).
</acceptance_criteria>
</task>

</tasks>

## <verification>
Execute the following verification steps:
1. Compile the templates:
   ```bash
   templ generate
   ```
2. Run unit and integration tests:
   ```bash
   go test -v ./internal/repository/...
   go test -v ./internal/api/handler/...
   go test -v ./cmd/pergo/...
   ```
3. Run the compiler check on the entire project:
   ```bash
   go build ./cmd/pergo
   ```
</verification>

## <success_criteria>
1. Merged contacts display a single consolidated message thread combining WhatsApp and Telegram chat histories in the Inbox.
2. Dashboard UI permits search-and-merge operations on contacts with full transaction rollbacks on failure.
3. Outbound replies can be directed dynamically to any of the contact's linked identities using the reply channel picker dropdown.
4. Core codebase does not contain deprecated siloed Telegram contact mapping code.
</success_criteria>

## Artifacts this phase produces
- `cmd/pergo/admin_contact_merge_test.go`
