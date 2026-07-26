# Pitfalls Research: WABA Deep Integration

**Domain:** WABA Deep Integration (Template Management, Commerce Catalogs, Meta Flows, Session Window Enforcement, Adapter Integration)
**Target Architecture:** PerGo Self-Hosted CPaaS (Go 1.22+, Echo v5, NATS JetStream, PostgreSQL via pgx/v5, whatsmeow, WABA Cloud API)
**Researched:** 2026-07-25
**Confidence:** HIGH (Derived from official Meta WhatsApp Cloud API specs, Graph API error code documentation, Meta Flows Data API specs, Meta Commerce Manager documentation, and PerGo codebase inspection).

---

## Template Management Pitfalls

### 1. Meta Approval Delays & Asynchronous Status Synchronization
* **Severity:** HIGH
* **What goes wrong:** Template creation via Meta Graph API (`POST /{waba_account_id}/message_templates`) only returns an initial HTTP response with status `PENDING`. Automated Machine Learning models evaluate template copy; templates flagged for policy review enter manual queuing that takes **24 to 48 hours**. Attempting to send messages using a `PENDING`, `REJECTED`, or `PAUSED` template fails with Graph API Error `100` / `132001` (`Template does not exist` / `Template is disabled`).
* **Why it happens:** CPaaS platforms that store template state synchronously on HTTP response creation assume instant availability. Without listening to asynchronous status webhooks, the local database remains out of sync with Meta's true status.
* **How to avoid:**
  1. Implement a dedicated webhook handler for `message_template_status_update` events sent to the WABA webhook endpoint. Update local `waba_templates` status to `APPROVED`, `REJECTED`, `PAUSED`, `DISABLED`, or `IN_APPEAL` asynchronously.
  2. Block outbound dispatch at the API layer if `waba_templates.status != 'APPROVED'`. Return clear HTTP 422 errors (`Template not approved by Meta: status = PENDING`) instead of attempting Graph API calls.
  3. Store Meta's `rejection_reason` from webhook payloads in `waba_templates.rejection_reason` so operators can inspect rejections directly in the PerGo console.

### 2. Template Versioning, Editing, & Parameter Structure Gotchas
* **Severity:** CRITICAL
* **What goes wrong:** Meta does **not** support numerical template versioning (e.g. `v1`, `v2`). Editing an existing template in Meta Commerce Manager/Business Manager overwrites the live template definition and triggers a full re-review. During re-review, outbound messages using that template name fail or fail parameter validation.
* **Why it happens:** Meta templates consist of rigid component arrays (`HEADER`, `BODY`, `FOOTER`, `BUTTONS`). Placeholder syntax in body text is strictly ordinal (`{{1}}`, `{{2}}`). If an application sends 3 parameters for a template registered with 2 placeholders, Meta returns Error `132000` / `132007` (`Parameter count mismatch`).
* **How to avoid:**
  1. Treat templates as **immutable** in PerGo. Instead of editing an active template name, create new template names (e.g., `order_update_v2`) and deprecate the old ones.
  2. Implement local JSON schema validation against stored component parameter definitions before enqueuing template dispatch. Verify parameter count, parameter types (text vs media link vs currency), and component ordering.
  3. Enforce explicit language and locale scoping (`language.code`, e.g. `en_US` vs `en` vs `pt_BR`). Requesting `"en"` when only `"en_US"` is approved throws Error `132001`.

### 3. Rate Limits & Throttling on Template Management API
* **Severity:** MEDIUM
* **What goes wrong:** Polling Meta's Graph API to check approval status across hundreds of templates quickly exhausts Graph API rate limits, triggering HTTP 429.
* **Why it happens:** Graph API applies Business Manager and App-level rate limits (typically 200 calls/hour per user). Automated polling loops violate these limits under multi-tenant scale.
* **How to avoid:**
  1. Rely **100% on webhooks** (`message_template_status_update`) for template lifecycle transitions.
  2. Provide an operator-initiated "Sync Templates" button with strict per-workspace rate limiting (max once per 15 minutes).
  3. Inspect Meta response headers (`x-business-use-case-usage`, `x-app-usage`) to apply backoff.

