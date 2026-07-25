# Project Requirements

## WABA Session Window & Delivery Status Requirements

### REQ-WABA-24H: 24-Hour Session Window Validation
- **Description**: The WABA channel handler must validate the 24-hour session window prior to dispatching non-template messages (text, media, interactive buttons/lists/flows).
- **Behavior**: If the contact has no incoming message recorded within the last 24 hours, the API must reject the request immediately with HTTP 422 `session_window_expired` and an informative message directing the client to use a WABA template message instead.

### REQ-WABA-STATUS: Delivery Status Webhook Translation
- **Description**: Incoming Meta delivery status webhooks (`sent`, `delivered`, `read`, `failed`) must be correlated to the internal dispatch ID and forwarded to the workspace webhook URL.
- **Behavior**: Meta numerical error codes (e.g., 131047, 131026) must be translated into standardized, human-readable error reasons (`session_window_expired`, `phone_not_on_whatsapp`, `payment_required`) in the payload.

## WABA Meta Flows Requirements

### REQ-WABA-FLOW-SEND: Simplified Meta Flow Dispatch
- **Description**: The API must allow dispatching Meta Flows via a clean `type: "flow"` payload with `flow_id`, `flow_cta`, `flow_screen`, and `flow_data`.
- **Behavior**: The WABA channel transformer constructs the Meta Graph API v25.0 payload using `flow_message_version: "3"` and auto-generates a UUID `flow_token` if omitted.

### REQ-WABA-FLOW-DECODE: Automatic Flow Response Webhook Decoding
- **Description**: Incoming `nfm_reply` webhooks from completed Meta Flows must be parsed and decoded automatically.
- **Behavior**: The escaped JSON string in `response_json` is unmarshaled into a clean `data` map and emitted as a `type: "flow_response"` event to the client webhook.

## WABA Commerce & Catalog Requirements

### REQ-WABA-CATALOG-SEND: Simplified Product & Catalog Dispatch
- **Description**: The API must allow dispatching single products (`type: "product"`) and multi-product lists (`type: "product_list"`) by specifying product SKUs.
- **Behavior**: PerGo automatically resolves the `catalog_id` from connection metadata if omitted, and transforms the request into Meta Graph API v25.0 interactive product payloads.

### REQ-WABA-ORDER-WEBHOOK: Normalized Shopping Cart Order Webhooks
- **Description**: Incoming shopping cart orders placed by customers in WhatsApp must be parsed into normalized `order.created` events.
- **Behavior**: The PerGo webhook handler parses items, quantities, prices, currency, and customer notes into a structured `order.created` JSON event.

## WABA Template Management & Status Requirements

### REQ-WABA-TEMPLATE-WEBHOOK: Real-time Template Status Webhooks
- **Description**: The system must process Meta system webhooks (`message_template_status_update`) for template approval, rejection, and policy changes.
- **Behavior**: Upon receiving a status update webhook, PerGo updates the local `waba_templates` record and emits a normalized `template.status_updated` event to client webhooks.

### REQ-WABA-TEMPLATE-SYNC: On-Demand Template Synchronization
- **Description**: Operators and client applications must be able to trigger a full template synchronization from Meta Cloud API v25.0 on demand via `POST /admin/devices/:id/templates/sync`.
- **Behavior**: PerGo queries Meta Graph API `GET /v25.0/{waba_id}/message_templates`, upserts all definitions into `waba_templates`, and returns a summary JSON object.

## WABA Template CRUD & Lifecycle Requirements

### REQ-WABA-TEMPLATE-CREATE: Template Creation with Local Validation
- **Description**: The API must allow creating new WABA message templates via `POST /api/v1/workspaces/:ws/connections/:conn/templates` with full local validation before submission to Meta Graph API.
- **Behavior**: PerGo validates the template payload locally (category correctness, body length ≤1024 chars, variable placeholder format `{{N}}` sequencing, header media sample presence, button URL format, footer length ≤60 chars, language code validity) and returns structured validation errors immediately. Only after local validation passes does PerGo submit to Meta Graph API `POST /v25.0/{waba_id}/message_templates` and persist the template with status `PENDING`.
- **Categories**: `MARKETING`, `UTILITY`, `AUTHENTICATION` — each with category-specific validation rules.

### REQ-WABA-TEMPLATE-EDIT: Template Editing with Version Tracking
- **Description**: The API must allow editing existing approved templates via `PUT /api/v1/workspaces/:ws/connections/:conn/templates/:name` with the same local validation as creation.
- **Behavior**: When an approved template is edited, Meta creates a new pending version while the current version remains active. PerGo must track both versions separately in `waba_templates` (e.g., v1 APPROVED, v2 PENDING). The API returns both the active version and the pending edit. Local validation is applied identically to creation before submission.

### REQ-WABA-TEMPLATE-DELETE: Template Deletion
- **Description**: The API must allow deleting templates via `DELETE /api/v1/workspaces/:ws/connections/:conn/templates/:name` with optional language-specific deletion.
- **Behavior**: PerGo calls Meta Graph API `DELETE /v25.0/{waba_id}/message_templates?name={name}` (or with `hsm_id` for specific language variants), soft-deletes the local `waba_templates` record, and emits a `template.deleted` event to client webhooks.

