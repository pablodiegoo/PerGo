---
title: Implement WABA Commerce Catalog Messages and Order Webhook Parsing
date: 2026-07-25
priority: high
tags: [waba, commerce, catalog, order]
resolves_phase: 33
---

# Implement WABA Commerce Catalog Messages and Order Webhook Parsing

## Context
Meta WABA supports native product display and shopping cart orders. PerGo needs to support `type: "product"` and `type: "product_list"` dispatches with automatic `catalog_id` resolution, as well as parsing incoming cart orders into `order.created` webhook events.

## Implementation Tasks

1. **Connection Config (`default_catalog_id`)**:
   - Allow storing `default_catalog_id` in WABA Connection credentials/settings metadata.

2. **Dispatch Payload Transformers**:
   - Implement `type: "product"` transformer in WABA package (resolves `sku` to `action.product_retailer_id` and injects `catalog_id`).
   - Implement `type: "product_list"` transformer (maps `sections[].skus` to `action.sections[].product_items`).

3. **Incoming Order Webhook Handler**:
   - In WABA webhook handler, detect `order` message objects.
   - Extract item list, SKU, quantity, unit price, and currency.
   - Calculate `total_amount` and emit normalized `order.created` event.

4. **Unit & Integration Tests**:
   - Test single product and multi-product payload transformation.
   - Test default `catalog_id` fallback and per-request `catalog_id` override.
   - Test order webhook parser and event generation.
