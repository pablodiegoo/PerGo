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
