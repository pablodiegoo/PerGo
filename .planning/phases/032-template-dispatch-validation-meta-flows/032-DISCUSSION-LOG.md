# Phase 032: Template Dispatch, Validation Engine & Meta Flows - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-26
**Phase:** 032-template-dispatch-validation-meta-flows
**Areas discussed:** Smart Session Window Fallback, Template Parameter Validation & Rejection Behavior, Meta Flows Dispatch & Data Exchange Security, Inbound Flow Response Decoding

---

## Smart Session Window Fallback (DISP-04)

| Option | Description | Selected |
|--------|-------------|----------|
| Connection-level default template | Auto-upgrade freeform message to default_template_name (passing body as {{1}}) if configured on connection; otherwise return HTTP 422 session_window_expired | ✓ |
| Strict rejection | Never auto-upgrade freeform messages outside the 24h window; always return HTTP 422 requiring type: "template" | |
| Header-driven opt-in | Auto-upgrade only if request header or payload flag is explicitly passed | |

**User's choice:** Connection-level default template fallback.
**Notes:** Auto-upgrade freeform message to `default_template_name` (passing body as `{{1}}`) if `default_template_name` is configured on the connection; otherwise return HTTP 422 `session_window_expired`.

---

## Template Parameter Validation & Rejection Behavior (DISP-01, DISP-02, DISP-03)

| Option | Description | Selected |
|--------|-------------|----------|
| Flexible parameters & synchronous HTTP 422 pre-flight | Support both array ["John", "101"] and map {"1": "John"} formats; validate against local template cache and reject non-APPROVED or missing templates synchronously at API ingestion before queueing | ✓ |
| Strict array parameters & pre-flight check | Support positional array parameters only; reject non-APPROVED or invalid parameter templates synchronously at ingestion | |
| Asynchronous worker validation | Accept message into queue immediately (HTTP 202) and let WABA worker reject / DLQ if template is non-APPROVED or invalid | |

**User's choice:** Flexible parameters & synchronous HTTP 422 pre-flight validation.
**Notes:** Validates template existence, parameter counts/limits, and status against local `WABATemplateRepository` cache synchronously before queueing into NATS.

---

## Meta Flows Dispatch & Data Exchange Security (FLOW-01, FLOW-03, FLOW-04)

| Option | Description | Selected |
|--------|-------------|----------|
| Per-connection credentials + Env Fallback | Store RSA 2048-bit private key in connection credentials JSON (encrypted via AES-256-GCM at rest), falling back to WABA_FLOWS_PRIVATE_KEY env var; auto-generate HMAC-SHA256 flow_token with 24h TTL | ✓ |
| Global Environment Key only | Read RSA private key exclusively from environment variable (WABA_FLOWS_PRIVATE_KEY_PEM) for all connections | |
| Per-workspace key management | Store RSA private key in workspace configuration settings | |

**User's choice:** Per-connection credentials + Env Fallback.
**Notes:** Store RSA 2048-bit private key in connection credentials JSON (encrypted at rest), fallback to `WABA_FLOWS_PRIVATE_KEY` env var; auto-generate HMAC-SHA256 `flow_token` with 24h TTL if omitted.

---

## Inbound Flow Response Decoding (`nfm_reply`) (FLOW-02)

| Option | Description | Selected |
|--------|-------------|----------|
| Normalized flow.completed webhook & Inbox rendering | Perform 2-stage JSON unmarshalling into structured map, emit normalized flow.completed event to workspace webhooks, and render formatted form response summary in Chat UI bubble | ✓ |
| Raw payload pass-through | Unmarshal response_json into map but emit generic message.received event without creating a dedicated flow.completed webhook event | |
| Dual event emission | Emit both flow.completed and message.received webhook events for full backward compatibility | |

**User's choice:** Normalized flow.completed webhook & Inbox rendering.
**Notes:** Perform 2-stage unmarshalling on `nfm_reply.response_json` into a structured map, emit normalized `flow.completed` event to workspace webhooks, and render form summary in Chat UI.

---

## the agent's Discretion

- Exact helper struct definitions for parameter validation.
- JSON error formatting for HTTP 422 pre-flight responses.

## Deferred Ideas

- None — discussion stayed strictly within phase scope.
