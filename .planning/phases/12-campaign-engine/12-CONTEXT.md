# Phase 12: Campaign Engine - Context

**Gathered:** 2026-07-14
**Status:** Ready for planning

<domain>
## Phase Boundary

Implement throttled campaign sending via CSV mailings, dynamic variable mapping, NATS batches, and enriched logging.

</domain>

<decisions>
## Implementation Decisions

### CSV Parsing and Validation Constraints
- Phone Validation and Sanitization: Keep only digits, validate length between 10 and 15 digits (E.164 range). Reject numbers outside this range.
- Error Reporting: Show summary counts (total, valid, duplicates, invalid) + download button for skipped rows CSV.
- Sniff CSV Delimiter: Auto-detect delimiter (comma, semicolon, tab) by checking the first row.
- Placeholders: Use first row as headers. If missing/unmatched, fall back to 0-based indices as placeholders (e.g. `{{0}}`, `{{1}}`).

### Campaign Scheduling & Dispatch Engine
- Campaign Persistence: Store campaign metadata in `campaigns` table (id, workspace_id, name, status [draft, scheduled, sending, completed, cancelled], scheduled_at), with message dispatches linked via campaign_id.
- Dispatch Throttling: Slice mailing into batches, enqueue batch-sending tasks to NATS JetStream, worker processes batch-by-batch using configurable delay + random jitter of [-0.5s, +0.5s].
- Duration Estimation: Display dynamic calculation in UI: `(Total Valid Messages / Batch Size) * (Inter-Batch Delay + Jitter Mean)`.
- Cancellation: Operator can cancel campaigns in the UI, updating campaign status to `cancelled`. Worker checks campaign status in DB/cache before sending each batch and aborts if cancelled.

### Database Logging & Enriched Outbound Logs (Option A)
- Enrichment: Add `campaign_id` (UUID, nullable), `template_name` (VARCHAR, nullable), and `variables_json` (JSONB, nullable) columns to the `message_dispatches` table.
- Indexing: Create a compound partial index: `CREATE INDEX idx_message_dispatches_campaign ON message_dispatches(workspace_id, campaign_id) WHERE campaign_id IS NOT NULL;`
- Variables JSON Format: Flat JSON object mapping placeholders to resolved strings (e.g. `{"nome": "João", "cidade": "São Paulo"}`).
- Trace-ID Generation: Generate unique Trace-ID using prefix format `campaign_${campaign_id}_${recipient}` for easy tracking and correlation.

### the agent's Discretion
None — all questions resolved.

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/repository/waba_template.go`: WABA templates storage and CRUD operations.
- `internal/repository/dispatch.go`: Message dispatches CRUD operations in `message_dispatches` table.
- `internal/platform/queue/jetstream.go`: NATS JetStream client and queue streams setup.

### Established Patterns
- Raw parameterized SQL queries in repositories using `pgxpool`.
- Clean Echo handlers return HTML fragments rendered by `a-h/templ`.
- Structured context-aware logging via `log/slog`.
- Transaction safety and migrations managed by pressly/goose.

### Integration Points
- Database schema migrations at `internal/platform/postgres/migrations`.
- Echo router registration in `cmd/pergo/main.go`.
- Admin console templates under `templates/pages/` and `templates/components/`.

</code_context>

<specifics>
## Specific Ideas

None — open to standard approaches.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>
