# Phase 31: Template CRUD, Meta Graph API Sync & Local Cache - Context

**Gathered:** 2026-07-26
**Status:** Ready for planning

<domain>
## Phase Boundary

This phase delivers **WABA template lifecycle management** — operators create, edit, delete, sync, and preview WhatsApp message templates through both a REST API and the admin UI. Templates are scoped per WABA connection, submitted directly to Meta's Graph API, cached locally in PostgreSQL with an in-memory layer, and kept in sync via Meta webhooks. This phase does NOT include template dispatch via `POST /messages` (Phase 32) or commerce catalogs (Phase 33).

</domain>

<decisions>
## Implementation Decisions

### Template Creation Flow
- **D-01:** Creating a template via REST API or admin UI submits it **immediately** to Meta Graph API and stores the result locally (status starts as PENDING). Single action — operator sees Meta's response including any errors.
- **D-02:** Template mutations follow **Meta's actual API constraints** — approved templates cannot be edited (only deleted and recreated); rejected/paused templates can be edited. Agent designs the update/delete logic accordingly.
- **D-03:** Templates are **scoped by connection** — `connection_id` is required for all template operations. Each WABA connection manages its own templates independently.
- **D-04:** Meta Graph API errors surface using the **existing error pattern** established in other handlers — agent follows existing conventions.

### Cache Architecture
- **D-05:** Agent decides cache key structure, **aligned with the connection-scoped ownership model** (connection_id as part of the key).
- **D-06:** **Webhook-driven invalidation only** — cache stays fresh via `message_template_status_update` webhooks. No TTL. Manual sync button serves as escape hatch if webhooks lag.
- **D-07:** Agent decides warmup strategy — context: potential **100k message burst** scenarios mean cold-start cache misses could hammer the DB. Eager warmup from PostgreSQL on startup is likely the right call.
- **D-08:** **Per-connection rate limit** on manual sync endpoint — each connection can sync independently every 15 minutes. Workspaces with multiple WABA numbers aren't bottlenecked.

### Admin UI — Template Builder
- **D-09:** **Full structured form** with dedicated sections for each component type — HEADER (select: text/image/video/document), BODY (textarea with {{1}} parameter helper buttons), FOOTER (text), BUTTONS (add/remove with type selection: URL/PHONE/QUICK_REPLY). Like Meta's own template builder.
- **D-10:** Template creation/edit uses an **inline page** at `/templates/new` (or `/templates/{id}/edit`), fitting the existing admin navigation pattern.
- **D-11:** Template listing uses a **table/list with color-coded status badges** — APPROVED (green), PENDING (yellow/amber), REJECTED (red), PAUSED (gray), DISABLED (gray strikethrough). Filterable by status column.
- **D-12:** Templates are **grouped under connection** in navigation — Connections → [connection] → Templates. Operator selects a WABA connection first, then manages its templates.
- **D-13:** Agent decides **multi-locale variant UX**, aligned with Meta's model where each name+language combination is a distinct template.
- **D-14:** **Toast notifications** for quality score changes (GREEN→YELLOW→RED), using the existing notification pattern already in the codebase.
- **D-15:** **Inline rejection display** — rejected templates show Meta's rejection reason directly in the list row (expandable) and prominently on the template detail/edit page.

### Visual Template Previewer
- **D-16:** **Live side-panel preview** — a WhatsApp-style chat bubble preview panel sits to the right of the creation form, updating in real-time as the operator types.
- **D-17:** Agent decides implementation approach — **HTMX debounced partial** vs. **client-side JS** — pick what gives the best UX within the templ+HTMX constraints.
- **D-18:** Preview uses **sample parameter values** — {{1}} → 'John', {{2}} → '12345', etc. Operator can optionally type custom sample values in a separate input area.
- **D-19:** Preview uses a **simplified chat bubble** style — captures the essence (rounded, colored background, component layout, buttons) but consistent with PerGo's Notion-inspired aesthetic rather than pixel-matching WhatsApp.

### Agent's Discretion
The user delegated the following decisions to the implementing agent:
- D-02: Exact edit/delete mutation logic following Meta's API constraints
- D-04: Error response format (follow existing handler patterns)
- D-05: Cache key structure (connection-scoped)
- D-07: Cache warmup strategy (context: 100k msg bursts)
- D-13: Multi-locale variant UX (aligned with Meta's name+language model)
- D-14: Quality alert delivery (toast, existing pattern)
- D-17: Preview rendering approach (HTMX partial vs client-side JS)

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### WABA Requirements
- `.planning/REQUIREMENTS.md` §TMPL-01 through §TMPL-09 — All nine template requirements for this phase

### Architecture & API
- `.planning/codebase/INTEGRATIONS.md` §4 — WABA Meta Graph API endpoints (`/v18.0/{waba_id}/message_templates`)
- `.planning/codebase/STRUCTURE.md` — Project layout, handler/repository/template conventions

### Prior Phase Decisions
- `.planning/phases/030-session-window-inbound-foundation/030-CONTEXT.md` — WABA session window enforcement, `recipient_sessions` table, webhook event patterns
- `.planning/phases/029-connection-slugs-api-routing/029-CONTEXT.md` — Connection slug routing, in-memory RWMutex cache pattern

### Spike Findings
- `.agents/skills/spike-findings-pergo/SKILL.md` — Spike 009 (WABA template inbox delivery), Spike 026 (WABA interactive messages)
- `.agents/skills/sketch-findings-pergo/SKILL.md` — Notion-inspired monochromatic design system, component patterns

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/repository/waba_template.go` — **WABATemplateRepository already exists** with full CRUD: Create, Upsert, GetByID, GetByNameAndLanguage, ListByWorkspace, ListByConnection, UpdateStatus, Delete. The `WABATemplate` struct has ID, WorkspaceID, ConnectionID, MetaTemplateID, Name, Language, Status, Category, Components, CreatedAt, UpdatedAt.
- `internal/api/handler/admin/device.go` — **syncTemplatesFromMeta() already exists** — calls Meta Graph API `GET /v18.0/{waba_account_id}/message_templates?limit=100`. Needs extraction into a dedicated client.
- `internal/api/handler/admin/campaign.go` & `inbox.go` — Already use WABATemplateRepository to list templates for campaigns and inbox template selector.

### Established Patterns
- **Echo v5 handlers**: `c *echo.Context`, multi-tenant workspace extraction via `tenant.WorkspaceIDFrom(c)`
- **Admin UI rendering**: Server-side templ components, HTMX partial responses via `mw.Render(c, http.StatusOK, page/component)`
- **In-memory RWMutex cache**: Phase 29's slug cache (`workspace_id:slug`) — same pattern applies here
- **Meta webhook handling**: `internal/api/handler/waba_webhook.go` with HandleGet (verification) and HandlePost (inbound events) — extend for `message_template_status_update`

### Integration Points
- Meta webhook handler needs extension for `message_template_status_update` event type
- Existing `syncTemplatesFromMeta()` logic in device.go should be extracted into `waba_template_client.go`
- Campaign and inbox handlers already consume templates — the new cache layer serves these existing consumers too
- NATS JetStream for potential async operations (template status change notifications)

</code_context>

<specifics>
## Specific Ideas

- Template builder should mirror Meta's own template builder UX — structured component sections, not a raw JSON editor
- Preview should feel natural alongside the form (side-panel, not modal), updating live as the operator types
- Quality score drops should be immediately visible — operators need to react before Meta pauses their template

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope

</deferred>

---

*Phase: 31-Template CRUD, Meta Graph API Sync & Local Cache*
*Context gathered: 2026-07-26*