### 4. Category Restrictions & Automatic Re-Classification
* **Severity:** HIGH
* **What goes wrong:** Developers submit templates under lower-cost categories (e.g., `UTILITY`), but Meta's automated AI re-classifies them as `MARKETING` or rejects them for category mismatch. High user block/report rates cause Meta to lower the template quality rating (`GREEN` → `YELLOW` → `RED`), auto-pausing the template.
* **How to avoid:**
  1. Store Meta's assigned category (which may differ from requested category) on webhook updates.
  2. Implement template quality rating tracking. Alert operators when a template drops to `YELLOW` or `RED`.
  3. Strict validation for `AUTHENTICATION` templates: ensure body copy matches Meta's rigid pattern.

---

## Commerce Catalog Pitfalls

### 1. Catalog ID Resolution & Multi-Tenant Binding Failures
* **Severity:** CRITICAL
* **What goes wrong:** Outbound product messages fail or display broken cards, returning Graph API Error `#131009` (`Parameter value is invalid: catalog_id`).
* **Why it happens:**
  1. A Meta Commerce Manager catalog must be explicitly connected to the specific WABA account and Phone Number.
  2. In multi-tenant platforms, sending a product message with a `catalog_id` belonging to Workspace A over Workspace B's connection violates Meta permissions.
  3. Sending SKU identifiers that do not exist or are disabled causes payload rejection.
* **How to avoid:**
  1. Store `default_catalog_id` scoped to `(workspace_id, connection_id)` with explicit validation.
  2. Validate `product_retailer_id` references before constructing the WABA JSON request.

### 2. Order Webhook Reliability & Payload Unmarshaling
* **Severity:** HIGH
* **What goes wrong:** Incoming order webhooks fail to parse, resulting in dropped customer orders or double-processed purchases.
* **Why it happens:**
  1. `item_price` can be delivered as a string or number depending on Meta API versions.
  2. Meta guarantees **at-least-once webhook delivery**. Network latency can cause duplicate order events.
* **How to avoid:**
  1. Unmarshal `item_price` into a custom string/decimal parser to prevent floating-point inaccuracy.
  2. Enforce strict idempotency on incoming order webhooks using `wamid`.
  3. Return HTTP 200 OK to Meta **immediately** after enqueueing the raw order payload into NATS JetStream.

---

## Meta Flows Pitfalls

### 1. `flow_token` Security, Tampering, & Replay Attacks
* **Severity:** CRITICAL
* **What goes wrong:** Attackers intercept or manipulate `flow_token` parameters, allowing them to submit spoofed flow responses.
* **How to avoid:**
  1. Generate `flow_token` as an **HMAC-signed token** containing: `workspace_id`, `connection_id`, `recipient_phone`, `flow_id`, `session_nonce`, and `expiration_timestamp`.
  2. Verify the `flow_token` signature and validate that `recipient_phone` in the incoming webhook matches the token claims.

### 2. `nfm_reply` Parsing Edge Cases (JSON-in-JSON)
* **Severity:** HIGH
* **What goes wrong:** Incoming Flow completion webhooks cause Go unmarshaling errors or panics.
* **Why it happens:** `interactive.nfm_reply.response_json` is delivered as an **escaped JSON string**, NOT a nested JSON object. Attempting a single-pass `json.Unmarshal` into a Go struct fails.
* **How to avoid:**
  1. Use **two-stage unmarshaling**: first extract `response_json` as `string`, then run a second `json.Unmarshal` on `[]byte(response_json)`.
  2. Handle null/missing fields gracefully using pointers or `json.RawMessage`.

### 3. Version Compatibility, Endpoint SLAs, & Security Requirements
* **Severity:** HIGH
* **What goes wrong:** Flows using dynamic data exchange fail with inline user errors or Graph API send errors.
* **Why it happens:**
  1. **Strict 3.0-Second Endpoint SLA** for data exchange endpoints.
  2. **Draft Status Restriction:** Sending a `DRAFT` flow to real customers returns error `132000`.
  3. Meta periodically sends `{"action": "ping"}` health checks.
* **How to avoid:**
  1. Ensure Data Endpoint handlers execute in **< 500ms**.
  2. Verify `X-Hub-Signature-256` HMAC-SHA256 headers using constant-time comparison.
  3. Block outbound dispatch if `flow_status != 'PUBLISHED'`.

---

## Session Window Pitfalls

### 1. Timezone & UTC Offset Confusion
* **Severity:** HIGH
* **What goes wrong:** Free-form messages fail with Error `131047` or templates are unnecessarily sent when a session is actually open.
* **How to avoid:**
  1. Convert all incoming message timestamps to UTC immediately upon webhook ingestion.
  2. Store `last_inbound_at` as `TIMESTAMPTZ` (always UTC).
  3. Calculate window expiration as `last_inbound_at.Add(24 * time.Hour)`.

