---
title: Implement WABA Meta Flows Dispatch and Webhook Response Decoding
date: 2026-07-25
priority: high
tags: [waba, interactive, meta-flows, nfm]
---

# Implement WABA Meta Flows Dispatch and Webhook Response Decoding

## Context
Meta Flows allow native in-app form experiences on WhatsApp. Spike 026 established the initial Go data model and parser for Meta Flows. This task implements full production support for static Flow dispatches and automatic `nfm_reply` response JSON decoding.

## Implementation Tasks

1. **Dispatch Transformer (`ToMetaJSON`)**:
   - Update `internal/channel/waba` transformer to handle `type: "flow"`.
   - Ensure `flow_message_version: "3"` and `action.name: "flow"` are set correctly.
   - Auto-generate `flow_token` using `google/uuid` if empty.

2. **Incoming Webhook Parser (`nfm_reply`)**:
   - In WABA webhook handler, detect `interactive.type == "nfm_reply"`.
   - Unmarshal `interactive.nfm_reply.response_json` into a generic `map[string]any`.
   - Construct normalized `flow_response` payload and publish to outbound webhook event stream.

3. **Unit & Integration Tests**:
   - Test `type: "flow"` transformation to Meta v25.0 payload format.
   - Test decoding `nfm_reply` JSON string with nested fields into structured event object.
