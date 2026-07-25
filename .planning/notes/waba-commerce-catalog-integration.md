---
title: WABA Commerce & Catalog Integration Architecture
date: 2026-07-25
context: Exploration of WhatsApp Commerce & Catalog Messages for PerGo WABA engine
---

# WABA Commerce & Catalog Integration Architecture

## Overview
WhatsApp Commerce allows businesses to showcase single products (`product`) or multi-product catalog sections (`product_list`) directly inside chat. Users can browse products, build a shopping cart, and place orders directly to the business.

To maintain PerGo's low-friction developer experience philosophy, PerGo stores `default_catalog_id` in WABA Connection metadata, accepts simple SKU references for dispatches, and normalizes complex Meta `order` webhooks into structured `order.created` events.

## Connection Metadata & Default Catalog

PerGo WABA connections store `default_catalog_id` in connection credentials/settings.
When dispatching product messages, developers can omit `catalog_id`, and PerGo automatically injects the connection's `default_catalog_id` (with optional per-request override).

## Dispatch Payload Formats (`POST /messages`)

### Single Product Message (`type: "product"`)
```json
{
  "to": "5511999999999",
  "channel": "loja-waba",
  "type": "product",
  "sku": "CAMISA_AZUL_M",
  "body": "Destaque da semana com 20% de desconto!"
}
```

### Multi-Product Message (`type: "product_list"`)
```json
{
  "to": "5511999999999",
  "channel": "loja-waba",
  "type": "product_list",
  "body": "Monte seu combo de bebidas:",
  "sections": [
    {
      "title": "Sucos Naturais",
      "skus": ["SUCO_LARANJA_500", "SUCO_UVA_500"]
    }
  ]
}
```

## Incoming Order Webhook Normalization (`order.created`)

When a customer submits a shopping cart order on WhatsApp, Meta delivers an incoming webhook containing product items, quantities, prices, and optional notes.

PerGo normalizes the payload and emits an `order.created` event to client webhooks:
```json
{
  "event": "order.created",
  "message_id": "wamid.HBgL...",
  "from": "5511999999999",
  "catalog_id": "9876543210",
  "text": "Por favor entregar sem gelo",
  "items": [
    {
      "sku": "SUCO_LARANJA_500",
      "quantity": 2,
      "unit_price": 12.50,
      "currency": "BRL"
    }
  ],
  "total_amount": 25.00
}
```
