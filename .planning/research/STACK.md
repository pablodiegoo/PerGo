# Stack Research: WABA Deep Integration

## Key Findings

1. **Zero New External Dependencies Required:**
   The existing PerGo stack—Go 1.22+ stdlib (`net/http`, `encoding/json`, `time`), `pgx/v5` (PostgreSQL), `Echo v5` (HTTP router), `NATS JetStream` (broker), `log/slog` (logging), and `a-h/templ + HTMX` (admin console)—is 100% sufficient to implement all v1.7 WABA Deep Integration features. No third-party Meta/WhatsApp SDK is needed or recommended.

2. **Meta Graph API Versioning Strategy:**
   - Default to **Meta Graph API `v25.0`** (released February 2026).
   - Parameterize the API version string dynamically in base URL construction (e.g. `https://graph.facebook.com/%s/`, defaulting to `v25.0` or overridden via `META_GRAPH_API_VERSION` env var / connection config).
   - Meta deprecates Graph API versions on a 2-year rolling cycle; dynamic versioning prevents hardcoded breaking changes across Meta API upgrades.

3. **Template CRUD Lifecycle & Local Storage:**
   - Template operations interface directly with `/{waba_id}/message_templates` (Create, List, Delete) and `/{template_id}` (Edit).
   - Local caching in PostgreSQL (`waba_templates` table via `internal/repository/waba_template.go`) provides instant validation and template selection UI without making blocking Graph API calls during message ingestion.
   - Status updates are kept in sync asynchronously via `message_template_status_update` webhooks (`APPROVED`, `REJECTED`, `PAUSED`, `DISABLED`).

4. **24-Hour Customer Service Window (CSW) Enforcement:**
   - Free-form messages (text, media, interactive, commerce, flows) can only be sent within 24 hours of the customer's last inbound message. Outside this window, Meta rejects non-template messages.
   - Pre-flight validation checks `recipient_sessions` table (`last_inbound_at`). If expired (`time.Since(last_inbound_at) > 24*time.Hour`) and message type is not `"template"`, PerGo immediately rejects ingestion with HTTP 422 (`code: "CSW_EXPIRED"`), saving Meta API quota and backpressure queue capacity.

5. **Commerce & Flow Message Mapping:**
   - Commerce single-product (`type: "product"`) and multi-product (`type: "product_list"`) use Meta's `type: "interactive"` JSON envelope with `action.catalog_id` and `action.sections`.
   - Customer catalog orders arrive via inbound webhooks with `messages[].type == "order"`, containing `order.catalog_id` and `order.product_items`.
   - Meta Flows (`type: "flow"`) are sent inside `type: "interactive"` with `action.name = "flow"` and parameters specifying `flow_id`, `flow_token`, `flow_action` (`navigate` or `data_exchange`), and `flow_action_payload`.
   - User flow responses arrive in webhooks as `type: "interactive"` with `interactive.type == "nfm_reply"`, containing an escaped/stringified JSON string in `interactive.nfm_reply.response_json`.

---

## Meta Graph API Endpoints

All Meta Graph API requests target `https://graph.facebook.com/v25.0/` using `Authorization: Bearer <WABA_ACCESS_TOKEN>`.

### 1. Template Management Endpoints

| Method | Graph API Path | Purpose | Key Parameters / Payload |
| :--- | :--- | :--- | :--- |
| **GET** | `/{waba_id}/message_templates` | List WABA Templates | `fields=id,name,status,category,language,components,rejected_reason`, `limit`, `after`, `status`, `category` |
| **GET** | `/{template_id}` | Fetch Single Template | Retrieves full component definition and status for a template ID |
| **POST** | `/{waba_id}/message_templates` | Create Template | `name`, `category` (`MARKETING`, `UTILITY`, `AUTHENTICATION`), `language`, `components` (array of `HEADER`, `BODY`, `FOOTER`, `BUTTONS`) |
| **POST** | `/{template_id}` | Edit Template | `category`, `components`. *(Allowed when status is `APPROVED`, `REJECTED`, or `PAUSED`. Max 1 edit/24h, up to 10 edits/month)* |
| **DELETE** | `/{waba_id}/message_templates` | Delete Template | `name={template_name}` (deletes all languages) or `hsm_id={template_id}` (deletes specific ID) |

### 2. Message Dispatch Endpoints

| Message Type | Graph API Path | Payload `type` | Payload Structure Summary |
| :--- | :--- | :--- | :--- |
| **Template** | `/{phone_number_id}/messages` | `template` | `{ "messaging_product": "whatsapp", "to": "...", "type": "template", "template": { "name": "...", "language": { "code": "..." }, "components": [...] } }` |
| **Single Product** | `/{phone_number_id}/messages` | `interactive` | `{ "messaging_product": "whatsapp", "to": "...", "type": "interactive", "interactive": { "type": "product", "body": { "text": "..." }, "action": { "catalog_id": "...", "product_retailer_id": "..." } } }` |
| **Product List** | `/{phone_number_id}/messages` | `interactive` | `{ "messaging_product": "whatsapp", "to": "...", "type": "interactive", "interactive": { "type": "product_list", "header": { "type": "text", "text": "..." }, "body": { "text": "..." }, "action": { "catalog_id": "...", "sections": [...] } } }` |
| **Meta Flow** | `/{phone_number_id}/messages` | `interactive` | `{ "messaging_product": "whatsapp", "to": "...", "type": "interactive", "interactive": { "type": "flow", "header": {...}, "body": {...}, "action": { "name": "flow", "parameters": { "flow_message_version": "3", "flow_token": "...", "flow_id": "...", "flow_cta": "...", "flow_action": "navigate", "flow_action_payload": { "screen": "...", "data": {...} } } } } }` |

