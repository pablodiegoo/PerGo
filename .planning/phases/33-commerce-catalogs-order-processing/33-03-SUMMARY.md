---
phase: 33
plan_id: "33-03"
subsystem: "channel/whatsapp"
tags:
  - whatsapp
  - waba
  - catalog
  - error-handling
key-files:
  - internal/channel/whatsapp/waba.go
  - internal/channel/whatsapp/waba_test.go
  - internal/channel/dispatcher.go
  - internal/platform/queue/orchestrator.go
---

# Plan 33-03 Summary: Outbound WABA Channel Adapter & Meta Error Mapping

## Task Summary Table

| Task ID | Task Title | Status | Commit Hash |
|---|---|---|---|
| T1 | Implement Interactive Product & Product List Payload Builders | Completed | `e357ee8` |
| T2 | Map Meta Catalog & SKU Error Codes to Terminal Errors | Completed | `e357ee8` |
| T3 | WABA Adapter Unit Tests | Completed | `e357ee8` |

## Summary of Changes

1. **Outbound Product Interactive Payload Formatters**:
   - Updated `wabaInteractive` and introduced `wabaProductAction`, `wabaProductSection`, and `wabaProductItem` to format Meta Graph API payloads for single-product (`type: "product"`) and multi-product list (`type: "product_list"`) messages.
   - Updated `channel.MessagePayload` and queue orchestrator to carry `Type` and `Product` payloads to channel adapters.

2. **Meta Commerce Error Mapping**:
   - Updated `WABAAdapter.classifyError` to map Meta error codes `131009` (invalid catalog ID) and `131084` (invalid product SKU) to terminal errors (`channel.NewTerminalError(...)`), preventing retry loops on unlinked catalogs or missing SKUs.

3. **Unit Tests**:
   - Added `TestWABAAdapter_ProductPayloads` to verify JSON serialization of single product and multi-product interactive payloads.
   - Added `TestWABAAdapter_MetaErrorClassification` to verify error code classification for codes `131009`, `131084`, `131030`, `131047`, and `130429`.

## Deviations
None.

## Verification
- Unit test suite (`go test -v -race ./internal/channel/whatsapp/...`) passed clean.

## Self-Check: PASSED
