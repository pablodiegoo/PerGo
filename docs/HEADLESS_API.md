# PerGo Headless CPaaS & Integration APIs Reference

This document describes the 100% programmatic headless REST APIs for PerGo, allowing external platforms (CRMs, ERPs, SaaS platforms, AI orchestrators) to provision tenants, manage connection lifecycles (including real-time QR pairing via SSE and polling), configure webhook subscriptions, and perform seamless Single Sign-On (SSO) operator hand-off into the native admin console.

---

## 1. Master Authentication & Workspace Provisioning

### 1.1 Authentication
The workspace provisioning endpoint is secured with **Master Authentication** using the configured `PERGO_MASTER_KEY` (or `PERGO_ADMIN_PASSWORD` as fallback).
Provide the key via the `Authorization` header:
```http
Authorization: Bearer <PERGO_MASTER_KEY>
```
or via the `X-Master-Key` header:
```http
X-Master-Key: <PERGO_MASTER_KEY>
```

### 1.2 Provision Workspace
Creates a new tenant workspace and automatically generates a default workspace API key and webhook signing secret.

* **Method & Path:** `POST /api/v1/workspaces`
* **Request Headers:**
  * `Authorization: Bearer <PERGO_MASTER_KEY>`
  * `Content-Type: application/json`

#### Request Body
```json
{
  "name": "Acme Corp",
  "generate_api_key": true,
  "generate_webhook_secret": true
}
```

#### Response (`201 Created`)
```json
{
  "id": "a5e8c1b2-3f4d-4e5a-8b9c-0d1e2f3a4b5c",
  "name": "Acme Corp",
  "api_key": "pgo_live_8f3d1b9a7c2e4f0a1b2c3d4e5f6a7b8c",
  "webhook_secret": "9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b3c2d1e0f9a8b",
  "created_at": "2026-08-15T00:00:00Z"
}
```

### 1.3 List Workspaces
Lists all configured tenant workspaces.

* **Method & Path:** `GET /api/v1/workspaces`
* **Query Parameters:**
  * `limit` (optional, integer, default: `50`): Maximum number of workspaces to return.
* **Request Headers:**
  * `Authorization: Bearer <PERGO_MASTER_KEY>` (or `X-Master-Key: <PERGO_MASTER_KEY>`)

#### Response (`200 OK`)
```json
{
  "workspaces": [
    {
      "id": "a5e8c1b2-3f4d-4e5a-8b9c-0d1e2f3a4b5c",
      "name": "Acme Corp",
      "pii_opt_in": false,
      "webhook_secret": "9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b3c2d1e0f9a8b",
      "created_at": "2026-08-15T00:00:00Z",
      "updated_at": "2026-08-15T00:00:00Z"
    }
  ]
}
```

---

## 2. Connection Lifecycle & QR Pairing APIs

All workspace-level endpoints require the workspace API key:
```http
Authorization: Bearer <WORKSPACE_API_KEY>
```

> **Note:** The endpoints are available under `/api/v1/connections` (canonical) and `/api/v1/devices` (retrocompatible alias).

### 2.1 Initiate Pairing (WhatsApp Web)
Starts an in-memory pairing session for WhatsApp Web (whatsmeow) and generates ephemeral QR codes.

* **Method & Path:** `POST /api/v1/connections/pair` (or `POST /api/v1/devices/pair`)

#### Request Body
```json
{
  "channel": "whatsapp",
  "phone": "5511999998888",
  "name": "WhatsApp Support",
  "proxy_url": ""
}
```

#### Response (`200 OK`)
```json
{
  "connection_id": "7b1c3d5e-9f8a-4b2c-8d1e-0a2b3c4d5e6f",
  "phone": "5511999998888",
  "status": "pairing_started",
  "message": "Sessão iniciada. Obtenha o QR code via polling ou stream SSE."
}
```

---

### 2.2 Get QR Code State (Polling)
Fetches the current QR code state for an active pairing session.

* **Method & Path:** `GET /api/v1/connections/:id/qr` (or `GET /api/v1/devices/:id/qr`, or with `?phone=5511...`)

#### Response (`200 OK` - Pending Scan)
```json
{
  "status": "pending",
  "code": "2@vJ8...base64rawstring...",
  "qr_data_url": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAA...",
  "pairing_code": "",
  "expires_at": "2026-08-15T00:01:25Z",
  "message": "Scan the QR code in WhatsApp",
  "connection_id": "7b1c3d5e-9f8a-4b2c-8d1e-0a2b3c4d5e6f"
}
```

