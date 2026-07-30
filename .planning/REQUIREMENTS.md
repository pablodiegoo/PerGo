# Requirements: PerGo

**Defined:** 2026-07-26
**Core Value:** A single API request delivers a message through any configured channel with automatic fallback — without per-message markup, without vendor lock-in, and with full custody of transaction data on infrastructure you control.

## v1.7 Requirements

Requirements for milestone v1.7 WABA Deep Integration. Each maps to roadmap phases.

### Template Management

- [x] **TMPL-01**: Operator can create WABA message templates via REST API and admin UI with header, body, footer, and button components
- [x] **TMPL-02**: Operator can list, view, edit, and delete templates with local PostgreSQL cache mirroring Meta state
- [x] **TMPL-03**: System syncs templates on-demand from Meta Graph API with operator-initiated sync button (rate-limited to once per 15 minutes)
- [x] **TMPL-04**: System stores and dispatches locale-specific template variants (e.g., en_US, pt_BR) under a single template name
- [x] **TMPL-05**: System tracks template lifecycle status (APPROVED, PENDING, REJECTED, PAUSED, DISABLED) via `message_template_status_update` webhooks
- [x] **TMPL-06**: System stores rejection reasons from Meta and displays them in admin console
- [x] **TMPL-07**: System tracks template quality score (GREEN, YELLOW, RED) and alerts operators when rating drops
- [x] **TMPL-08**: Admin UI renders visual template previewer showing WhatsApp-style chat bubble with interpolated parameters
- [x] **TMPL-09**: System exposes REST API endpoints for template CRUD: POST/GET/PUT/DELETE `/api/v1/waba/templates`

### Template Dispatch

- [x] **DISP-01**: User can send template messages via `POST /messages` with `type: "template"`, specifying template name, language, and parameters
- [x] **DISP-02**: System validates template parameters locally before dispatch (parameter count, character limits, button config, category rules)
- [x] **DISP-03**: System blocks dispatch for templates with `status != APPROVED` with actionable error response
- [x] **DISP-04**: System auto-upgrades freeform messages to a configured default template when 24h session window is expired (smart fallback)

### Session Window

- [x] **SESS-01**: System tracks per-contact `last_inbound_at` timestamps in `contact_sessions` table updated on every inbound WABA message
- [x] **SESS-02**: System rejects non-template WABA messages outside 24h window with HTTP 422 `session_window_expired` at API ingestion
- [x] **SESS-03**: WABA worker re-validates session window at dispatch time with 5-minute safety buffer (23h55m cutoff) to account for queue latency
- [x] **SESS-04**: System emits `session.expiring_soon` webhook event at the 23-hour mark for active sessions
- [x] **SESS-05**: System tracks 72h free entry point windows for Click-to-WhatsApp ad-initiated conversations

### Commerce Catalogs

- [x] **COMM-01**: User can send single-product messages (`type: "product"`) with `catalog_id` and `product_retailer_id` via `POST /messages`
- [x] **COMM-02**: User can send multi-product list messages (`type: "product_list"`) with titled sections via `POST /messages`
- [x] **COMM-03**: Operator can configure `default_catalog_id` per WABA connection in connection settings
- [x] **COMM-04**: System parses inbound WhatsApp order webhooks (`messages[].type == "order"`) into normalized `order.created` events with idempotent processing
- [x] **COMM-05**: System validates `catalog_id` binding and SKU existence before dispatching product messages

### Meta Flows

- [x] **FLOW-01**: User can dispatch Meta Flows via `POST /messages` with `type: "flow"`, specifying `flow_id`, `flow_token`, `flow_action`, and `flow_action_payload`
- [x] **FLOW-02**: System auto-parses inbound `nfm_reply` responses using two-stage JSON decoding (escaped string → structured map) into webhook events
- [x] **FLOW-03**: System generates HMAC-signed `flow_token` containing workspace context, recipient phone, nonce, and short TTL to prevent tampering
- [x] **FLOW-04**: System provides Data Exchange endpoint middleware with RSA 2048-bit / AES-256-GCM encryption for dynamic flow screen transitions

## Future Requirements

Deferred to v1.8+. Tracked but not in current roadmap.

### Advanced Commerce

- **COMM-F01**: Real-time stock & price check integration before dispatching product messages
- **COMM-F02**: Abandoned cart flow triggers combining catalog products with Utility Templates
- **COMM-F03**: Multi-currency and regional price formatting based on recipient locale

### Advanced Flows

- **FLOW-F01**: Low-code form builder to Flow JSON translator
- **FLOW-F02**: Flow token state tracking correlating completed submissions to campaigns/tickets

### Advanced Templates

- **TMPL-F01**: Multi-WABA drift detection and auto-import of templates created in Meta Business Manager
- **TMPL-F02**: Interactive visual previewer with live parameter interpolation and button layout simulation

## Out of Scope

| Feature | Reason |
|---------|--------|
| WhatsApp Payments integration | Separate Meta product with different API surface; not core messaging |
| Flow Builder / visual designer | PerGo is a backend router; flow design happens in Meta's Flow Builder |
| Catalog product database sync | Meta Commerce Manager handles catalog data; PerGo routes messages, not manages inventory |
| Template approval acceleration | Meta's ML review pipeline is opaque; no API to influence approval speed |
| SMS fallback for expired sessions | Cross-channel fallback is a routing engine concern, not WABA-specific; deferred |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| SESS-01 | Phase 30 | Complete |
| SESS-02 | Phase 30 | Complete |
| SESS-03 | Phase 30 | Complete |
| SESS-04 | Phase 30 | Complete |
| SESS-05 | Phase 30 | Complete |
| TMPL-01 | Phase 31 | Complete |
| TMPL-02 | Phase 31 | Complete |
| TMPL-03 | Phase 31 | Complete |
| TMPL-04 | Phase 31 | Complete |
| TMPL-05 | Phase 31 | Complete |
| TMPL-06 | Phase 31 | Complete |
| TMPL-07 | Phase 31 | Complete |
| TMPL-08 | Phase 31 | Complete |
| TMPL-09 | Phase 31 | Complete |
| DISP-01 | Phase 32 | Complete |
| DISP-02 | Phase 32 | Complete |
| DISP-03 | Phase 32 | Complete |
| DISP-04 | Phase 32 | Complete |
| FLOW-01 | Phase 32 | Complete |
| FLOW-02 | Phase 32 | Complete |
| FLOW-03 | Phase 32 | Complete |
| FLOW-04 | Phase 32 | Complete |
| COMM-01 | Phase 33 | Complete |
| COMM-02 | Phase 33 | Complete |
| COMM-03 | Phase 33 | Complete |
| COMM-04 | Phase 33 | Complete |
| COMM-05 | Phase 33 | Complete |

**Coverage:**
- v1.7 requirements: 27 total
- Mapped to phases: 27
- Unmapped: 0 ✓

---
*Requirements defined: 2026-07-26*
*Last updated: 2026-07-26 after initial definition*
