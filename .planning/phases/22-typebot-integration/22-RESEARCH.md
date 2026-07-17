# Phase 22: Typebot Integration — Research Findings

**Researched:** 2026-07-17
**Requirement IDs:** TYPE-01, TYPE-02, TYPE-03, TYPE-04
**Sources:** Typebot API docs (typebot.com), codebase analysis of Phase 21 Chatwoot integration patterns

---

## 1. Typebot API Contract

### 1.1 startChat — Initiate a new bot session

- **Endpoint:** `POST {apiURL}/api/v1/typebots/{publicId}/startChat`
- **Authentication:** Live `startChat` is public (no Bearer token needed for published bots). The `publicId` identifies the bot. For preview/test mode, `Authorization: Bearer <api-token>` is required.
- **Request Body (optional):**
```json
{
  "isStreamEnabled": false,
  "prefilledVariables": {
    "customerName": "John",
    "customerPhone": "+5511999999999",
    "channel": "whatsapp"
  }
}
```
- **Response:**
```json
{
  "sessionId": "clshrd41q42nwfsq004h8b79t",
  "messages": [
    {
      "id": "message-id-1",
      "type": "text",
      "content": {
        "type": "text",
        "richText": [{"children": [{"text": "Hello! How can I help you?"}]}]
      }
    },
    {
      "id": "message-id-2",
      "type": "image",
      "content": {
        "type": "image",
        "url": "https://example.com/image.jpg"
      }
    }
  ],
  "input": {
    "id": "input-id",
    "type": "text input"
  },
  "clientSideActions": [],
  "logs": [],
  "dynamicTheme": null
}
```
- **Key field:** `sessionId` — MUST be persisted in `typebot_sessions` for subsequent `continueChat` calls.
- **Key field:** `messages` — Array of bubble blocks (text, image, video, audio, embed) that the bot sends back immediately.
- **Key field:** `input` — If present, bot is waiting for user reply. If null/absent, bot flow ended.

### 1.2 continueChat — Send user reply to an active session

- **Endpoint:** `POST {apiURL}/api/v1/sessions/{sessionId}/continueChat`
- **Authentication:** None required for live sessions.
- **Request Body:**
```json
{
  "message": {
    "type": "text",
    "text": "User's reply here"
  }
}
```
- **Response:** Same schema as `startChat` — returns `messages[]`, `input`, `clientSideActions`, etc. The `sessionId` remains the same.
- **Error:** Returns **404** when session is expired/invalid → PerGo must delete the `typebot_sessions` row and optionally restart with `startChat`.

### 1.3 Webhook Block (for D-07 — `POST /api/integrations/typebot`)

