# Phase 29: Connection Slugs & API Channel Routing - Research

## Research Summary
Phase 29 introduces `slug` for connections in the PerGo system to enable human-friendly channel identifiers in API routing (`CreateMessageRequest`). The research found that `Connection` creation is currently handled by `internal/api/handler/admin/device.go` and persisted via `ConnectionRepository` in `internal/repository/connection.go`. Routing logic is orchestrated by `OutboundProcessor` in `internal/outbound/processor.go` via the `RouteResolver` interface. We will need a slug generation utility to normalize strings, a DB migration to add the `slug` column and backfill existing rows, and in-memory caching in the repository to bypass database queries during high-throughput message ingestion.

## Codebase Analysis
- **`internal/domain/message.go`**: Contains `CreateMessageRequest`. The `Channel` field is currently validated against a strict `ValidChannels` map (`whatsapp`, `telegram`, etc.). This validation needs adjusting because a channel can now be a generic connection slug.
- **`internal/outbound/processor.go`**: Implements `Ingest`. It queries `p.resolver.GetBySenderIdentity` or `p.resolver.GetDefaultChannelConnection`. We need to add `p.resolver.GetBySlug(ctx, workspaceID, req.Channel)` in the routing step (D-02).
- **`internal/repository/connection.go`**: Defines the `Connection` struct. The repository will need `slug` added to queries, scans, and inserts. It will also serve as the `RouteResolver`, so we must implement `GetBySlug`. To satisfy D-08, we should introduce an in-memory cache `map[string]*Connection` guarded by `sync.RWMutex` (as demonstrated in `session-caching.md`).
- **`internal/api/handler/admin/device.go`**: Contains `Create` handler for new connections. We should auto-generate the slug here or in `ConnectionRepository.Create` using the `name` field, appending `-1`, `-2` etc. in case of duplicates (D-03, D-04).
- **`internal/platform/postgres/migrations/`**: Contains the migration scripts. We will add a new migration (e.g. `030_connection_slugs.go` or `.sql`) to add the `slug` column, populate it, and enforce uniqueness.

## Technical Approach
1. **Slug Utility:** Implement a `slug.Generate(name string)` function (e.g., in `internal/pkg/slug`) that converts strings to lowercase ASCII, replacing whitespace and special characters with hyphens.
2. **Database Migration:** Create a multi-step migration (using a `.go` migration via `goose` or PL/pgSQL). Steps: 
   - `ADD COLUMN slug VARCHAR(255)`
   - Backfill slugs from `name` + `channel`, handling `workspace_id` collisions via numeric suffixes.
   - `ALTER COLUMN slug SET NOT NULL`
   - Create unique index `idx_connections_workspace_slug ON connections(workspace_id, slug)`.
3. **Repository Updates:** Update `Connection` struct in `internal/repository/connection.go` to include `Slug string`. Update all CRUD queries (`INSERT`, `SELECT`, `UPDATE`).
4. **Cache & RouteResolver:**
   - Introduce `ConnectionCache` inside `ConnectionRepository`.
   - On `Create`, `UpdateStatus`, `Delete`, `SaveCredentials`, update or invalidate the cache.
   - Implement `GetBySlug(ctx, workspaceID uuid.UUID, slug string) (*Connection, error)` which checks the cache first (key: `workspace_id:slug`). On miss, it queries the DB and caches the result.
5. **API & Validation Updates:**
   - In `internal/domain/message.go`, relax `ValidChannels` check for `req.Channel` so it allows generic slugs. Validation will naturally happen during routing resolution.
   - In `internal/outbound/processor.go` (`Ingest`), implement D-02 routing precedence: attempt `GetBySlug(ctx, workspaceID, req.Channel)` first, then fallback to `GetDefaultChannelConnection(...)`.

## Dependencies & Integration Points
- API handlers: `internal/api/handler/admin/device.go` and `internal/api/handler/message.go`.
- UI templates (admin console) will need to be updated to show the slug or allow users to edit it (D-05).
- Webhook handlers that use `RouteResolver` (e.g., `internal/webhook/verb_handlers.go`).

## Risk Assessment
- **Validation Drift:** `CreateMessageRequest` validation currently assumes `req.Channel` must be a known legacy channel type. Dropping this means invalid channels will only be caught downstream. We must handle `RouteError` appropriately to return a `400 Bad Request` or `404 Not Found` payload format.
- **Migration Downtime:** Adding a `NOT NULL` constraint requires caution. The backfill logic must guarantee no `NULL` fields before applying the constraint.
- **Cache Staleness:** If the cache isn't correctly invalidated across all updates (especially credential changes or deletions), routing will fail or send via dead connections.

## Test Strategy
- **Unit Tests:** `slug.Generate` should be exhaustively tested for Unicode handling and stripping.
- **Repository Tests:** `ConnectionRepository.GetBySlug` should test cache hit/miss semantics and write-locks.
- **Processor Tests:** `outbound.Processor_test.go` should mock `GetBySlug` and verify that the fallback routing logic works.
- **Migration Tests:** Manually verify the up/down migration logic over an existing seed database to ensure duplicates don't crash the migration.
