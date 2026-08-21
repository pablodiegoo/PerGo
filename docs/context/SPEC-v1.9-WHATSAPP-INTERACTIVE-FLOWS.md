# WhatsApp Interactive & Meta Flow Engine Architecture

## Problem Statement

When building omnichannel messaging workflows, backend developers and marketing operators on commercial platforms (Twilio, Take Blip, Botconversa, Wati) face critical limitations and excessive friction around interactive messaging:
1. **Meta Flows Complexity & Cryptographic Overhead**: Meta Flows require complex asymmetric RSA keypairs, AES-128-GCM payload decryption, IV inversion, and strict 3000ms latency SLAs for dynamic screen data exchange. Existing open-source tools lack a turnkey cryptographic termination endpoint, forcing developers to hand-roll cryptographic primitives or maintain heavy custom servers.
2. **Stateful Session Bottlenecks**: Managing flow session state across millions of broadcast recipients typically incurs high database read/write pressure, session table locks, and orphaned session records in PostgreSQL.
3. **Inbound Fragmentation & Operator Blindness**: Inbound interactive replies (`nfm_reply` for completed flows, `button_reply`, `list_reply`, and `order` from catalogs) arrive as raw provider JSON structures. When forwarded to human-in-the-loop helpdesks (such as Chatwoot) or conversational engines (such as Typebot), agents see empty or cryptic messages without human-readable summaries, while automated webhooks miss structured metadata.
4. **Multi-Channel Inconsistency**: Attempting to send interactive UI controls (like 3+ buttons or rich lists) to channels with differing native capabilities (WhatsApp Web via whatsmeow, Telegram, Instagram) either hard-crashes the worker or fails unpredictably without a deterministic degradation policy.
5. **Static Broadcasts**: Campaign broadcast tools fail to interpolate personalized contact attributes into interactive components (such as dynamic button labels, personalized list rows, or custom form metadata dictionaries in flow action payloads).

## Solution

Deliver a high-performance **WhatsApp Interactive & Meta Flow Engine** that turns PerGo into a zero-friction developer gateway and marketing broadcaster for interactive messaging:
1. **Stateless Cryptographic Flow Tokens**: Generate compact, tamper-proof HMAC-SHA256 `flow_token` values encoding workspace, connection, contact, flow ID, and expiration metadata. Enables high-volume broadcasting (500+ msgs/sec) with zero database session overhead.
2. **Turnkey Flow Data Exchange Termination & Webhook Delegation**: Expose a dedicated Flow Endpoint (`/api/v1/waba/flows/data-exchange`) that terminates RSA/AES-128-GCM crypto, synchronously delegates decrypted screen requests to the workspace's `flow_webhook_url` within a 2500ms SLA, and returns encrypted responses to Meta. If the partner webhook fails or times out, PerGo returns an encrypted fallback error screen (`{"screen": "ERROR", ...}`) to prevent client crashes.
3. **Automated WABA Connection RSA Keypair Management**: Auto-generate an RSA 2048-bit keypair on WABA connection creation (stored encrypted with AES-256-GCM in credentials), provide a public key export endpoint (`GET /api/v1/connections/:id/flow-public-key`) and admin console copy tool for Meta Flow Builder, and support custom PEM overrides.
4. **Dual Representation Inbound Event Normalization**: Inbound interactive replies populate `event.Body` with markdown human-readable summaries (e.g. `📄 *Form Submitted*\n- Name: John`) for operator visibility in Chatwoot, while preserving full typed JSON payloads in `event.Interactive` and `event.Metadata` for Typebot and webhooks.
5. **Graceful Cross-Channel Degradation**: Support configurable `fallback_behavior: "degrade" | "fail"`, automatically converting rich interactive buttons and lists into numbered text menus on channels lacking native multi-button support (e.g. WhatsApp Web) while offering strict error modes when desired.
6. **Deep Campaign Variable Interpolation**: Broadcaster Engine dynamically interpolates contact custom attributes and CSV variables into message bodies, interactive button titles, list section rows, and `flow_action_payload` dictionaries.

## User Stories

