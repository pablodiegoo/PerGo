# Features Research: WABA Deep Integration

## Template Management

### Table Stakes
- **Full Lifecycle CRUD & Meta Sync**: Ability to create, list, retrieve status, and delete templates programmatically via Meta Graph API and admin UI. Local database synchronization mirroring remote Meta template state.
- **Complete Component Support**:
  - `HEADER`: Text (max 1 variable), Image, Video, Document, or Location.
  - `BODY`: Rich text formatted message (`*bold*`, `_italic_`, `~strikethrough~`, `` `code` ``) with sequential variables (`{{1}}`, `{{2}}`).
  - `FOOTER`: Static plain text (max 60 characters).
  - `BUTTONS`: Quick Replies (up to 3), Call-to-Action (URL with dynamic tail variable `{{1}}`, Phone Number), and Copy Code (for OTP/Authentication).
- **Status & Rejection Lifecycle Tracking**: Tracking states (`APPROVED`, `PENDING`, `REJECTED`, `PAUSED`, `DISABLED`). Ingesting Meta rejection reasons directly into the DB and console for operator visibility.
- **Meta Category Alignment**: Strict classification under Meta's template categories (`MARKETING`, `UTILITY`, `AUTHENTICATION`).
- **Multi-Locale / Language Variants**: Storing and dispatching locale-specific template variants (e.g., `en_US`, `pt_BR`, `es_ES`) under a single template identifier with fallback handling.

### Differentiators
- **Automated Webhook Status & Quality Score Telemetry**: Real-time webhook notifications when template status changes or when quality rating drops (`GREEN`, `YELLOW`, `RED`), preventing account flags before outbound campaigns fail.
- **Strict Parameter Schema & Pre-flight Validation**: Enforcing JSON Schema or typed parameters for template variables at the API boundary, rejecting invalid parameter counts or malformed inputs *before* making HTTP requests to Meta.
- **Automated Session Window Fallback Routing**: When an outbound freeform message fails due to an expired 24h window, automatically falling back to an approved default Utility Template or alternative provider (SMS/Telegram).
- **Interactive Visual Previewer & Live Interpolation**: Admin UI rendering showing exact WhatsApp chat bubble visuals, dynamic button layout, and media header placeholders prior to Meta submission.
- **Multi-WABA Drift Detection & Auto-Import**: Syncing templates across multiple WABA accounts, detecting local vs remote definition drift, and auto-importing existing templates created directly in Meta Business Manager.

---

## Commerce Catalogs

### Table Stakes
- **Single-Product Messages (SPM)**: Dispatching `type: "product"` interactive messages containing `catalog_id` and `product_retailer_id` with a "View" button opening the product overlay in WhatsApp.
- **Multi-Product Messages (MPM)**: Dispatching `type: "product_list"` interactive messages featuring up to 30 items organized into titled sections (`sections[].product_items[]`).
- **Catalog Link / Storefront Message**: Sending catalog messages (`type: "catalog_message"`) or templates with interactive "View Catalog" buttons opening the full business storefront inside WhatsApp.
- **Catalog Pre-flight Verification**: Validating that `catalog_id` is linked to the WABA and checking SKU format before dispatching product messages.

### Differentiators
- **Native Order Webhook Processing Engine**: Ingesting native WhatsApp `order` webhooks (triggered when customers add items to cart and submit order in chat) and parsing them into standardized JSON order payloads for ERP/CRM integration.
- **Real-Time Stock & Price Check Integration**: Querying internal database/ERP stock levels prior to dispatching Multi-Product Messages to prevent presenting out-of-stock items.
- **Abandoned Cart Flow Triggers**: Combining catalog product payloads with WhatsApp Flows or Utility Templates for automated cart recovery.
- **Multi-Currency & Regional Formatting**: Automatic price and currency formatting based on recipient locale and Meta Commerce Manager settings.

---

## Meta Flows

### Overview & Architecture
WhatsApp Flows are native, multi-screen interactive forms embedded directly inside WhatsApp chat. They eliminate external webview redirects, delivering high-conversion UX for appointment booking, lead generation, customer surveys, and product customization.

### Static Mode vs. Dynamic Endpoint Mode
- **Static Mode (No Endpoint)**:
  - Flow screens, input components, and routing logic are entirely defined in a static Flow JSON hosted by Meta.
  - Zero server requests occur during user navigation between screens.
  - Submitted data is delivered via the completion `nfm_reply` webhook.
  - Ideal for static lead capture, surveys, and fixed registration.
- **Dynamic Mode (Data Exchange Endpoint)**:
  - Flow communicates with a business-hosted HTTPS endpoint during screen transitions.
  - Enables real-time server-side validation, dynamic lookup (e.g., available booking slots, user account lookup), and dynamic screen content.
  - **Crypto & Security Requirement**: Requests and responses exchanged with Meta must be encrypted/decrypted using RSA 2048-bit key pairs and AES-256-GCM encryption.

### `nfm_reply` Webhook Payload Structure
When a user completes a Flow, Meta sends an interactive webhook message of type `interactive` with subtype `nfm_reply`:

```json
{
  "type": "interactive",
  "interactive": {
    "type": "nfm_reply",
    "nfm_reply": {
      "name": "flow",
      "body": "Sent",
      "response_json": "{\"customer_name\":\"John Doe\",\"booking_date\":\"2026-08-01\",\"slot\":\"14:00\"}"
    }
  }
}
```
*Key Implementation Detail*: `response_json` is an **escaped stringified JSON string**. CPaaS platforms must automatically parse this string into a structured native map for API consumers.

