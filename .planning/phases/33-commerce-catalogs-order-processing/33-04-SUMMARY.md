---
phase: 33
plan_id: "33-04"
subsystem: commerce
tags: [whatsapp, order, webhook, nats, deduplication]
key-files:
  - internal/channel/whatsapp/waba_inbound.go
  - internal/inbound/processor.go
  - internal/api/handler/waba_webhook.go
  - internal/api/handler/waba_webhook_test.go
  - internal/channel/whatsapp/waba_test.go
---

# Phase 33 Plan 33-04 Summary: Inbound Webhook Parser & order.created Event Emission

Parsed Meta WhatsApp order webhooks (`messages[].type == "order"`), performed idempotent `wamid` deduplication via `InboundDedupRepository`, and published normalized `order.created` (`domain.EventTypeOrderCreated`) events to workspace webhooks and JetStream topics.

## Tasks Summary

| Task | Description | Status | Commit |
| --- | --- | --- | --- |
| T1 | Inbound Order Webhook Parsing in WABAInboundAdapter | Completed | `0125902` |
| T2 | Publish Order Created Events & Idempotent Wamid Deduplication | Completed | `c8a3876` |
| T3 | Inbound Order Webhook Integration Unit Tests | Completed | `92ea283` |

## Key Changes

1. **`internal/channel/whatsapp/waba_inbound.go`**:
   - Extended `Messages` struct in `ValueData` to include Meta order payload (`catalog_id`, `text`, `product_items` with SKU, quantity, item_price, currency).
   - Added parsing logic for `msg.Type == "order"`, calculating order total price $\sum (\text{qty} \times \text{price})$, constructing human-readable `Body` summary, and attaching raw order JSON in `Metadata["order_json"]`.
   - Added robust `parseQuantity` and `parsePrice` helper functions.

2. **`internal/inbound/processor.go`**:
   - Added `PublishOrderCreated(ctx context.Context, workspaceID uuid.UUID, ev *domain.OrderCreatedEvent) error` method for publishing `order.created` events to NATS subject `inbound.events.<workspace_id>`.
   - Added `DedupRepo()` getter method to `InboundProcessor`.
   - Updated `Process` method to respect pre-checked deduplication state (`ev.Metadata["deduplicated"] == "true"`).

3. **`internal/api/handler/waba_webhook.go`**:
   - Added detection for inbound order events (`event.Metadata["type"] == "order"`).
   - Implemented `wamid` deduplication check via `dedupRepo.InsertAndCheck`.
   - On unique `wamid`, deserialized `domain.OrderCreatedEvent` from `Metadata["order_json"]` and published normalized `order.created` event.
   - On duplicate `wamid`, logged and suppressed duplicate `order.created` event emissions.

4. **Tests**:
   - Added `TestWABAInboundAdapter_OrderParsing` in `internal/channel/whatsapp/waba_test.go`.
   - Added `TestWABAWebhook_OrderDeduplication` integration test in `internal/api/handler/waba_webhook_test.go` verifying end-to-end order parsing, deduplication, and NATS event emission.

## Deviations from Plan

None.

## Verification Results

- `go test -v -race ./internal/api/handler/... ./internal/inbound/... ./internal/channel/whatsapp/...` passed clean.

## Self-Check: PASSED