#### Response (`200 OK` - Paired)
```json
{
  "status": "paired",
  "message": "device paired successfully",
  "connection_id": "7b1c3d5e-9f8a-4b2c-8d1e-0a2b3c4d5e6f"
}
```

---

### 2.3 Real-Time QR Code Stream (Server-Sent Events - SSE)
Streams pairing updates in real time using Server-Sent Events (SSE). Closes automatically upon successful scan or terminal error.

* **Method & Path:** `GET /api/v1/connections/:id/qr/stream` (or `GET /api/v1/devices/:id/qr/stream`)
* **Response Header:** `Content-Type: text/event-stream`

#### Stream Events
```text
event: qr
data: {"status":"pending","code":"2@abc...","qr_data_url":"data:image/png;base64,...","expires_at":"2026-08-15T00:01:25Z"}

event: paired
data: {"status":"paired","message":"device paired successfully","connection_id":"7b1c3d5e-9f8a-4b2c-8d1e-0a2b3c4d5e6f"}
```

---

### 2.4 List Connections
Returns all configured channels and active sessions for the workspace.

* **Method & Path:** `GET /api/v1/connections` (or `GET /api/v1/devices`)

#### Response (`200 OK`)
```json
{
  "connections": [
    {
      "id": "7b1c3d5e-9f8a-4b2c-8d1e-0a2b3c4d5e6f",
      "name": "WhatsApp Support",
      "slug": "whatsapp-support",
      "channel": "whatsapp",
      "sender_identity": "5511999998888",
      "status": "connected",
      "is_default": true,
      "push_name": "Acme Support",
      "connected_since": "2026-08-15T00:02:00Z",
      "created_at": "2026-08-15T00:00:00Z",
      "updated_at": "2026-08-15T00:02:00Z"
    }
  ]
}
```

---

### 2.5 Disconnect Connection
Disconnects the active session, cancels active pairing loops, emits a `connection.status` event, and removes the connection.

* **Method & Path:** `DELETE /api/v1/connections/:id` (or `DELETE /api/v1/devices/:id`)

#### Response (`200 OK`)
```json
{
  "status": "disconnected",
  "connection_id": "7b1c3d5e-9f8a-4b2c-8d1e-0a2b3c4d5e6f"
}
```

---

### 2.6 Register WhatsApp Cloud (WABA) Connection
Registers a Meta WhatsApp Cloud API account instance for the workspace, automatically sanitizing the sender identity and configuring credentials.

* **Method & Path:** `POST /api/v1/connections/waba` (or `POST /api/v1/workspaces/:workspace_id/connections/waba`, alias `POST /api/v1/devices/waba`)
* **Request Headers:**
  * `Authorization: Bearer <WORKSPACE_API_KEY>`
  * `Content-Type: application/json`

#### Request Body
```json
{
  "name": "Acme WhatsApp Cloud",
  "phone_number_id": "98765432101",
  "waba_account_id": "12345678901",
  "token": "EAABbCcDd123...",
  "verify_token": "custom_verify_token",
  "app_secret": "meta_app_secret_xyz",
  "display_phone_number": "+55 11 98888-7777",
  "verified_name": "Acme Official Support"
}
```

#### Response (`201 Created`)
```json
{
  "id": "c2d3e4f5-a6b7-4c8d-9e0f-1a2b3c4d5e6f",
  "name": "Acme WhatsApp Cloud",
  "slug": "acme-whatsapp-cloud",
  "channel": "whatsapp_cloud",
  "sender_identity": "5511988887777",
  "status": "connected",
  "is_default": false,
  "connected_since": "2026-08-18T15:00:00Z",
  "created_at": "2026-08-18T15:00:00Z",
  "updated_at": "2026-08-18T15:00:00Z"
}
```

---

### 2.7 Register Telegram Bot Connection
Registers a Telegram Bot instance by validating the Bot API Token against `https://api.telegram.org/bot<token>/getMe` and registering the webhook automatically when an HTTPS external URL is configured.

* **Method & Path:** `POST /api/v1/connections/telegram` (or `POST /api/v1/workspaces/:workspace_id/connections/telegram`, alias `POST /api/v1/devices/telegram`)
* **Request Headers:**
  * `Authorization: Bearer <WORKSPACE_API_KEY>`
  * `Content-Type: application/json`