- **How it works:** In Typebot flow builder, the operator places a "Webhook" block that pauses the conversation until an external service POSTs to a Typebot-provided URL.
- **PerGo's webhook endpoint** (`POST /api/integrations/typebot`) is the *reverse* direction — it receives manually triggered messages FROM Typebot's HTTP Request blocks (not the Webhook block). The Typebot operator configures an "HTTP Request" block in the flow that POSTs to PerGo's endpoint to trigger an outbound message asynchronously.
- **Expected incoming payload** (operator-defined in Typebot's HTTP Request block body):
```json
{
  "workspace_id": "uuid",
  "connection_id": "uuid",
  "to": "customer_phone_or_id",
  "body": "Message text from bot",
  "channel": "whatsapp"
}
```

### 1.4 Session Lifecycle

| Event | Action |
|-------|--------|
| First customer message to a bot-mapped connection | Call `startChat`, persist `sessionId` in `typebot_sessions` |
| Subsequent customer messages | Call `continueChat` with stored `sessionId` |
| `continueChat` returns 404 | Delete session row, call `startChat` to restart |
| `input` field absent in response | Bot flow completed, delete session row |
| Inactivity timeout (e.g. 30 min) | Delete session row; next message triggers `startChat` |

---

## 2. Existing Patterns to Reuse from Chatwoot Integration (Phase 21)

### 2.1 Integration Repository & Encryption (`internal/repository/integration.go`)

- **Unified `integrations` table** with `provider = "typebot"` — UNIQUE constraint on `(workspace_id, provider)`.
- **AES-256-GCM encryption** via `CredentialProvider.Encrypt/Decrypt` on the `config BYTEA` column.
- **`IntegrationRepository.Save()`** uses `ON CONFLICT (workspace_id, provider) DO UPDATE` — exact same upsert pattern for Typebot.
- **`IntegrationRepository.GetByProvider(ctx, workspaceID, "typebot")`** — decrypt and return config.
- **Typebot config envelope** stored in the encrypted JSON `config` field:
```json
{
  "api_url": "https://typebot.example.com",
  "bots": [
    {
      "public_id": "my-bot-abc123",
      "name": "Sales Bot",
      "connection_id": "uuid-of-connection",
      "trigger_keywords": ["sales", "vendas"],
      "is_default": true
    },
    {
      "public_id": "my-bot-support",
      "name": "Support Bot",
      "connection_id": "uuid-of-connection-2",
      "trigger_keywords": ["support", "suporte"],
      "is_default": false
    }
  ]
}
```

### 2.2 Webhook Handler Pattern (`internal/api/handler/chatwoot_webhook.go`)

Reusable pattern for `TypebotWebhookHandler`:
1. Extract `workspaceID` from `tenant.WorkspaceIDFrom(ctx)` (set by `AuthMiddleware` via API key query param).
2. Read and parse JSON body.
3. Resolve `customerIdentity` from `contact_identities` table.
4. Construct `domain.QueueMessage` with `ConnectionID`, `SenderIdentity`, `To`, `Channel`, `Body`.
5. Publish to NATS `messages.outbound` via `publisher.Publish()`.
6. Return `200 OK`.

### 2.3 Syncer Pattern (`internal/integration/chatwoot/syncer.go`)

The `ChatwootSyncer` pattern maps directly to a `TypebotForwarder`:
1. Fetch integration config via `integrationRepo.GetByProvider(ctx, workspaceID, "typebot")`.
2. Check `integration.Active`.
3. Unmarshal config JSON to typed struct.
4. Create HTTP client for Typebot API calls.
5. Execute business logic (for Typebot: `startChat`/`continueChat`, not sync to external).

### 2.4 InboundProcessor Hook Pattern (`internal/inbound/processor.go`)

The Chatwoot syncer is registered via **setter injection** and called in a goroutine post-NATS-publish. The same pattern applies for TypebotForwarder.

### 2.5 Admin Handler Pattern (`internal/api/handler/admin/integration.go`)

`ChatwootAdminHandler` shows the exact template:
- `GetSettings(c *echo.Context)` — parse `workspace_id`, load integration config, render templ page.
- `PostSettings(c *echo.Context)` — parse form values, validate, marshal config JSON, save via `integrationRepo.Save()`.

### 2.6 Main.go Wiring Pattern (`cmd/pergo/main.go`)

Chatwoot wiring: Create repos → Create syncer → Inject into processor → Create admin handler → Register routes.

---

## 3. InboundProcessor Hook Point for Bot Forwarding

**File:** `internal/inbound/processor.go` — `Process()` method

**Flow position:** After contact resolution, dedup, media upload, NATS publish, and Chatwoot sync.

**Recommended insertion point:** After Chatwoot sync (new step 10), OR in parallel with Chatwoot sync. Both run in goroutines and are independent.

**Critical: The forwarder must publish bot responses synchronously within the goroutine.** When `startChat`/`continueChat` returns `messages[]`, the forwarder immediately constructs `domain.QueueMessage` payloads and publishes them to `messages.outbound` via the `Publisher` interface (D-06 hybrid flow).

---

## 4. Database Schema Requirements

### 4.1 `typebot_sessions` Table

**Migration file:** `028_create_typebot_sessions.sql`

```sql
-- +goose Up
CREATE TABLE typebot_sessions (
    workspace_id  UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    contact_id    UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    connection_id UUID NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    session_id    TEXT NOT NULL,
    bot_public_id TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (workspace_id, contact_id, connection_id)
);

CREATE INDEX idx_typebot_sessions_contact ON typebot_sessions(contact_id);
CREATE INDEX idx_typebot_sessions_updated ON typebot_sessions(updated_at);

-- +goose Down
DROP INDEX IF EXISTS idx_typebot_sessions_updated;
DROP INDEX IF EXISTS idx_typebot_sessions_contact;
DROP TABLE IF EXISTS typebot_sessions;
```

**Design rationale (D-05):**
- Composite PK `(workspace_id, contact_id, connection_id)` — one active session per contact per connection channel.
- `updated_at` — updated on every `continueChat` call; used for inactivity timeout queries.
- `session_id` — the Typebot API session identifier, used for `continueChat` calls.
- `bot_public_id` — tracks which bot config the session belongs to.

### 4.2 No changes needed to `integrations` table

The existing `integrations` table already supports `provider = "typebot"`.

---

## 5. Admin Settings UI Patterns

### 5.1 Template Structure

Follow `templates/pages/integrations.templ` pattern:

**Page components:**
- `TypebotSettingsPage(workspaceID, cfg, active, successMsg, errorMsg)` — wrapper with `layout.Base`
- `TypebotSettingsContent(...)` — form with:
  - API URL input (text/url)
  - Bot configurations section (repeatable fieldset with HTMX dynamic add/remove)
    - Public ID input, Bot name input, Connection ID dropdown, Trigger keywords input, Default bot checkbox
  - Active toggle checkbox
  - Save button

### 5.2 Route Registration

```go
adminGroup.GET("/workspaces/:workspace_id/integrations/typebot", typebotAdminHandler.GetSettings)
adminGroup.POST("/workspaces/:workspace_id/integrations/typebot", typebotAdminHandler.PostSettings)
```

### 5.3 Sidebar Update

The current "Integrations" link in `templates/layout/sidebar.templ` points directly to `/integrations/chatwoot`. This needs a second submenu item for Typebot.

---

## 6. Integration Points and Data Flow

### 6.1 Inbound Customer Message → Typebot → Outbound Reply

```
Customer → WhatsApp msg → PerGo InboundProcessor.Process()
  → Contact resolved → Dedup → Media → NATS publish → Chatwoot sync
  → TypebotForwarder (goroutine):
    → Lookup typebot_sessions
    → [NO SESSION]: Match bot config (connection_id + keywords) → startChat → Save session
    → [HAS SESSION]: continueChat with stored sessionId → Update updated_at
    → Parse messages[] → Map to QueueMessage → NATS publish "messages.outbound"
```

### 6.2 Typebot HTTP Request Block → PerGo Webhook → Outbound

```
Typebot Flow HTTP Request Block → POST /api/integrations/typebot?token=xxx
  → Parse payload → Resolve contact identity → Build QueueMessage → NATS publish outbound
```

### 6.3 Bot Selection Logic (D-03, D-04)

1. Receive inbound message for (workspace_id, connection_id)
2. Check if contact has active session in typebot_sessions
   → YES: Forward to continueChat (ignore any trigger keywords — D-04)
   → NO: Load typebot config → Find bot configs matching connection_id → Check trigger keywords → Match or use default → startChat

---

## 7. Key Implementation Files to Create/Modify

### New Files:
| File | Purpose |
|------|---------|
| `internal/platform/postgres/migrations/028_create_typebot_sessions.sql` | typebot_sessions table |
| `internal/repository/typebot_session.go` | TypebotSessionRepository CRUD |
| `internal/integration/typebot/client.go` | TypebotClient (startChat, continueChat HTTP calls) |
| `internal/integration/typebot/forwarder.go` | TypebotForwarder (implements ForwardToBot interface) |
| `internal/api/handler/typebot_webhook.go` | TypebotWebhookHandler (POST /api/integrations/typebot) |
| `internal/api/handler/admin/typebot_integration.go` | TypebotAdminHandler (GET/POST settings) |
| `templates/pages/typebot_settings.templ` | Admin settings page template |

### Modified Files:
| File | Change |
|------|--------|
| `internal/inbound/processor.go` | Add `TypebotForwarder` interface, setter, goroutine call |
| `cmd/pergo/main.go` | Wire TypebotSessionRepo, TypebotForwarder, handlers, routes |
| `templates/layout/sidebar.templ` | Add Typebot integration link in settings submenu |

---

## 8. Risks and Edge Cases

| Risk | Mitigation |
|------|------------|
| Typebot session expires between PerGo's `continueChat` calls | Catch 404 response → delete session → call `startChat` to restart |
| Race condition: two inbound messages arrive before first `startChat` completes | Use `INSERT ... ON CONFLICT DO NOTHING` for session creation; second goroutine retries as `continueChat` |
| Typebot API is slow/down | 15s timeout on goroutine context; log and fail silently (inbound message already published to NATS) |
| Bot response contains rich elements (buttons, links) that don't map to WhatsApp/Telegram | Parse `messages[].type` and format appropriately; fallback to plain text for unsupported types |
| Multiple bot responses need ordered delivery | Process `messages[]` array sequentially, publish each as separate `QueueMessage` with same traceID |
| `prefilledVariables` — what to pass | Pass `customerName`, `customerPhone`, `channel`, `contactId` to give bot context |

---

## 9. Confidence Assessment

| Finding | Confidence | Source |
|---------|------------|--------|
| Typebot API endpoints (startChat/continueChat) | HIGH | Official Typebot docs + community implementations |
| Typebot response schema (messages, input, sessionId) | HIGH | Official API docs |
| Typebot auth model (publicId public, Bearer for preview) | HIGH | Official docs |
| Typebot webhook block mechanics | MEDIUM | Docs + community; exact production URL pattern needs verification on self-hosted |
| Chatwoot integration patterns (all) | HIGH | Direct codebase analysis |
| InboundProcessor hook point | HIGH | Direct codebase analysis |
| contact_identities schema and resolution | HIGH | Direct codebase analysis |
| Integration encryption envelope | HIGH | Direct codebase analysis |
