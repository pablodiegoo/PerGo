---
phase: 33
plan_id: "33-05"
subsystem: ui
tags: [inbox, templ, order, catalog, UI]
key-files:
  - templates/components/message_bubble.templ
  - templates/components/message_bubble_templ.go
  - templates/components/message_bubble_test.go
  - internal/repository/audit.go
---

# Phase 33 Plan 33-05 Summary: Inbox Admin UI Order Summary Bubbles & Catalog Cards

Rendered visual WhatsApp order summary bubbles for inbound orders and formatted catalog product cards for outbound dispatches in the Inbox Admin Chat UI using Templ and Tailwind CSS.

## Task Summary Table

| Task | Description | Status | Commit |
| --- | --- | --- | --- |
| T1 | Implement Inbound Order Summary & Outbound Product Card Templ Bubbles | Completed | `35d6675` |
| T2 | Chat UI Message Bubble Unit Test Suite | Completed | `3ccac8b` |

## Accomplishments

- **Inbound Order Summary Bubbles**:
  - Extended `templates/components/message_bubble.templ` with `parseOrderDetails` helper supporting `order_json` metadata, direct metadata attributes, and fallback body parsing.
  - Rendered clean white container bubbles featuring a catalog badge (🛒 **Pedido do Catálogo** `<catalog_id>`), formatted item details table (SKU, Qty, Price), customer note callout box (amber styling), total price, currency badge, and timestamp.

- **Outbound Product Catalog Cards**:
  - Implemented `parseProductDetails` helper to process single product (`product`) and multi-product (`product_list`) payloads.
  - Rendered dark/zinc catalog cards with catalog ID badge (📦 **Catálogo de Produtos**), header/section titles, total sections indicator, item SKUs list with price tags, footer, and delivery status checkmarks.

- **Repository Extensions & Unit Testing**:
  - Extended `repository.ThreadMessage` struct in `internal/repository/audit.go` with `Metadata map[string]string` field and updated `ListThreadByContact` to scan JSONB metadata into `ThreadMessage`.
  - Added full Templ HTML unit test coverage in `templates/components/message_bubble_test.go` verifying order summary cards, single product cards, multi-product cards, and text fallback bubbles.

## Deviations

None.

## Self-Check: PASSED