1. As a backend developer, I want to dispatch an interactive reply button message via `POST /api/v1/messages`, so that my customer receives clickable buttons in WhatsApp.
2. As a backend developer, I want to dispatch an interactive list menu with up to 10 section rows, so that my customer can select from a categorized menu.
3. As a backend developer, I want to dispatch a Meta Flow CTA message with a flow ID, CTA label, and custom initial screen payload, so that my customer can complete a native multi-screen form in WhatsApp.
4. As a system operator creating a WABA connection, I want PerGo to automatically generate an RSA 2048-bit keypair, so that I do not need to use the OpenSSL CLI to provision Meta Flow keys.
5. As a system operator, I want to view and copy the connection's public RSA key PEM from the admin console, so that I can paste it into Meta's Flow Builder dashboard.
6. As a backend developer, I want to retrieve the public key PEM via `GET /api/v1/connections/:id/flow-public-key`, so that my deployment scripts can automate Meta Flow registration via Graph API.
7. As a backend developer, I want PerGo to generate a stateless HMAC-SHA256 signed `flow_token` for each flow dispatch, so that flow exchanges carry verified contact, connection, and workspace identity without database lookup.
8. As a backend developer, I want PerGo to terminate RSA and AES-128-GCM encryption at `/api/v1/waba/flows/data-exchange` and forward decrypted JSON to my `flow_webhook_url`, so that my backend only handles plain JSON.
9. As a backend developer, I want PerGo to enforce a strict 2500ms timeout on synchronous flow webhook delegation, so that responses stay well within Meta's 3000ms latency window.
10. As a WhatsApp customer opening a flow whose webhook backend is temporarily down or slow, I want to see a friendly encrypted error screen, so that WhatsApp does not show an abrupt application crash.
11. As a WhatsApp customer opening a flow after the 7-day TTL has elapsed, I want to receive an encrypted `EXPIRED` screen, so that expired forms cannot be submitted.
12. As a support agent using Chatwoot, I want incoming Flow submissions (`nfm_reply`) to display a formatted markdown summary of submitted form fields, so that I immediately see the customer's input in the conversation timeline.
13. As a backend developer consuming webhooks, I want `nfm_reply` events to include the raw decrypted JSON dictionary in `event.Interactive.NFMReply.Data`, so that my automated CRM pipeline ingests structured form data.
14. As a support agent using Chatwoot, I want incoming button clicks (`button_reply`) and list selections (`list_reply`) to appear as clear textual selections in the chat window, so that I understand what option the customer picked.
15. As a bot builder using Typebot, I want button clicks and list selections to pass the selected button/row ID and title, so that the bot flow transitions seamlessly.
16. As a marketing manager launching a broadcast campaign, I want to include an Interactive Message with `{{name}}` and `{{balance}}` variables in button titles or list rows, so that each contact receives a personalized interactive interface.
17. As a marketing manager launching a campaign, I want custom contact attributes to interpolate into the `flow_action_payload` map, so that the Meta Flow opens pre-populated with contact-specific data.
18. As a system operator sending an interactive message to a WhatsApp Web (whatsmeow) connection, I want >3 buttons to automatically degrade to a numbered text list when `fallback_behavior` is `degrade`, so that messages deliver reliably without operator intervention.
19. As a backend developer requiring exact UI fidelity, I want to specify `fallback_behavior: "fail"` in `POST /api/v1/messages`, so that the API rejects or fails delivery if the channel cannot render native buttons.
20. As a compliance officer, I want all flow submissions and interactive replies to record audit logs in `audit_logs` with trace ID correlation, so that interactions are fully auditable.
21. As a backend developer, I want to query the interactive message schema and endpoint documentation via an OpenAPI 3.1 specification at `/api/openapi.yaml`, so that I can generate client SDKs.
22. As a system operator, I want an interactive developer sandbox at `/docs` powered by an embedded Scalar UI, so that I can test interactive payloads directly against my PerGo instance.

## Implementation Decisions

- **Stateless HMAC-SHA256 Flow Token Architecture**:
  - `flow_token` encodes `(workspace_id, connection_id, contact_id, flow_id, expires_at)` signed with the workspace secret using HMAC-SHA256.
  - Default TTL is 7 days; tokens expired upon arrival at `/data-exchange` trigger immediate encrypted `EXPIRED` response.
  - Zero database session table writes during broadcast dispatch.

