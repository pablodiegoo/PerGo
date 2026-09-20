# REST Endpoints Reference

Comprehensive reference for all public REST API endpoints in PerGo.

---

## 1. Unified Message Dispatch

### Send Message
Dispatches a message across WhatsApp Web, WhatsApp Cloud (WABA), or Telegram with optional fallback channels.

- **Method & Path:** `POST /api/v1/messages`
- **Headers:**
  - `Authorization: Bearer <WORKSPACE_API_KEY>`
  - `Content-Type: application/json`

#### Text Message Payload
```json
{
  "to": "5511999999999",
  "channel": "whatsapp",
  "body": "Hello! Your package has been dispatched.",
  "fallback_channels": ["telegram"]
}
```

#### Media Attachment Payload
```json
{
  "to": "5511999999999",
  "channel": "whatsapp",
  "body": "Here is your invoice.",
  "media": {
    "media_url": "https://example.com/invoice-102.pdf",
    "media_type": "document",
    "filename": "invoice-102.pdf",
    "caption": "Monthly Invoice"
  }
}
```

#### WhatsApp Cloud Template Payload
```json
{
  "to": "5511999999999",
  "channel": "whatsapp_cloud",
  "template": {
    "name": "shipping_update",
    "language": "en_US",
    "components": [
      {
        "type": "body",
        "parameters": [
          {"type": "text", "text": "John"},
          {"type": "text", "text": "TRK98234"}
        ]
      }
    ]
  }
}
```

#### Response (`202 Accepted`)
```json
{
  "trace_id": "7c9e6679-7425-40de-944b-e07fc1f90ae7",
  "status": "queued"
}
```

---

## 2. Tenant Workspace Provisioning (Master Auth)

### Provision Workspace
Creates an isolated tenant workspace and optionally provisions default API keys and webhook signing secrets.

- **Method & Path:** `POST /api/v1/workspaces`
- **Headers:**
  - `Authorization: Bearer <PERGO_MASTER_KEY>` (or `X-Master-Key`)
  - `Content-Type: application/json`

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

### List Workspaces
- **Method & Path:** `GET /api/v1/workspaces`
- **Headers:** `Authorization: Bearer <PERGO_MASTER_KEY>`
- **Response (`200 OK`):**
```json
{
  "workspaces": [
    {
      "id": "a5e8c1b2-3f4d-4e5a-8b9c-0d1e2f3a4b5c",
      "name": "Acme Corp",
      "created_at": "2026-08-15T00:00:00Z"
    }
  ]
}
```

---

## 3. Device & Pairing Management

### Initiate Pairing
- **Method & Path:** `POST /api/v1/devices/pair`
- **Headers:** `Authorization: Bearer <WORKSPACE_API_KEY>`
- **Response (`201 Created`):**
```json
{
  "device_id": "d1e2f3a4-b5c6-7d8e-9f0a-1b2c3d4e5f6a",
  "status": "pairing",
  "qr_code": "2@b...Base64EncodedQR..."
}
```

### Get QR Code
- **Method & Path:** `GET /api/v1/devices/:id/qr`
- **Response (`200 OK`):**
```json
{
  "device_id": "d1e2f3a4-b5c6-7d8e-9f0a-1b2c3d4e5f6a",
  "qr_code": "2@b...Base64EncodedQR...",
  "status": "pairing"
}
```

### List Devices
- **Method & Path:** `GET /api/v1/devices`
- **Response (`200 OK`):**
```json
{
  "devices": [
    {
      "id": "d1e2f3a4-b5c6-7d8e-9f0a-1b2c3d4e5f6a",
      "name": "Support Line",
      "phone_number": "5511999999999",
      "status": "connected",
      "battery": 95,
      "updated_at": "2026-08-15T00:00:00Z"
    }
  ]
}
```

---

## 4. Campaigns & Broadcast Engine

### Create Campaign
- **Method & Path:** `POST /api/v1/campaigns`
- **Headers:** `Authorization: Bearer <WORKSPACE_API_KEY>`
- **Request Body:**
```json
{
  "name": "Black Friday Sale",
  "connection_id": "d1e2f3a4-b5c6-7d8e-9f0a-1b2c3d4e5f6a",
  "tag_ids": ["tag-uuid-1", "tag-uuid-2"],
  "message_body": "Hello {{name}}, our sale starts now!",
  "rate_limit_per_min": 30,
  "delay_min_seconds": 1,
  "delay_max_seconds": 3
}
```
- **Response (`201 Created`):**
```json
{
  "id": "c1a2b3c4-d5e6-7f8a-9b0c-1d2e3f4a5b6c",
  "status": "scheduled",
  "total_recipients": 1450
}
```

### Pause & Resume Campaign
- **Pause:** `POST /api/v1/campaigns/:id/pause`
- **Resume:** `POST /api/v1/campaigns/:id/resume`

---

## 5. Single Sign-On (SSO) Hand-off

Allows an external parent SaaS or CRM to log an administrator directly into the PerGo Operator Console:

- **Method & Path:** `GET /admin/sso?token=<HMAC_OR_JWT_TOKEN>`
- **Behavior:** Validates the signature using `PERGO_SESSION_SECRET`. On success, sets the native session cookie and issues an `HTTP 302 Found` redirect to `/admin/`.
