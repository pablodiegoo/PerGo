# Project Research Summary

## Key Findings

### Stack
- **Zero new dependencies required** — existing Go stdlib (`net/http`, `encoding/json`), pgx/v5, Echo v5, NATS JetStream, templ + HTMX, and log/slog cover all v1.7 features.
- **Meta Graph API v25.0** — default target version, parameterized for future upgrades. All template CRUD, message dispatch, and webhook operations use the same auth/base URL pattern.
- All new data models (WABATemplate, FlowParameters, InteractiveProduct, OrderItem) are domain structs — no ORM, no external schema library.

### Features — Table Stakes vs Differentiators

| Area | Table Stakes (Must Ship) | Differentiators (Ship if Time) |
|------|--------------------------|-------------------------------|
| **Templates** | CRUD lifecycle, component support (header/body/footer/buttons), status tracking, category alignment, multi-locale variants, local PostgreSQL cache | Quality score telemetry, visual previewer, multi-WABA drift detection |
| **Commerce** | Single-product messages, multi-product lists, catalog pre-flight verification | Native order webhook processing, stock check integration |
| **Meta Flows** | Flow dispatch (`type: "flow"`), `nfm_reply` auto-parsing | Data exchange middleware/crypto, flow token state tracking |
| **Session Window** | Session tracking database, pre-flight out-of-window block (HTTP 422) | Smart template auto-upgrade fallback, window expiration webhooks |
| **Validation** | Parameter sequential numbering, character limits, button config rules, category validation | — (all validation is table stakes) |

### Architecture — Where Things Go

| Component | Location | Notes |
|-----------|----------|-------|
| Template PostgreSQL repo | `internal/repository/waba_template.go` | Already exists — extend with CRUD methods |
| Meta Graph API client | `internal/channel/whatsapp/waba_template_client.go` | New file — encapsulates all Graph API calls |
| In-memory template cache | `internal/channel/whatsapp/template_cache.go` | New file — `sync.RWMutex`, keyed by `connection_id:name:language` |
| Session window checker | `internal/channel/whatsapp/waba_session_manager.go` | New file — backed by `recipient_sessions` table |
| Commerce/Flow transformers | `internal/channel/whatsapp/waba_interactive.go` | Extends existing interactive mapping |
| Template REST API | `internal/api/template_handler.go` | New file — Echo v5 CRUD endpoints |
| Admin UI templates | `internal/admin/templates/` | New templ components for template management |
| Webhook routing | `internal/channel/whatsapp/waba_inbound.go` | Extend discriminator for `message_template_status_update`, `order`, `nfm_reply` |

### Critical Pitfalls to Address

1. **Template approval is async** — Meta returns `PENDING` on creation; must rely on webhooks for actual status. Block dispatch if `status != APPROVED`.
2. **Two-stage JSON unmarshaling** — `nfm_reply.response_json` is a stringified JSON string, not a nested object. Must decode twice.
3. **Session window race at dispatch time** — validate window in the worker goroutine (not just at ingestion), with 5-minute safety buffer (close at 23h55m).
4. **Queue latency violation** — messages enqueued within the window may be dispatched after it closes. Re-validate at dispatch; auto-convert to template or fail fast.
5. **`flow_token` security** — must be HMAC-signed with tenant context and short TTL to prevent tampering/replay.
6. **Commerce `item_price` type instability** — Meta delivers as string or number across versions. Use custom parser.
7. **Idempotent webhook processing** — Meta delivers at-least-once. Deduplicate on `wamid`/`order_id` before executing side effects.
8. **Independent new tables** — `waba_templates`, `waba_sessions` as standalone tables. Never add columns to high-traffic tables without `CONCURRENTLY`.

## Implications for Roadmap

### Suggested Build Order (4 phases, dependency-driven)

1. **Session Window & Inbound Foundation** — `recipient_sessions` schema, inbound `last_inbound_at` tracking, `IsWindowOpen` pre-flight check in dispatcher. This is a dependency for all other WABA features because session state must be accurate before template/commerce dispatch.

2. **Template CRUD, Sync & Caching** — `waba_templates` schema, Meta Graph API client, in-memory cache, status webhook handler, local validation engine, REST API + admin UI. Template dispatch depends on having templates stored locally.

3. **Template Sending & Meta Flows** — `POST /messages` with `type: "template"` parameter binding, `type: "flow"` dispatch transformer, `nfm_reply` two-stage parsing. Both features extend the existing outbound dispatch pipeline.

4. **Commerce Catalogs & Order Processing** — `type: "product"` / `type: "product_list"` transformers, `catalog_id` resolution, inbound `order` webhook parsing. Lowest dependency chain — can be built independently once the interactive transformer infrastructure exists from Phase 3.

### Key Design Decisions to Lock

- Template dispatch MUST validate `status == APPROVED` against local cache before enqueuing
- Session window validation runs BOTH at ingestion (HTTP 422) AND at dispatch (fail-fast/template-fallback)
- `flow_token` uses HMAC-SHA256 signature with `workspace_id + connection_id + recipient + nonce + expiry`
- Commerce order webhooks are idempotent on `wamid` with NATS JetStream enqueue-first pattern
- All new PostgreSQL tables are independent (no ALTER on `messages` or `conversations`)

## Sources

- Meta WhatsApp Cloud API Reference (v25.0)
- Meta WhatsApp Business Management API — Message Templates
- Meta WhatsApp Flows Developer Guide & Data API Specs
- Meta Commerce Manager & Product Catalog API
- Meta Graph API Error Codes documentation
- PerGo codebase inspection: `internal/channel/whatsapp/`, `internal/domain/`, `internal/repository/`