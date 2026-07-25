# Phase 29: Connection Slugs & API Channel Routing - Context

**Gathered:** 2026-07-25
**Status:** Ready for planning

<domain>
## Phase Boundary

Refactoring the API ingestion payload and database model to support unique human-friendly connection slugs for message routing, automatic slug generation on connection creation/editing, and in-memory cache lookup while preserving backward compatibility for legacy channel types.

</domain>

<decisions>
## Implementation Decisions

### API Ingestion Payload Routing (`POST /api/v1/messages`)
- **D-01:** The `channel` field in `CreateMessageRequest` accepts either a connection `slug` (e.g. `"vendas-sp"`) or a legacy channel type (e.g. `"whatsapp_cloud"`).
- **D-02:** Routing resolution precedence in `OutboundProcessor`:
  1. Lookup connection by `(workspace_id, slug)` matching the `channel` field or `from` field.
  2. If no slug match is found, fallback to resolving connection by legacy channel type where `is_default = true`.

### Slug Auto-Generation & Validation
- **D-03:** Auto-generate human-friendly slugs from the connection `name` when created (e.g. `vendas-sp`, `suporte-telegram`).
- **D-04:** Handle slug collisions per workspace by appending numeric suffixes (e.g. `vendas-sp-2`, `vendas-sp-3`).
- **D-05:** Slugs must be validated against `^[a-z0-9-]+$` and enforced unique per `workspace_id` (`UNIQUE(workspace_id, slug)`). Users can edit slugs via the admin UI.

### Database Migration
- **D-06:** Database migration adds `slug` column (`VARCHAR(255) NOT NULL`) with unique index `idx_connections_workspace_slug ON connections(workspace_id, slug)`.
- **D-07:** Migration populates existing connection rows by sanitizing `name` + `channel` with fallback numeric suffixes to ensure no NULL slugs.

### Ingest Gateway Performance & Cache
- **D-08:** Maintain an in-memory cache map `map[string]*Connection` in `ConnectionRepository` / `OutboundProcessor` indexed by `workspace_id:slug` for sub-millisecond route resolution.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Domain & API
- `.planning/PROJECT.md` — Core value and multi-tenant connection model
- `docs/API.md` — API payload schema specification for `POST /api/v1/messages`
- `internal/domain/message.go` — `CreateMessageRequest` payload struct
- `internal/outbound/processor.go` — `OutboundProcessor` ingestion and connection route resolution
- `internal/repository/connection.go` — `Connection` struct, DB repository, and encryption wrapper

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `repository.ConnectionRepository`: Database CRUD for connections with AES-256-GCM encryption/decryption.
- `outbound.Processor`: Handles ingestion payload validation, route resolution, and publishing to JetStream.
- `tenant.WorkspaceIDFrom`: Context extractor for multi-tenant workspace isolation.

### Established Patterns
- Multi-tenant query isolation using `workspace_id`.
- Handlers use Echo v5 context and return domain error structs.

</code_context>

<specifics>
## Specific Ideas

- Allow sending messages using either `"channel": "vendas-sp"` or `"channel": "whatsapp_cloud"` without breaking existing CRM integrations.
- Human-friendly connection names in the admin panel automatically convert to readable slugs.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>

---

*Phase: 29-connection-slugs-and-api-channel-routing*
*Context gathered: 2026-07-25*