### REQ-WABA-TEMPLATE-VALIDATE: Standalone Template Validation Endpoint
- **Description**: The API must expose a dry-run validation endpoint `POST /api/v1/workspaces/:ws/connections/:conn/templates/validate` that runs the full local validation suite without submitting to Meta.
- **Behavior**: Returns a structured response with `valid: true/false` and an array of validation errors with field paths, enabling integrators and UI builders to pre-validate before submission.

## WABA Reactions Requirements

### REQ-WABA-REACTION-SEND: Send Reactions via WABA Cloud API
- **Description**: The API must allow sending emoji reactions to existing messages via `POST /messages` with `type: "reaction"`, specifying the target `message_id` (WAMID) and `emoji`.
- **Behavior**: PerGo constructs the Meta Graph API payload `{ "type": "reaction", "reaction": { "message_id": "<WAMID>", "emoji": "👍" } }`. Setting `emoji` to empty string removes a previous reaction. The 24-hour session window validation does NOT apply to reactions (Meta allows reactions outside the window).

### REQ-WABA-REACTION-WEBHOOK: Incoming Reaction Webhook Processing
- **Description**: Incoming reaction events from contacts must be parsed and forwarded to the workspace webhook URL as normalized `message.reaction` events.
- **Behavior**: The WABA inbound adapter detects `type: "reaction"` in the webhook payload, extracts `reaction.message_id` and `reaction.emoji` (empty emoji = reaction removed), correlates to the original dispatch, and emits a `type: "reaction"` event with `target_message_id`, `emoji`, and `contact` fields.

## WABA Connection Setup Requirements

### REQ-WABA-WEBHOOK-AUTO: Automatic Webhook URL Registration on Connection Setup
- **Description**: When a WABA connection is saved with valid credentials (`phone_number_id`, `access_token`, `waba_account_id`), PerGo must automatically register its webhook callback URL with Meta's Graph API, eliminating the manual step in Meta Developer Console.
- **Behavior**: On connection save, PerGo calls `POST /v25.0/{app_id}/subscriptions` with the instance's webhook URL and verify token. On connection deletion, PerGo tears down the subscription. If auto-registration fails (e.g., insufficient permissions), the connection is still saved but a warning is surfaced to the operator with manual setup instructions.

## WABA Catalog Query Requirements

### REQ-WABA-CATALOG-QUERY: Retrieve Business Catalog Products
- **Description**: The API must expose `GET /api/v1/workspaces/:ws/connections/:conn/catalog` to list products from the business's WhatsApp Commerce catalog via Meta Graph API.
- **Behavior**: PerGo queries `GET /v25.0/{catalog_id}/products` (resolving `catalog_id` from connection metadata) and returns a paginated list of products with fields: `retailer_id`, `name`, `description`, `price`, `currency`, `image_url`, `availability`. Supports cursor-based pagination and optional filtering by availability.

### REQ-WABA-CATALOG-COLLECTIONS: Retrieve Catalog Collections
- **Description**: The API must expose `GET /api/v1/workspaces/:ws/connections/:conn/catalog/collections` to list product collections from the business catalog.
- **Behavior**: PerGo queries `GET /v25.0/{catalog_id}/product_sets` and returns collection names, IDs, and product counts. This enables integrators to compose `type: "product_list"` messages by selecting from existing collections.

## Connection Test Requirements

### REQ-CONN-TEST-VERIFY: Credential Verification (Read-Only)
- **Description**: The API must expose `POST /api/v1/workspaces/:ws/connections/:conn/test/verify` to validate that connection credentials are correct without sending any message.
- **Behavior**: PerGo calls a read-only provider API appropriate to the channel type and returns `{ "status": "ok", "provider_info": {...} }` on success or `{ "status": "error", "reason": "..." }` on failure. Per-channel strategies:
  - **WABA**: `GET /v25.0/{phone_number_id}` — returns business profile info
  - **Telegram**: `getMe` — returns bot identity
  - **WhatsApp Web**: Check whatsmeow session connected state
  - **Email (SMTP)**: `EHLO` handshake without sending
  - **Email (SES)**: `GetSendQuota` API call
  - **Mautic**: `GET /api/contacts?limit=1` with API key

### REQ-CONN-TEST-SEND: End-to-End Test Message Dispatch
- **Description**: The API must expose `POST /api/v1/workspaces/:ws/connections/:conn/test/send` to send a real test message through the full pipeline, requiring a `destination` parameter from the operator.
- **Behavior**: PerGo dispatches a channel-appropriate test message and returns the dispatch result including delivery status. Per-channel strategies:
  - **WABA**: Sends the pre-approved `hello_world` template to the specified phone number
  - **Telegram**: Sends a "PerGo test message ✅" text to the specified chat_id
  - **WhatsApp Web**: Sends a simple text message to the specified phone number
  - **Email**: Sends a branded test email to the specified email address
- **Constraint**: Rate-limited to 1 test per connection per minute to prevent abuse.