### 2. System Clock Drift & Boundary Race Conditions
* **Severity:** CRITICAL
* **What goes wrong:** Messages dispatched near the 24-hour mark are rejected by Meta because Meta's reference clock has elapsed.
* **How to avoid:**
  1. Enforce an internal **Safety Buffer** (5 minutes). Mark the window as "closed" at **23 hours and 55 minutes**.
  2. Automatically route through template fallback when near the boundary.

### 3. Queue Latency & Staggered Dispatch Boundary Violation
* **Severity:** CRITICAL
* **What goes wrong:** A free-form message is enqueued at hour 23:58 (session valid) but the worker dispatches at hour 24:02 due to queue backlog.
* **How to avoid:**
  1. Re-evaluate the 24-hour session window **at worker dispatch time** inside `WABAAdapter.Dispatch()`.
  2. If the session expired while waiting in the NATS JetStream queue, auto-convert to template payload or fail fast with `SESSION_EXPIRED_IN_QUEUE`.

### 4. 72-Hour Free Entry Point Window
* **Severity:** MEDIUM
* **What goes wrong:** System unnecessarily restricts messages during free entry windows from Click-to-WhatsApp ads.
* **How to avoid:**
  1. Track `entry_point_type` in sessions. If `ctwa`, extend window to **72 hours**.
  2. Expose `is_window_open` in the API response for CRM callers.

---

## Integration Pitfalls

### 1. Existing Adapter Breakage Risk
* **Severity:** CRITICAL
* **What goes wrong:** Adding WABA-specific features breaks the generic `channel.Dispatcher` interface or pollutes non-WABA adapters.
* **How to avoid:**
  1. Keep `channel.Dispatcher` lean and decoupled.
  2. Use WABA-specific payload transformation in the WABA adapter layer, not the core domain.
  3. Check capabilities at runtime before attempting rich feature dispatch.

### 2. Webhook Handler Routing Conflicts
* **Severity:** HIGH
* **What goes wrong:** A single WABA webhook endpoint receives multiple event types. Unknown sub-events get silently ignored.
* **How to avoid:**
  1. Implement a **Routing Discriminator** based on `entry[0].changes[0].field`.
  2. Route `nfm_reply` to Flow handler, `order` to Commerce handler, `message_template_status_update` to Template handler.

### 3. Database Migration Ordering
* **Severity:** HIGH
* **What goes wrong:** Migrations adding columns to high-write tables cause locks, blocking HTTP ingestion.
* **How to avoid:**
  1. Add new feature tables as independent tables with foreign keys.
  2. Use `CREATE INDEX CONCURRENTLY` in goose migrations.
  3. Run migrations before deploying new worker code.

---

## Prevention Strategies

1. **Worker-Level Dispatch Validation:** Always validate the 24h session window and template approval status at **dispatch time** with a 5-minute safety buffer.
2. **Two-Stage Unmarshaling Pattern:** Always parse Meta's stringified JSON fields using two-stage JSON unmarshaling.
3. **Idempotent Webhook Processing:** Enforce strict idempotency on `wamid` and `order_id` in PostgreSQL.
4. **Signed Cryptographic Flow Tokens:** Always issue HMAC-signed `flow_token` payloads.
5. **Independent New Tables:** Add `waba_templates`, `waba_sessions` as standalone tables, not columns on existing high-traffic tables.

---

## Sources

* Meta WhatsApp Cloud API Reference: `https://developers.facebook.com/docs/whatsapp/cloud-api/reference`
* Meta WhatsApp Cloud API Error Codes: `https://developers.facebook.com/docs/whatsapp/cloud-api/support/error-codes`
* Meta Message Templates Documentation: `https://developers.facebook.com/docs/whatsapp/message-templates`
* Meta WhatsApp Flows Developer Guide: `https://developers.facebook.com/docs/whatsapp/flows`
* Meta Commerce Manager & Product Catalog API: `https://developers.facebook.com/docs/whatsapp/solution-providers/commerce-settings`
* PerGo Codebase: `internal/channel/whatsapp/waba.go`, `internal/api/handler/waba_webhook.go`, `docs/architecture/02-technical-decisions.md`