#### Request Body
```json
{
  "name": "Acme Telegram Support",
  "token": "123456789:ABCdefGhIJKlmNoPQRsTUVwxyZ",
  "secret_token": "optional_custom_secret"
}
```

#### Response (`201 Created`)
```json
{
  "id": "d3e4f5a6-b7c8-4d9e-0f1a-2b3c4d5e6f7a",
  "name": "Acme Telegram Support",
  "slug": "acme-telegram-support",
  "channel": "telegram",
  "sender_identity": "@acme_support_bot",
  "status": "connected",
  "is_default": false,
  "connected_since": "2026-08-18T15:00:00Z",
  "created_at": "2026-08-18T15:00:00Z",
  "updated_at": "2026-08-18T15:00:00Z"
}
```

---

## 3. Webhook Subscriptions REST API

Programmatic CRUD for webhook subscribers with automated HMAC-SHA256 signing secret generation and SSRF protection.

### 3.1 Create Subscription
* **Method & Path:** `POST /api/v1/webhooks/subscriptions`

#### Request Body
```json
{
  "url": "https://api.externalcrm.com/webhooks/pergo",
  "events": [
    "message.received",
    "message.delivered",
    "message.failed",
    "connection.status"
  ],
  "is_active": true
}
```

#### Response (`201 Created`)
```json
{
  "subscription": {
    "id": "d1e2f3a4-b5c6-4e5a-8b9c-0d1e2f3a4b5c",
    "workspace_id": "a5e8c1b2-3f4d-4e5a-8b9c-0d1e2f3a4b5c",
    "url": "https://api.externalcrm.com/webhooks/pergo",
    "events": ["message.received", "message.delivered", "message.failed", "connection.status"],
    "secret": "9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b3c2d1e0f9a8b",
    "is_active": true,
    "created_at": "2026-08-15T00:00:00Z",
    "updated_at": "2026-08-15T00:00:00Z"
  }
}
```

### 3.2 List Subscriptions
* **Method & Path:** `GET /api/v1/webhooks/subscriptions`

### 3.3 Get Subscription
* **Method & Path:** `GET /api/v1/webhooks/subscriptions/:id`

### 3.4 Update Subscription
* **Method & Path:** `PUT /api/v1/webhooks/subscriptions/:id`

### 3.5 Delete Subscription
* **Method & Path:** `DELETE /api/v1/webhooks/subscriptions/:id`

---

## 4. Connection Status Webhook Event (`connection.status`)

Whenever a connection connects, disconnects, reconnects, or degrades, PerGo dispatches signed webhook events to all subscribers:

```json
{
  "event": "connection.status",
  "trace_id": "c1a2b3d4-e5f6-4a5b-8c9d-0e1f2a3b4c5d",
  "workspace_id": "a5e8c1b2-3f4d-4e5a-8b9c-0d1e2f3a4b5c",
  "connection_id": "7b1c3d5e-9f8a-4b2c-8d1e-0a2b3c4d5e6f",
  "channel": "whatsapp",
  "sender_identity": "5511999998888",
  "status": "connected",
  "timestamp": "2026-08-15T00:02:10Z"
}
```

Every delivery includes the `X-PerGo-Signature` HMAC-SHA256 header (see `docs/WEBHOOK_SIGNATURES.md`).

---

## 5. Single Sign-On (SSO) / Seamless Admin Hand-off

Allows external SaaS/CRM operators to transition directly into the PerGo native admin console without re-authenticating with passwords.

### 5.1 Protocol
1. External backend generates a signed HMAC-SHA256 short-lived token (TTL <= 120s) signed with `PERGO_SESSION_SECRET`.
2. External backend redirects the user to `GET /admin/sso?token=<token>&redirect=/admin/connections`.
3. PerGo validates the signature and timestamp in constant time, issues `pergo-session` and `pergo-active-workspace` cookies, and redirects the operator.

### 5.2 Token Payload Format
```json
{
  "sub": "operator@externalcrm.com",
  "workspace_id": "a5e8c1b2-3f4d-4e5a-8b9c-0d1e2f3a4b5c",
  "role": "admin",
  "iat": 1755198000,
  "exp": 1755198060,
  "nonce": "c9a1b2e3f4"
}
```

* Token structure: `base64url(payload).base64url(hmac_sha256(payload, PERGO_SESSION_SECRET))` (or standard 3-part JWT format).
