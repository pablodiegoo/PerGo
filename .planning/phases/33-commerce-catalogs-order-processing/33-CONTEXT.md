# Phase 33: Commerce Catalogs & Order Processing - Context

**Gathered:** 2026-07-30
**Status:** Ready for planning

<domain>
## Phase Boundary

Phase 33 delivers WABA Commerce integration for PerGo: `POST /messages` support for single-product messages (`type: "product"`) and multi-product list messages (`type: "product_list"`), connection-level `default_catalog_id` configuration, pre-flight payload & catalog validation with Meta API error translation, inbound WhatsApp order webhook parsing into normalized `order.created` events with idempotent `wamid` deduplication, and visual Chat UI summary bubbles for products and inbound orders.

Implements COMM-01, COMM-02, COMM-03, COMM-04, COMM-05.
</domain>

<decisions>
## Implementation Decisions

### Pre-flight Catalog & SKU Validation (COMM-01, COMM-02, COMM-05)
- **D-01:** Synchronous structural pre-flight validation + Meta Graph API error translation. During `POST /messages` ingestion for `type: "product"` and `type: "product_list"`, validate non-empty `catalog_id` (or fallback to connection default), non-empty retailer IDs/SKUs (`product_retailer_id`), and section/item bounds (max 10 sections, max 30 items total, title max 24 chars). If validation fails, return HTTP 422 `invalid_product_payload` with detailed validation errors before NATS JetStream enqueue.
- **D-02:** Worker Meta API error mapping. If Meta Graph API returns catalog/SKU errors during worker dispatch (e.g., error codes 131009/131084 for invalid catalog/SKU), map them to normalized delivery failure events `order_dispatch_failed` / `invalid_sku` with trace correlation.

### Inbound Order Webhook Processing & Inbox UI (COMM-04)
- **D-03:** Message metadata order storage & normalized event emission. Parse inbound Meta webhooks where `messages[].type == "order"` (containing `catalog_id`, `text`, `product_items` with SKU, quantity, item_price, currency). Store raw order structure inside `messages.metadata`. Emit normalized `order.created` event payload to workspace webhooks containing `order_id`, `catalog_id`, `items`, `total_price`, `currency`, `wamid`, `contact_id`, and `trace_id`.
- **D-04:** Idempotent order deduplication. Use `wamid` message deduplication to ensure inbound order webhooks are processed exactly once and prevent duplicate `order.created` webhook emissions.
- **D-05:** Visual Chat UI order & product bubbles. Render formatted WhatsApp order summary bubbles in the admin Inbox Chat UI showing catalog ID badge, product item list, price totals, currency badge, and customer notes. Render outbound `product` and `product_list` messages as formatted catalog product cards.

### Default Catalog Auto-Injection & Config (COMM-01, COMM-02, COMM-03)
- **D-06:** Connection default catalog setting. Add `default_catalog_id` field to WABA connection credentials / settings in PostgreSQL.
- **D-07:** Resolution precedence order. When `POST /messages` comes in for `type: "product"` or `type: "product_list"`:
  1. Explicit `catalog_id` in request payload.
  2. `default_catalog_id` from target WABA connection.
  3. If both missing, synchronously reject with HTTP 422 `missing_catalog_id`.

### agent's Discretion
- Internal struct layout for `ProductItem` and `ProductSection` in `internal/domain/message.go`.
- CSS classes and Templ component structure for rendering order bubbles in `internal/ui/view/inbox.templ`.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

- `.planning/REQUIREMENTS.md` — Requirements COMM-01 to COMM-05
- `.planning/ROADMAP.md` — Phase 33 specification and success criteria
- `internal/api/handler/message.go` — Ingestion handler for `POST /messages`
- `internal/outbound/processor.go` — Outbound dispatch pipeline and NATS queue listener
- `internal/channel/whatsapp/waba.go` — WABA channel adapter sending Meta Graph API payloads (`SendProduct`, `SendProductList`)
- `internal/api/handler/waba_webhook.go` — Inbound WABA webhook handler for `messages[].type == "order"`

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `crypto.EncryptAESGCM` & `crypto.DecryptAESGCM`: WABA credentials encryption in `internal/platform/crypto/`.
- `WABAConnection` / `WABAConfig`: WABA credentials model in `internal/repository/connection.go`.
- `OutboundProcessor`: Handles NATS JetStream queue consumption and channel dispatch in `internal/outbound/processor.go`.

### Established Patterns
- **Ingestion Pre-flight Validation**: Synchronous validation in `POST /messages` handler returning HTTP 422 before NATS enqueue.
- **Polymorphic Webhook Verbs**: Webhook normalization and event dispatch pattern established in Phase 24/32.

### Integration Points
- `internal/domain/message.go`: Add `MessageTypeProduct`, `MessageTypeProductList`, and order metadata structures.
- `internal/api/handler/message.go`: Add product payload validation and `default_catalog_id` fallback.
- `internal/channel/whatsapp/waba.go`: Add Meta Graph API interactive product message formatters.
- `internal/api/handler/waba_webhook.go`: Add inbound `type: "order"` webhook parser and `order.created` event dispatcher.
- `internal/ui/view/inbox.templ`: Add visual order summary bubble component.

</code_context>

<specifics>
## Specific Ideas

- Product list limits: 10 sections max, 30 total items across sections, 24 characters max per section title.
- Order webhook payload normalization: include `catalog_id`, `items[]` (sku, qty, price, currency), `total_price`, `currency`, `wamid`, `trace_id`.
</specifics>

<deferred>
## Deferred Ideas

- Real-time stock & price check integration before product message dispatch (Deferred to COMM-F01).
- Abandoned cart trigger flows (Deferred to COMM-F02).
- Multi-currency and regional price formatting based on recipient locale (Deferred to COMM-F03).

</deferred>

---

*Phase: 33-commerce-catalogs-order-processing*
*Context gathered: 2026-07-30*
