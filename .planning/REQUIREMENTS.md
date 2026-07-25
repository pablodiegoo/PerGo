# Project Requirements

## WABA Session Window & Delivery Status Requirements

### REQ-WABA-24H: 24-Hour Session Window Validation
- **Description**: The WABA channel handler must validate the 24-hour session window prior to dispatching non-template messages (text, media, interactive buttons/lists/flows).
- **Behavior**: If the contact has no incoming message recorded within the last 24 hours, the API must reject the request immediately with HTTP 422 `session_window_expired` and an informative message directing the client to use a WABA template message instead.

### REQ-WABA-STATUS: Delivery Status Webhook Translation
- **Description**: Incoming Meta delivery status webhooks (`sent`, `delivered`, `read`, `failed`) must be correlated to the internal dispatch ID and forwarded to the workspace webhook URL.
- **Behavior**: Meta numerical error codes (e.g., 131047, 131026) must be translated into standardized, human-readable error reasons (`session_window_expired`, `phone_not_on_whatsapp`, `payment_required`) in the payload.