- **Flow Data Exchange Seam & Resiliency**:
  - Endpoint: `POST /api/v1/waba/flows/data-exchange?connection_id=<uuid>`.
  - Protocol Flow:
    1. Decrypts `encrypted_aes_key` using connection RSA private key.
    2. Decrypts `encrypted_flow_data` with AES-128-GCM.
    3. Validates and parses `flow_token`.
    4. Forwards decrypted payload to `flow_webhook_url` via HTTP POST with `X-PerGo-Signature` HMAC header and 2500ms timeout context.
    5. Inverts IV (`crypto.InvertIV`).
    6. Encrypts webhook response JSON with AES-128-GCM using inverted IV.
    7. Returns base64 ciphertext to Meta.
  - On timeout / 5xx error from partner webhook, returns encrypted `{"screen": "ERROR", "data": {"error_message": "Service temporarily unavailable. Please try again later."}}`.

- **WABA Connection RSA Key Management**:
  - Automatically generate 2048-bit RSA keypair on connection creation if not supplied in credentials.
  - Persist private key in `connection.credentials` encrypted with AES-256-GCM.
  - Endpoint: `GET /api/v1/connections/:id/flow-public-key` returns `{"public_key_pem": "..."}`.
  - Admin UI provides copy button and manual PEM override field.

- **Inbound Event Dual Representation**:
  - `InboundChannelAdapter` and `waba_webhook.go` parse `nfm_reply`, `button_reply`, `list_reply`, and `order`.
  - `InboundEvent.Body` receives markdown-formatted summary (`📄 *Form Submitted*`, `🔘 *Selected*: ...`).
  - `InboundEvent.Interactive` receives typed struct with `Type`, `ButtonReply`, `ListReply`, `NFMReply`, and `Order`.
  - `InboundProcessor` triggers `PublishFlowCompleted` and `PublishOrderCreated` domain events.

- **Broadcaster Engine Deep Interpolation**:
  - `domain.ResolveCampaignPayload` recursively interpolates variables into:
    - Text bodies
    - `Interactive.Header.Text`, `Interactive.Body.Text`, `Interactive.Footer.Text`
    - `Interactive.Action.Buttons[].Reply.Title`
    - `Interactive.Action.Sections[].Rows[].Title` & `Description`
    - `Interactive.Action.Parameters.FlowActionPayload` map values

- **Cross-Channel Graceful Degradation**:
  - `fallback_behavior` field in `POST /api/v1/messages` (`"degrade"` vs `"fail"`).
  - WhatsApp Web adapter converts >3 buttons or >10 list rows to numbered text menus.
  - Telegram adapter converts interactive reply buttons into `inline_keyboard` with `callback_data`.

- **Schema & Configuration Additions**:
  - `workspaces`: add column `flow_webhook_url` (TEXT, NULLABLE).
  - `api/openapi.yaml`: document interactive schemas, flow endpoints, and public key endpoints.

## Testing Decisions

- **Testing Seam**: HTTP REST API + Inbound Webhook Level (highest possible seam).
- **Behavior-Focused Integration Tests**:
  - Flow Data Exchange: Test `POST /api/v1/waba/flows/data-exchange` end-to-end with real RSA/AES-128-GCM encrypted Meta request payloads, verifying webhook forwarding, inverted IV encryption, and fallback error screens on downstream timeout.
  - Inbound Normalization: Test `POST /api/v1/waba/webhook` with `nfm_reply` and `button_reply`, asserting on both formatted `Body` text in Chatwoot sync and structured JSON in InboundProcessor domain events.
  - Broadcaster Deep Interpolation: Test campaign dispatch with interactive button and flow payloads, asserting interpolated variables in NATS JetStream dispatched messages.
  - Cross-Channel Degradation: Test dispatch of >3 buttons to WhatsApp Web connection, asserting numbered text menu output when `degrade` is set, and `TerminalError` when `fail` is set.
- **Prior Art**: Follows patterns established in `internal/api/handler/flow_data_exchange_test.go`, `cmd/pergo/waba_status_receipts_test.go`, `internal/channel/whatsapp/waba_test.go`, and `internal/domain/campaign_test.go`.

## Out of Scope

- Visual drag-and-drop Meta Flow screen builder inside PerGo (flows are designed in Meta Flow Builder or external visual designers).
- Real-time catalog inventory synchronization with Shopify/WooCommerce (delegated to external commerce integrations).
- Interactive payment gateways (WhatsApp Pay / Meta Pay hosted checkout).

## Further Notes

- All terminology complies strictly with `CONTEXT.md`.
- Architectural decisions adhere to ADR-0012 (WhatsApp Interactive & Meta Flow Engine Architecture).
