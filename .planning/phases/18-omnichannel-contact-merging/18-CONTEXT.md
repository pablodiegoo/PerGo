# Phase 18: Omnichannel Contact Merging - Context

**Gathered:** 2026-07-16
**Status:** Ready for planning

<domain>
## Phase Boundary

This phase implements omnichannel contact merging. It unifies recipient channel-specific identities (Telegram handle, WhatsApp number) under a single `contacts` profile scoped by workspace, auto-resolves identities on inbound/outbound events, allows operators to merge profiles, and unifies message histories under consolidated chat threads in the Inbox.

</domain>

<decisions>
## Implementation Decisions

### Database Schema & Constraints
- **Workspace Scoped Identities**: Scope `contact_identities` per workspace. The table will have a `workspace_id` reference (or we can reference the `workspace_id` from the parent `contacts` table) and enforce `UNIQUE (contact_id, channel, sender_identity)` or a custom unique index. To enforce uniqueness per workspace-channel-sender, we can include a workspace constraint, e.g. `UNIQUE (workspace_id, channel, sender_identity)` on the `contact_identities` table or via a composite constraint.
- **Delete Cleanup Strategy**: Use cascade deletion on `contact_id` (`ON DELETE CASCADE`) to clean up identities automatically.
- **Core Fields**: The `contacts` table will store `id`, `workspace_id`, `name`, `email`, `created_at`, `updated_at`.
- **Legacy Telegram Migration**: Migrate existing `telegram_contacts` records into `contacts` and `contact_identities` tables inside the migration SQL script, then drop `telegram_contacts`.

### Inbound Identity Resolution Pipeline
- **Trigger**: Run the contact resolution (`ResolveContact`) inside inbound message processors.
- **Name Source**: On first resolution, extract the sender name from WhatsApp push name or Telegram metadata, falling back to the phone/handle if blank.
- **Concurrency Protection**: Use database upsert pattern (`INSERT ... ON CONFLICT DO NOTHING`) inside transactions.
- **Outbound Message Resolution**: Automatically resolve/create a contact when an operator initiates outbound messaging.

### Contact Merging API & Dashboard UI
- **Merge Verification**: Merging is strictly verified to ensure both contact IDs belong to the same workspace and are distinct.
- **Merge Target**: Wrap all merge updates in a transaction: update all foreign keys in `contact_identities` and modify conversation pointers to primary.
- **Search & Select Interface**: Add an HTMX-powered type-ahead search panel inside the Inbox chat view for merging.
- **Audit Logging**: Log profile merge operations as user action logs (`logs/actions`).

### Inbox Consolidated Thread Queries
- **Unifying Threads**: Query `audit_logs` using the set of all sender identities linked to the contact.
- **Composer Channel Selection**: Display a selector in the compose box to choose which channel to reply on.
- **Unread Count**: Consolidated unread count summing all linked identities.
- **Query Compatibility**: Retain existing CTE query structure for Inbox conversation list, adding contacts as an enrichment layer.

### the agent's Discretion
- The exact layout of the merging dropdown/dialog in the Inbox.
- The precise structure of the database transaction wrapper in the repository code.

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- [DefaultDispatcher](file:///home/pablo/Coding/OmniGo/internal/webhook/dispatcher.go)
- [WorkspaceRepository](file:///home/pablo/Coding/OmniGo/internal/repository/workspace.go)

### Established Patterns
- Pure SQL raw parameterized queries.
- Echo handlers returning compiled `templ` layouts.
- Transactions using `pgx.Tx` or connection pools.

### Integration Points
- `internal/api/handler/admin/`: Add new contact routes and merge actions.
- `templates/pages/inbox.templ` and chat views: Render consolidated lists and merging components.
- Database migration directory for the new tables setup.

</code_context>

<specifics>
## Specific Ideas
- None — discussion stayed within phase scope.

</specifics>

<deferred>
## Deferred Ideas
- None — discussion stayed within phase scope.

</deferred>
