---
status: "changes_requested"
files_reviewed: 3
critical: 1
warning: 1
info: 1
total: 3
---

# Code Review: Phase 25 (JSON-to-Protobuf mapping)

## Summary
The JSON-to-Protobuf mapping for WhatsApp Web (`whatsmeow`) and the generic WABA mapping have been implemented. The domain schema successfully abstracts the interactive messages and handles validation per D-01. However, the WABA adapter completely misses the `fallback_behavior` (D-03) degradation logic that was correctly implemented in the `whatsmeow` adapter.

## Critical Issues

1. **Missing `fallback_behavior` logic in WABA adapter (`waba.go`)**
   - **Location:** `internal/channel/whatsapp/waba.go:278` (where `reqPayload.Interactive` is built)
   - **Issue:** Unlike `adapter.go` which checks `len(m.Interactive.Action.Buttons) > 3` and degrades to a text message if `fallback_behavior == "degrade"`, the WABA adapter blindly passes all buttons and sections to Meta's API. Meta will reject payloads with >3 buttons or >10 sections with an HTTP 400. This results in a terminal failure, completely ignoring the `"degrade"` fallback directive. 
   - **Fix:** Implement the same pre-dispatch limit checks (e.g., > 3 buttons or > 10 sections) in `waba.go` and convert the payload to a standard text message if `m.FallbackBehavior != "fail"`.

## Warnings

1. **Inconsistent Handling of Simultaneous `Media` and `Interactive` payloads**
   - **Location:** `internal/channel/whatsapp/waba.go:334` & `adapter.go:106`
   - **Issue:** If a client sends both `Media` and `Interactive` in the same request:
     - `adapter.go` prioritizes `Interactive` and ignores S3 `Media`.
     - `waba.go` prioritizes `Media` (since it hits the `if m.Media != nil` block and immediately returns `a.sendRequest(...)`) and ignores `Interactive`.
   - **Fix:** While providing both might be considered invalid usage, the adapters should either behave consistently (e.g., prioritize `Interactive` in both) or `domain.ValidateMessage` should explicitly reject requests containing both `Media` and `Interactive`.

## Information / Notes

1. **`channel_overrides` integration**
   - **Location:** `waba.go:176` and `adapter.go:265`
   - **Note:** The complete replacement pattern (D-02) is cleanly implemented in both adapters. For WABA, it drops the JSON directly into the request body. For WhatsApp Web, it successfully leverages `protojson.Unmarshal` to parse directly into a `waE2E.Message`.
