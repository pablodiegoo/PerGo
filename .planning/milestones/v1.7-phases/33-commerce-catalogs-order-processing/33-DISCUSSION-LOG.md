# Phase 33: Commerce Catalogs & Order Processing - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-30
**Phase:** 33-Commerce Catalogs & Order Processing
**Areas discussed:** Pre-flight Catalog & SKU Validation, Inbound Order Data Storage & Chat UI Inbox Rendering, Default Catalog Auto-Injection & Validation Limits

---

## Area 1: Pre-flight Catalog & SKU Validation

| Option | Description | Selected |
|--------|-------------|----------|
| Synchronous structural pre-flight validation + Meta Graph API error translation | Fast ingestion; validates catalog binding, SKU formatting, section/item counts; maps Meta SKU/catalog errors on dispatch | ✓ |
| Local SKU cache with background sync | Sync catalog SKUs from Meta into Postgres/memory; validate against local cache at ingestion time | |
| Live Meta Graph API lookup on ingestion | Call Meta API synchronously during POST /messages; rejects invalid SKUs instantly but adds network latency | |

**User's choice:** Synchronous structural pre-flight validation + Meta Graph API error translation
**Notes:** Fast ingestion, pre-flight checks structural bounds and connection catalog configuration, maps Meta dispatch errors (e.g. invalid SKU/catalog codes 131009/131084) to normalized events.

---

## Area 2: Inbound Order Data Storage & Chat UI Inbox Rendering

| Option | Description | Selected |
|--------|-------------|----------|
| Store order JSON in messages.metadata + emit normalized order.created webhook + render visual WhatsApp order bubble in Chat UI | Stateless & lean; full order details in webhook event and Chat UI bubble | ✓ |
| Create a dedicated orders database table in PostgreSQL + emit webhook event + render Chat UI bubble | Relational orders table for historical order queries within PerGo | |

**User's choice:** Store order JSON in messages.metadata + emit normalized order.created webhook + render visual WhatsApp order bubble in Chat UI
**Notes:** PerGo functions as a message router; storing order details in `messages.metadata` + emitting `order.created` webhook events keeps data lean and enables full downstream CRM/ERP order processing.

---

## Area 3: Default Catalog Auto-Injection & Validation Limits

| Option | Description | Selected |
|--------|-------------|----------|
| Explicit precedence (Payload catalog_id > Connection default_catalog_id > HTTP 422 missing_catalog_id) + Sync HTTP 422 for Meta limit violations | Clear resolution order for catalog IDs; enforces Meta WABA product list constraints at ingestion | ✓ |
| Strict payload catalog_id requirement | Require catalog_id in every POST /messages payload | |

**User's choice:** Explicit precedence (Payload catalog_id > Connection default_catalog_id > HTTP 422 missing_catalog_id) + Sync HTTP 422 for Meta limit violations
**Notes:** Precedence resolves missing catalog IDs automatically when connection has a `default_catalog_id`. Synchronously validates Meta product list constraints (max 10 sections, max 30 items, 24-char title limit).

---

## the agent's Discretion

- Internal struct layout for `ProductItem` and `ProductSection` in Go.
- Specific CSS/HTML rendering of catalog product cards and order bubbles in `inbox.templ`.

---

## Deferred Ideas

- COMM-F01: Real-time stock & price check integration before dispatching product messages.
- COMM-F02: Abandoned cart flow triggers combining catalog products with Utility Templates.
- COMM-F03: Multi-currency and regional price formatting based on recipient locale.