### Table Stakes
- **Flow Interactive Dispatch**: Supporting outbound messages with `interactive.type = "flow"`, `flow_id`, `flow_token`, `flow_action`, and `flow_action_payload`.
- **`nfm_reply` Webhook Ingestion & Auto-Parsing**: Automatically deserializing stringified `response_json` into native JSON payloads in incoming message webhooks.
- **Flow JSON Schema Validation**: Validating local Flow JSON structures prior to Meta publishing.

### Differentiators
- **Built-in Data Exchange Middleware & Crypto Handler**: Providing built-in Go middleware for RSA key pair generation, request decryption, signature verification, and response encryption for Dynamic Flow endpoints.
- **Flow Token State Tracking & Context Correlation**: Tracking `flow_token` throughout customer interactions to link completed form submissions back to specific session states, support tickets, or marketing campaigns.
- **Low-Code Form Builder to Flow JSON Translator**: Converting standard JSON forms or HTML form definitions into Meta WhatsApp Flow JSON syntax.

---

## Session Window Enforcement

### 24-Hour Customer Service Window Mechanics
- **Trigger**: The 24-hour window opens immediately when a customer sends a message or interacts with the business.
- **Window Reset**: Every incoming message from the user resets the 24-hour timer.
- **Inside 24h Window**: Freeform session messages (text, media, audio, document, location, interactive buttons, product lists) can be sent without template pre-approval.
- **Outside 24h Window**: Freeform messages are **strictly rejected** by Meta API (Error code 131047 / 470). ONLY pre-approved Template Messages (`UTILITY`, `MARKETING`, `AUTHENTICATION`) can be sent.
- **72-Hour Free Entry Points**: Conversations initiated via Click-to-WhatsApp ads or Facebook Page CTAs open a **72-hour free window** where freeform and template messages incur no per-message fees.
- **2025/2026 Pricing Alignment**: Meta enforces per-message pricing based on template category. Service messages within open windows are tracked per conversation session.

### Table Stakes
- **Session Tracking Database**: Storing per-contact `last_user_message_at` timestamps in PostgreSQL.
- **Pre-flight Out-of-Window Block**: Checking window validity before issuing freeform outbound requests, returning HTTP 422/429 with actionable error details (`SESSION_WINDOW_CLOSED`) instead of forwarding invalid requests to Meta.

### Differentiators
- **Smart Template Auto-Upgrade / Fallback**: If an outbound freeform message is attempted outside the 24h window, automatically mapping the payload into an approved default Utility Template rather than dropping the message.
- **Window Expiration Webhooks**: Emitting `session.expiring_soon` events (e.g., at 23h mark) to alert support agents or trigger re-engagement bots before the window closes.
- **Cross-Channel Fallback**: Automatically re-routing messages to SMS or Telegram when the WhatsApp 24h window is closed and no appropriate template exists.
- **72h Free Ad Window Tracking**: Tracking free entry point attribution to optimize campaign messaging costs.

---

## Template Validation Rules

Local pre-flight validation rules matching Meta Cloud API constraints:

1. **Parameter / Variable Constraints**:
   - Must use numeric sequential placeholders: `{{1}}`, `{{2}}`, `{{3}}`.
   - Cannot skip numbers (e.g., `{{1}}` and `{{3}}` without `{{2}}` is invalid).
   - Variables must be numeric indices inside braces; named variables (e.g. `{{name}}`) or spaces inside braces (e.g. `{{ 1 }}`) are invalid.
   - Variables cannot be placed at the very start or very end of the body text if it is the sole text.
   - Variables cannot be adjacent (e.g. `{{1}}{{2}}` is invalid; must be separated by text or space).
   - Parameter value length: Max 15 characters recommended per parameter value.

2. **Component Character Limits**:
   - **Template Name**: Max 512 characters. Lowercase alphanumeric and underscores only (`^[a-z0-9_]+$`).
   - **Header Text**: Max 60 characters. Max 1 variable (`{{1}}`). Rich formatting (`*bold*`, etc.) NOT allowed in text headers.
   - **Body Text**: Max 1024 characters.
   - **Footer Text**: Max 60 characters. Plain text only; no variables or rich formatting allowed.
   - **Quick Reply Button Text**: Max 25 characters per button.
   - **Call-to-Action (CTA) Button Text**: Max 25 characters per button.

3. **Button Configuration Rules**:
   - Maximum 3 buttons total for standard templates (Quick Reply or CTA).
   - Max 3 Quick Reply buttons.
   - Max 2 CTA buttons: maximum 1 Phone Number button and maximum 1 URL button.
   - URL button dynamic suffix: max 1 variable allowed only at the end of the URL (e.g., `https://example.com/orders/{{1}}`).

4. **Formatting Rules**:
   - Body text allows max 2 consecutive newlines (`\n\n`).
   - Supported inline styling: `*bold*`, `_italic_`, `~strikethrough~`, `` `monospace` ``.

5. **Category Selection**:
   - Must be one of `UTILITY`, `MARKETING`, or `AUTHENTICATION`.

---

## Sources

- **Meta for Developers**: WhatsApp Business Management API Documentation (Template Management & Validation).
- **Meta for Developers**: WhatsApp Cloud API Interactive Messages & Catalog Integration.
- **Meta for Developers**: WhatsApp Flows Specification & Data Exchange Encryption.
- **Twilio Docs**: Content Template Builder & WhatsApp Commerce API.
- **MessageBird (Bird) Docs**: WhatsApp Flows, Template Sync & Session Window Rules.
- **Vonage API Reference**: WhatsApp Template Management & Business Solutions.
