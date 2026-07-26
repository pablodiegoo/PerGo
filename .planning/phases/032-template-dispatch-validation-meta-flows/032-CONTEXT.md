# Phase 32: Template Dispatch, Validation Engine & Meta Flows - Context

**Gathered:** 2026-07-26
**Status:** Ready for planning

<domain>
## Phase Boundary

Phase 32 delivers `POST /messages` support for `type: "template"`, local template parameter & approval validation, smart session-window fallback (auto-upgrading freeform messages outside the 24h window to a configured default template), `type: "flow"` dispatching with HMAC-SHA256 `flow_token` generation, RSA 2048-bit / AES-256-GCM Data Exchange endpoint middleware, and two-stage JSON decoding of `nfm_reply` inbound flow responses into `flow.completed` webhook events and formatted Chat UI summaries.

Implements DISP-01, DISP-02, DISP-03, DISP-04, FLOW-01, FLOW-02, FLOW-03, FLOW-04.
</domain>

<decisions>
## Implementation Decisions

### Smart Session Window Fallback (DISP-04)
- **D-01:** Connection-level default template fallback. When a freeform message (`type: "text"`, `"image"`, etc.) is submitted via `POST /messages` targeting a recipient whose 24h session window is expired (`IsWindowOpenBool` returns `false`), PerGo checks if `default_template_name` is configured on the target WABA connection.
- **D-02:** If `default_template_name` is present, PerGo automatically upgrades the request to a template dispatch, binding the freeform body text as parameter `{{1}}`. If `default_template_name` is NOT configured or parameter binding fails, PerGo returns HTTP 422 `session_window_expired`.

### Template Parameter Validation & Rejection Behavior (DISP-01, DISP-02, DISP-03)
- **D-03:** Flexible parameter formats. Support both positional array format (`parameters: ["John", "101"]`) and key-value map format (`parameters: {"1": "John", "2": "101"}`) in `POST /messages` template payloads.
- **D-04:** Synchronous HTTP 422 pre-flight validation. During `POST /messages` ingestion, validate the requested template against `WABATemplateRepository` in-memory cache:
  - If template does not exist: reject with HTTP 422 `template_not_found`.
  - If template `status != "APPROVED"` (e.g. `PENDING`, `REJECTED`, `PAUSED`): reject synchronously with HTTP 422 `template_not_approved` (including status and rejection reason in response).
  - If parameter count or character limits fail: reject with HTTP 422 `invalid_template_parameters`.
  - Messages passing pre-flight are enqueued into NATS JetStream for dispatch.

### Meta Flows Dispatch & Data Exchange Security (FLOW-01, FLOW-03, FLOW-04)
- **D-05:** RSA Private Key configuration. Store RSA 2048-bit private key in WABA connection credentials JSON (`private_key` field, encrypted at rest via AES-256-GCM), with global fallback to `WABA_FLOWS_PRIVATE_KEY` env var.
- **D-06:** HMAC-SHA256 `flow_token` generation. If `flow_token` is omitted in `POST /messages` (`type: "flow"`), PerGo auto-generates a signed HMAC-SHA256 token encoding `workspace_id`, `recipient_phone`, `flow_id`, timestamp, and a nonce with a 24h TTL.
- **D-07:** Data Exchange Middleware (`POST /api/v1/waba/flows/data-exchange`). Expose Echo handler/middleware handling RSA 2048-bit private key decryption of `encrypted_flow_data`, AES-256-GCM data payload decryption, and AES-256-GCM response payload encryption as required by Meta Flows architecture.

### Inbound Flow Response Decoding (`nfm_reply`) (FLOW-02)
- **D-08:** Two-stage JSON unmarshalling. Parse Meta's `nfm_reply.response_json` string inside `interactive.nfm_reply` webhook callbacks into a structured Go `map[string]interface{}`.
- **D-09:** Webhook Event Normalization & Chat UI summary. Emit a normalized `flow.completed` event to workspace webhook subscribers containing `screen`, `data`, `flow_token`, `contact_id`, and `wamid`. Render a formatted form response summary inside the Chat UI message bubble.

### agent's Discretion
- Exact naming of internal helper methods in `internal/channel/whatsapp/waba.go` and `internal/outbound/processor.go`.
- Struct tags and validation error response JSON formats for HTTP 422 pre-flight rejections.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

- `.planning/REQUIREMENTS.md` — Requirements DISP-01 to DISP-04 and FLOW-01 to FLOW-04
- `.planning/ROADMAP.md` — Phase 32 specification and success criteria
- `internal/repository/waba_template.go` — WABATemplateRepository with in-memory RWMutex cache (`GetByNameAndLanguage`)
- `internal/session/window.go` — ContactSessionChecker (`IsWindowOpenBool`)
- `internal/api/handler/message.go` — Ingestion handler for `POST /messages`
- `internal/outbound/processor.go` — Outbound dispatch pipeline and NATS queue listener
- `internal/channel/whatsapp/waba.go` — WABA channel adapter sending Meta Graph API payloads

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `WABATemplateRepository.GetByNameAndLanguage(ctx, connectionID, name, language)`: O(1) in-memory cache lookup for template validation during `POST /messages` ingestion.
- `ContactSessionChecker.IsWindowOpenBool(ctx, workspaceID, phone)`: Checks 24h session window status for smart fallback.
- `crypto.EncryptAESGCM` & `crypto.DecryptAESGCM`: Credentials encryption utilities in `internal/platform/crypto/`.

### Established Patterns
- **Pre-flight Ingestion Validation**: `POST /messages` validates auth, tenancy, payload structure, and queue depth synchronously before publishing to NATS JetStream.
- **WABA Client & Graph API**: `internal/client/waba_meta.go` and `internal/channel/whatsapp/waba.go` consume WABA credentials (`token`, `phone_number_id`, `waba_account_id`).

### Integration Points
- `internal/api/handler/message.go`: Add template & session window pre-flight checks.
- `internal/channel/whatsapp/waba.go`: Add `SendTemplate`, `SendFlow`, and `nfm_reply` handling.
- `internal/api/handler/waba_webhook.go`: Add `nfm_reply` two-stage parsing and `flow.completed` event emission.

</code_context>

<specifics>
## Specific Ideas

- Parameter binding: Support array `["val1", "val2"]` and map `{"1": "val1", "2": "val2"}`.
- Smart fallback: When `IsWindowOpenBool` returns `false`, check `wabaConn.DefaultTemplateName`. If set, automatically format message as template `type: "template"` with `{{1}}` = body.
</specifics>

<deferred>
## Deferred Ideas

- Visual drag-and-drop Flow builder (Out of Scope).
- Automatic Meta Commerce inventory sync (Out of Scope, deferred to v1.8+).

</deferred>

---

*Phase: 032-template-dispatch-validation-meta-flows*
*Context gathered: 2026-07-26*
