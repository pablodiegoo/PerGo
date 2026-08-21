# 0012. WhatsApp Interactive & Meta Flow Engine Architecture

Status: accepted

## Context

PerGo operates as a headless CPaaS gateway providing unified messaging across WhatsApp Cloud (WABA), WhatsApp Web, Telegram, Instagram, and Email. While basic message routing and campaign broadcasting exist, rich interactive capabilities (Meta Flows, dynamic screen data exchange, interactive reply buttons, list menus, and commerce product catalogs) required architectural standardization to prevent tight coupling, database write bottlenecks, and cross-channel inconsistencies.

## Decisions

1. **Stateless Cryptographic Flow Tokens**: Meta Flow sessions use compact HMAC-SHA256 signed `flow_token` payloads encoding `(workspace_id, connection_id, contact_id, flow_id, exp)` with a default 7-day TTL. This eliminates database I/O bottlenecks during high-volume broadcasts (500+ msgs/sec) and prevents cross-tenant tampering without stateful session tracking in PostgreSQL.
2. **Synchronous Webhook Delegation for Flow Data Exchange**: PerGo's Flow Endpoint (`/api/v1/waba/flows/data-exchange`) acts as a pure cryptographic and protocol termination seam. It decrypts incoming RSA/AES-128-GCM payloads, forwards decrypted JSON synchronously to the workspace's configured `flow_webhook_url` within a 2500ms timeout budget, and re-encrypts the partner's JSON screen response for Meta. If the partner endpoint fails or times out, PerGo returns an encrypted fallback error screen (`{"screen": "ERROR", ...}`) to ensure a graceful WhatsApp client experience.
3. **Automated RSA Keypair Lifecycle**: WABA connections automatically generate an RSA 2048-bit keypair upon creation (persisted in credentials encrypted with AES-256-GCM at rest). The public key PEM is exportable via API (`GET /api/v1/connections/:id/flow-public-key`) and the admin console for upload to Meta Flow Builder, with optional manual PEM overrides.
4. **Dual Representation for Inbound Interactive Events**: Inbound interactive replies (`nfm_reply`, `button_reply`, `list_reply`, `order`) populate `event.Body` with human-readable markdown summaries for operator helpdesks (e.g. Chatwoot) while preserving rich typed JSON payloads in `event.Interactive` and `event.Metadata` for automated bots (Typebot) and webhooks.
5. **Configurable Cross-Channel Degradation**: Interactive messages dispatched to connections lacking native UI parity adhere to `fallback_behavior: "degrade" | "fail"`. By default (`degrade`), buttons and lists degrade to numbered text menus, while `fail` returns a terminal error.
6. **Deep Variable Interpolation in Broadcaster**: Campaign dispatches dynamically interpolate contact custom attributes and CSV overrides into text bodies, button titles, and `flow_action_payload` parameters.