---

## Data Models Required

### 1. Template CRUD & Caching Models

```go
// WABATemplate represents a stored WhatsApp message template in PostgreSQL.
type WABATemplate struct {
	ID             uuid.UUID       `json:"id"`
	WorkspaceID    uuid.UUID       `json:"workspace_id"`
	ConnectionID   uuid.UUID       `json:"connection_id"`
	MetaTemplateID string          `json:"meta_template_id"`
	Name           string          `json:"name"`
	Language       string          `json:"language"`
	Status         string          `json:"status"`   // "APPROVED", "PENDING", "REJECTED", "PAUSED", "DISABLED"
	Category       string          `json:"category"` // "MARKETING", "UTILITY", "AUTHENTICATION"
	Components     json.RawMessage `json:"components"`
	RejectedReason string          `json:"rejected_reason,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}
```

### 2. Commerce Catalog & Order Webhook Models

```go
// InteractiveProduct payload for single product dispatches.
type InteractiveProduct struct {
	CatalogID         string `json:"catalog_id"`
	ProductRetailerID string `json:"product_retailer_id"`
}

// InboundOrderPayload maps inbound messages[].order webhooks.
type InboundOrderPayload struct {
	CatalogID    string      `json:"catalog_id"`
	Text         string      `json:"text,omitempty"`
	ProductItems []OrderItem `json:"product_items"`
}

type OrderItem struct {
	ProductRetailerID string  `json:"product_retailer_id"`
	Quantity          int     `json:"quantity"`
	ItemPrice         float64 `json:"item_price"`
	Currency          string  `json:"currency"`
}
```

### 3. Meta Flows & Response Decoding Models

```go
// FlowParameters defines payload parameters for type: "flow".
type FlowParameters struct {
	FlowMessageVersion string             `json:"flow_message_version"` // "3"
	FlowToken          string             `json:"flow_token"`
	FlowID             string             `json:"flow_id"`
	FlowCTA            string             `json:"flow_cta"`
	FlowAction         string             `json:"flow_action"` // "navigate" or "data_exchange"
	FlowActionPayload  *FlowActionPayload `json:"flow_action_payload,omitempty"`
}

// NFMReplyPayload represents inbound interactive response for Meta Flows.
type NFMReplyPayload struct {
	Name         string `json:"name"`          // "flow"
	Body         string `json:"body"`          // "Sent" or custom button text
	ResponseJSON string `json:"response_json"` // Stringified JSON string! Must be decoded.
}
```

---

## Dependencies Assessment

| Area | Requirement | Existing Stack Package | New Dependency Needed? |
| :--- | :--- | :--- | :--- |
| **HTTP Communication** | Meta Graph API REST calls | Stdlib `net/http` + `context` | ❌ None |
| **JSON Processing** | Payload encoding & `nfm_reply` stringified JSON parsing | Stdlib `encoding/json` | ❌ None |
| **Database Persistence** | Local template cache & session windows | `github.com/jackc/pgx/v5` | ❌ None |
| **API Endpoints** | Template CRUD REST API | `github.com/labstack/echo/v5` | ❌ None |
| **Asynchronous Worker** | JetStream queue dispatch for templates/commerce/flows | `github.com/nats-io/nats.go` | ❌ None |
| **Structured Logging** | Trace-correlated logs for Meta API requests & webhooks | Stdlib `log/slog` | ❌ None |
| **Admin UI Render** | Template management interface & status badges | `github.com/a-h/templ` + `HTMX` | ❌ None |

**Verdict:** 100% existing stack coverage. Zero new dependencies required.

---

## Integration Points

1. **`internal/channel/whatsapp/waba.go` (WABAAdapter):**
   - Extend outbound payload construction to handle `type: "template"`, `type: "product"`, `type: "product_list"`, and `type: "flow"`.
   - Implement HTTP client methods for Meta Graph API template CRUD.

2. **`internal/domain/message.go`:**
   - Add template parameters, commerce product/product_list payloads, and flow action parameters to message structs.

3. **`internal/repository/waba_template.go` & `recipient_session.go`:**
   - PostgreSQL persistence for template cache and session window tracking.

4. **`internal/webhook/waba_inbound.go`:**
   - Extend inbound webhook handling for `message_template_status_update`, `order`, and `nfm_reply`.

5. **`internal/api/template_handler.go`:**
   - REST endpoints for operator template management.

---

## Sources

- **Meta Graph API Reference (v25.0):** WhatsApp Business Management API - Message Templates
- **Meta Cloud API Interactive Messages:** Send Interactive Messages
- **Meta Cloud API Commerce Catalog Messages:** Single and Multi-Product Messages
- **Meta Flows Protocol & Webhook Specs:** WhatsApp Flows - Receiver Endpoint & Webhooks
- **PerGo Internal Codebase:** `internal/channel/whatsapp/waba.go`, `internal/domain/message.go`
