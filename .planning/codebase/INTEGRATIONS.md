# Integrations Map

This document catalogs all external integrations, communication brokers, databases, and third-party APIs used by PerGo.

## 1. Database (PostgreSQL)

PerGo utilizes PostgreSQL v16 as its system of record.
- **Access Patterns**: Executed using `pgx/v5` pgxpool directly (no ORM). Standard SQL queries are written inline.
- **Key Tables**:
  - `workspaces`: Multi-tenant workspace isolation boundaries.
  - `api_keys`: SHA-256 hashed keys for client authorization.
  - `connections`: Credentials for WhatsApp, WABA, and Telegram connections.
  - `audit_logs`: Partitioned history of all inbound, outbound, and internal events.
  - `recipient_sessions`: Maps conversation state, last read timestamps, and recipient isolation details.

## 2. Event Broker (NATS JetStream)

NATS JetStream acts as the durability boundary between ingestion and dispatch.
- **Outbound Stream (`MESSAGES`)**:
  - Configured with `WorkQueuePolicy` retention, meaning a message is processed once and removed upon ACK.
  - Subscribed to `messages.>` subject.
  - **Backpressure**: Enforces a limit of 1000 in-flight messages (`MaxMsgs`). Rejects new publications (`DiscardNew`) with an HTTP 429 once full.
- **Webhook Stream (`WEBHOOKS`)**:
  - Configured with `LimitsPolicy` to store incoming events for webhook distribution.
  - Subscribed to `webhooks.>` subject.

## 3. WhatsApp Web Adapter (whatsmeow)

Unofficial multi-device WhatsApp integration via WhatsApp Web client protocol.
- **Session Store**: Persists session credentials and keys. Uses custom column encryption on `whatsmeow_device` table (AES-256-GCM) to prevent plain-text storage of device keys.
- **Ingestion**: Event handlers listening to Whatsmeow dispatch loops parse messages and delegate to `InboundProcessor` (`internal/session/inbound_processor.go`).
- **QR Login**: Renders interactive QR codes over WebSocket / HTTP polling in the admin console.

## 4. WhatsApp Cloud API (WABA)

Official Meta WhatsApp Business platform.
- **Outbound**: Handled by posting payloads to Graph API endpoint `/v18.0/{phone_number_id}/messages` with system user access token.
- **Inbound Webhook**: Configured at `/api/v1/webhooks/waba`, validates hub.challenge signature, decodes change items, and writes them to the DB audit log.
- **Template Querying**: Syncs template definitions from Meta Graph API `/v18.0/{waba_id}/message_templates`.

## 5. Telegram Bot API

Telegram bot channel adapter.
- **Outbound**: Uses Telegram Send API (`/bot{token}/sendMessage`, `/sendPhoto`, etc.).
- **Inbound Webhook**: Receives updates at `/api/v1/webhooks/telegram/{connection_id}`. Employs a custom validator that fetches and caches the bot's username via `getMe` during token configuration to populate the `"to"` field in the database without performing dynamic HTTP requests on webhook intake.

## 6. Webhooks Delivery & DLQ

- **Event Forwarder**: Pulls consumed events from NATS JetStream `WEBHOOKS` stream and posts them to registered client endpoints.
- **Dead-Letter Queue (DLQ)**: Retries webhook deliveries up to 5 times. On ultimate failure, logs payload to the `webhook_dlq` table for manual recovery in the admin console.

## 7. Storage Integration (S3 compatible)

- **Storage Client**: `internal/platform/storage/s3.go` connects to AWS S3 or MinIO.
- **Media Download/Upload**: Incoming media URLs are downloaded, size-validated (max 25MB), content-hashed, and stored in S3 at `{workspace_id}/{hash}.{ext}`.
- **Media Proxy**: Exposes media files securely at `/media/{workspace_id}/{hash}.{ext}`.
