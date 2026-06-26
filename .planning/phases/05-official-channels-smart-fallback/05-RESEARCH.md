# Phase 5: Official Channels & Smart Fallback - Research Report

## 1. WABA Integration Details

The WhatsApp Cloud API (WABA) is a stateless REST API hosted by Meta. To integrate WABA as a channel adapter in OmniGo, we will call Meta's Graph API endpoints.

### API Endpoint & Auth Headers
*   **Base URL & Version:** `https://graph.facebook.com/v18.0` (as defined in `D-01`)
*   **Message Send Endpoint:** `POST https://graph.facebook.com/v18.0/{phone_number_id}/messages`
*   **Authentication:** 
    *   `Authorization: Bearer {system_user_access_token}`
    *   `Content-Type: application/json`

### Request Payloads

#### A. Text Message Payload
For free-form text messages (only valid within the 24-hour customer service window):
```json
{
  "messaging_product": "whatsapp",
  "recipient_type": "individual",
  "to": "{recipient_phone_number}",
  "type": "text",
  "text": {
    "preview_url": false,
    "body": "{message_body_text}"
  }
}
```

#### B. Template Message Payload
For structured template messages (required outside the 24-hour customer service window):
```json
{
  "messaging_product": "whatsapp",
  "recipient_type": "individual",
  "to": "{recipient_phone_number}",
  "type": "template",
  "template": {
    "name": "{template_name}",
    "language": {
      "code": "{language_code}"
    },
    "components": [
      {
        "type": "body",
        "parameters": [
          {
            "type": "text",
            "text": "{param1_value}"
          },
          {
            "type": "text",
            "text": "{param2_value}"
          }
        ]
      }
    ]
  }
}
```
*Note: Phase 5 restricts templates to text-only parameters (headers/body/footer). Media template components are deferred.*

### Response Payloads

#### A. Success Response (HTTP 200 OK)
Meta returns a unique WhatsApp Message ID (`wamid`) for tracking status:
```json
{
  "messaging_product": "whatsapp",
  "contacts": [
    {
      "input": "{recipient_phone_number}",
      "wa_id": "{whatsapp_user_id}"
    }
  ],
  "messages": [
    {
      "id": "wamid.HBgL..."
    }
  ]
}
```

#### B. Error Response (HTTP 4xx / 5xx)
Meta returns detailed error codes and subcodes:
```json
{
  "error": {
    "message": "Unsupported post request...",
    "type": "OAuthException",
    "code": 100,
    "error_subcode": 33,
    "fbtrace_id": "A1B2C3D4"
  }
}
```

---

## 2. Telegram Bot Integration Details

The Telegram Bot API is a stateless HTTP API.

### API Endpoint & Auth Headers
*   **Base URL:** `https://api.telegram.org/bot{bot_token}`
*   **Message Send Endpoint:** `POST https://api.telegram.org/bot{bot_token}/sendMessage`
*   **Headers:** `Content-Type: application/json`

### Send Message Payload
```json
{
  "chat_id": "{chat_id_or_channel_username}",
  "text": "{body_text}",
  "parse_mode": "MarkdownV2"
}
```

### Success Response
```json
{
  "ok": true,
  "result": {
    "message_id": 12345,
    "from": {
      "id": 987654321,
      "is_bot": true,
      "first_name": "MyBot",
      "username": "my_bot"
    },
    "chat": {
      "id": 123456789,
      "first_name": "User",
      "type": "private"
    },
    "date": 1678901234,
    "text": "Hello"
  }
}
```

### Webhook Registration
To receive inbound messages, register the webhook using:
*   **Endpoint:** `POST https://api.telegram.org/bot{bot_token}/setWebhook`
*   **Payload:**
    ```json
    {
      "url": "https://{your-domain}/webhooks/telegram/{workspace_id}",
      "secret_token": "{secure_uuid_or_random_token}"
    }
    ```

### Webhook Validation
Telegram includes the `secret_token` in the headers of all webhook HTTP POST requests:
*   Header key: `X-Telegram-Bot-Api-Secret-Token`
*   Validation logic: Reject requests immediately with `403 Forbidden` if the header is missing or does not match the configured `secret_token` for the workspace.

---

## 3. Template Management DB Schema & CRUD

WABA templates will be persisted locally to facilitate UI rendering, status checking, and validation before sending.

### Database Tables Schema

```sql
-- Migration: 004_create_official_channels_tables.sql

-- A. Workspace Channel Credentials Table
CREATE TABLE channel_credentials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel TEXT NOT NULL, -- 'whatsapp_cloud', 'telegram'
    credentials BYTEA NOT NULL, -- AES-256-GCM encrypted JSON
    key_id TEXT NOT NULL, -- Key identifier for encryption key rotation
    key_version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, channel)
);
CREATE INDEX idx_channel_credentials_workspace ON channel_credentials(workspace_id);

-- B. WABA Templates Table
CREATE TABLE waba_templates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    meta_template_id TEXT, -- Populated after Meta API registration
    name TEXT NOT NULL,
    language TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending', -- 'pending', 'approved', 'rejected', 'paused'
    category TEXT NOT NULL, -- 'MARKETING', 'UTILITY', 'AUTHENTICATION'
    components JSONB NOT NULL, -- Text templates components
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, name, language)
);
CREATE INDEX idx_waba_templates_workspace ON waba_templates(workspace_id);
```

### CRUD API & Admin Integration
1.  **Create Template (`POST /admin/workspaces/:workspace_id/templates`):**
    *   Saves the template locally in the DB with status `pending`.
    *   Calls Meta Graph API: `POST https://graph.facebook.com/v18.0/{waba_account_id}/message_templates`.
    *   Saves the returned `id` as `meta_template_id`.
2.  **List Templates (`GET /admin/workspaces/:workspace_id/templates`):**
    *   Fetches from `waba_templates` filtered by `workspace_id`.
3.  **Sync/Poll Approval Status:**
    *   **Manual Trigger:** Admin panel features a "Sync" button that hits `POST /admin/workspaces/:workspace_id/templates/:template_id/sync`. This performs a `GET https://graph.facebook.com/v18.0/{meta_template_id}` to retrieve the template status and update the DB.
    *   **Automated Webhook:** Register a webhook endpoint under `/webhooks/waba/:workspace_id` to handle Meta's template status update notifications (`message_template_status_update` field) and update the status in the DB automatically.

---

## 4. 24h Customer Service Window Tracking

For WABA, free-form text messages are rejected by Meta if there is no user-initiated session within 24 hours. To enforce this logic locally (avoiding API charges and early failing to fallback channels), we track the last inbound message per recipient.

### Database Table Schema
```sql
CREATE TABLE recipient_sessions (
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    recipient_phone TEXT NOT NULL,
    channel TEXT NOT NULL,
    last_inbound_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, recipient_phone, channel)
);
```

### Check Logic before Dispatch (WABA Adapter)
When a message is received by the worker for WABA:
1.  Check if the payload is a template message (`TemplateName` is populated). If yes, proceed to send.
2.  If it is a free-form message:
    *   Query `recipient_sessions` for `(workspace_id, recipient_phone, 'whatsapp_cloud')`.
    *   If no row exists, or if `now().Sub(last_inbound_at) > 24 * time.Hour`:
        *   Return a terminal error: `channel.NewTerminalError(errors.New("customer service window expired"))`.
        *   This skips queue retry and triggers immediate routing to the next channel in `fallback_channels`.

### Session Session Updates
Every time an inbound message is received (via whatsmeow, Telegram webhooks, or WABA webhooks), the platform upserts `recipient_sessions`:
```sql
INSERT INTO recipient_sessions (workspace_id, recipient_phone, channel, last_inbound_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (workspace_id, recipient_phone, channel)
DO UPDATE SET last_inbound_at = EXCLUDED.last_inbound_at;
```

---

## 5. Fallback and Terminal Errors

### NATS Queue Payload Wrapper (Bridge)
Currently, `handler/message.go` publishes the raw `CreateMessageRequest` without `workspace_id` or `trace_id` inside the JSON payload. This is a gap since the worker requires the tenant identifier to load credentials and log dispatches.

We will define an internal `QueueMessage` struct to wrap the published NATS message payload:
```go
type QueueMessage struct {
	WorkspaceID      uuid.UUID           `json:"workspace_id"`
	TraceID          string              `json:"trace_id"`
	To               string              `json:"to"`
	Channel          string              `json:"channel"`
	Body             string              `json:"body"`
	Metadata         map[string]string   `json:"metadata,omitempty"`
	TTLSeconds       *int                `json:"ttl_seconds,omitempty"`
	QueuedAt         time.Time           `json:"queued_at"`
	FallbackChannels []string            `json:"fallback_channels,omitempty"`
	TemplateName     string              `json:"template_name,omitempty"`
	Language         string              `json:"language,omitempty"`
	Components       []TemplateComponent `json:"components,omitempty"`
}

type TemplateComponent struct {
	Type       string              `json:"type"`       // "header", "body", "buttons"
	Parameters []TemplateParameter `json:"parameters"`
}

type TemplateParameter struct {
	Type string `json:"type"` // "text"
	Text string `json:"text,omitempty"`
}
```

### Failures & Fallback Table (State Tracker)
To prevent duplicate deliveries across retries and restarts, we track dispatches in the database:
```sql
CREATE TABLE message_dispatches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    trace_id TEXT NOT NULL UNIQUE,
    current_channel TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued', -- 'queued', 'sending', 'sent', 'failed'
    fallback_index INT NOT NULL DEFAULT 0,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_message_dispatches_trace_id ON message_dispatches(trace_id);
```

### Worker Fallback Loop Design (Go)

```go
func (w *Worker) processMessage(ctx context.Context, msg jetstream.Msg) {
	traceID := msg.Headers().Get("Nats-Msg-Id")
	attempt := w.retryAttempt(msg)

	var payload QueueMessage
	if err := json.Unmarshal(msg.Data(), &payload); err != nil {
		slog.Error("worker: failed to unmarshal payload", "error", err)
		_ = msg.Ack()
		return
	}

	// --- 1. Deduplication & State Recovery ---
	dispatch, err := w.repo.GetOrCreateDispatch(ctx, payload.WorkspaceID, traceID, payload.Channel)
	if err != nil {
		slog.Error("worker: failed to fetch dispatch state", "error", err, "trace_id", traceID)
		w.handleFailure(msg, traceID, attempt)
		return
	}

	// If already successfully sent, ack and skip
	if dispatch.Status == "sent" {
		slog.Info("worker: message already sent, skipping duplicate delivery", "trace_id", traceID)
		_ = msg.Ack()
		return
	}

	// If already failed permanently, ack and skip
	if dispatch.Status == "failed" {
		slog.Info("worker: message already failed permanently, skipping", "trace_id", traceID)
		_ = msg.Ack()
		return
	}

	// --- 2. Dispatch Loop with Fallback ---
	currentChannel := dispatch.CurrentChannel
	fallbackIndex := dispatch.FallbackIndex

	for {
		// Resolve the dispatcher for the current channel
		dispatcher, ok := w.dispatchers.Get(currentChannel)
		if !ok {
			err = fmt.Errorf("no dispatcher registered for channel %s", currentChannel)
			w.repo.UpdateDispatchStatus(ctx, traceID, currentChannel, "failed_transient", fallbackIndex, err.Error())
			w.handleFailure(msg, traceID, attempt)
			return
		}

		// Inject workspace context
		dispatchCtx := tenant.WithWorkspaceID(ctx, payload.WorkspaceID)

		// Dispatch message
		err = dispatcher.Dispatch(dispatchCtx, &channel.MessagePayload{
			MessageID: traceID,
			TraceID:   traceID,
			To:        payload.To,
			Channel:   currentChannel,
			Body:      payload.Body,
			Metadata:  payload.Metadata,
		})

		if err == nil {
			// Success! Update DB status and Ack
			_ = w.repo.UpdateDispatchStatus(ctx, traceID, currentChannel, "sent", fallbackIndex, "")
			_ = msg.Ack()
			slog.Info("worker: message successfully delivered", "trace_id", traceID, "channel", currentChannel)
			return
		}

		// Handle error: Check if it's a terminal error
		if channel.IsTerminal(err) {
			slog.Warn("worker: terminal dispatch failure", "channel", currentChannel, "error", err, "trace_id", traceID)

			// Check if we can fallback to another channel
			if fallbackIndex < len(payload.FallbackChannels) {
				nextChannel := payload.FallbackChannels[fallbackIndex]
				fallbackIndex++
				slog.Info("worker: attempting fallback", "from", currentChannel, "to", nextChannel, "trace_id", traceID)
				
				currentChannel = nextChannel
				_ = w.repo.UpdateDispatchStatus(ctx, traceID, currentChannel, "sending", fallbackIndex, err.Error())
				continue // loop to try the next channel
			} else {
				// Fallbacks exhausted, mark as permanently failed
				_ = w.repo.UpdateDispatchStatus(ctx, traceID, currentChannel, "failed", fallbackIndex, err.Error())
				_ = msg.Ack() // Ack to prevent NATS retries on permanent failures
				slog.Error("worker: message permanently failed after exhausting all fallbacks", "trace_id", traceID)
				return
			}
		} else {
			// Transient error (e.g. network timeout). Do not fallback!
			// Save current state as failed_transient to resume on next NATS delivery.
			_ = w.repo.UpdateDispatchStatus(ctx, traceID, currentChannel, "failed_transient", fallbackIndex, err.Error())
			w.handleFailure(msg, traceID, attempt)
			return
		}
	}
}
```

### Error Classification Catalog

| Provider | Error Condition / Code | Classification | Reason |
| :--- | :--- | :--- | :--- |
| **WABA** | Code `131030` (Number not in WhatsApp) | **Terminal** | Address invalid; cannot be resolved. |
| **WABA** | Code `131047` (Outside 24h window) | **Terminal** | Violates policy; must use template or fallback. |
| **WABA** | Code `132000` (Template not approved) | **Terminal** | Config error; will not change dynamically. |
| **WABA** | Code `100` / Subcode `33` (Invalid param) | **Terminal** | Payload/metadata validation error. |
| **WABA** | Code `190` (Invalid access token) | **Terminal** | Configuration/credential failure. |
| **WABA** | Code `130429` (Rate limit) | *Transient* | Should retry after delay. |
| **WABA** | Code `131052` (Service unavailable) | *Transient* | Temporary Meta outage. |
| **Telegram**| Code `400` ("chat not found") | **Terminal** | Chat ID is incorrect or hasn't started bot. |
| **Telegram**| Code `403` ("bot was blocked by user") | **Terminal** | Recipient has blocked the bot. |
| **Telegram**| Code `401` ("unauthorized token") | **Terminal** | Credentials revoked/invalid. |
| **Telegram**| Code `429` ("too many requests") | *Transient* | Rate limit exceeded. |
| **WhatsApp** | `LoggedOut` / `403` / `unpaired` | **Terminal** | WhatsApp Web session is dead. |
| **WhatsApp** | Connection timeout / disconnect | *Transient* | Re-pairing or reconnect is active. |

---

## 6. Validation Architecture

To guarantee the robustness of Phase 5, we define a structured verification checklist.

### Unit Tests
1.  **Error Classification:** Verify that `IsTerminal()` correctly flags Meta error codes (`131030`, `131047`, etc.) and Telegram bot API errors.
2.  **Payload Validation:** Verify that `ValidateMessage` accepts valid fallback channels and rejects duplicate or unsupported channels. Test validation logic for templates (checking that `Language` is present when `TemplateName` is set).
3.  **24-Hour Window:** Mock the current time and check that `recipient_sessions` queries successfully distinguish between active sessions (<24h) and expired sessions (>24h).
4.  **Fallback State Machine:** Mock the dispatchers and run unit tests on the worker loop to assert that:
    *   A terminal error on the primary channel instantly triggers dispatch on the next fallback channel.
    *   A transient error halts the fallback loop and triggers NATS backoff.
    *   If a message is redelivered after a transient error, it resumes on the correct fallback channel.
    *   A message that already succeeded (status = `sent` in DB) is immediately skipped on redelivery.

### Integration Tests
1.  **Database Migrations:** Run migrations and verify table structures for `channel_credentials`, `waba_templates`, `recipient_sessions`, and `message_dispatches` exist and enforce constraints.
2.  **AES-256-GCM Encryption:** Verify that WABA and Telegram tokens are successfully encrypted before insert into `channel_credentials` and correctly decrypted when read.
3.  **Repository Operations:** Write integration tests to verify batch upserts for `recipient_sessions` and state transitions for `message_dispatches` work seamlessly under load.

### Manual Verification Checklist
1.  **Meta WABA Handshake:** Test webhook endpoint GET request verification by sending mocked `hub.mode=subscribe`, `hub.verify_token`, and `hub.challenge` query parameters, ensuring `200 OK` and the challenge string is returned.
2.  **Telegram Webhook Handshake:** Run `setWebhook` via curl and verify that inbound messages from Telegram are correctly received, their `X-Telegram-Bot-Api-Secret-Token` header matches, and `recipient_sessions` is successfully updated.
3.  **End-to-End Fallback (Local Simulation):**
    *   Register WABA and Telegram adapters with mock endpoints (pointing to an HTTP test server).
    *   Configure WABA to return a terminal error (e.g., `131047` outside 24h window).
    *   Configure Telegram Bot to return `200 OK` (success).
    *   Submit a message via `POST /api/v1/messages` targeting WABA with fallback `["telegram"]`.
    *   Verify the message dispatch status transitions to `sent` on `telegram` and the message is not retried by NATS.
    *   Inspect `audit_logs` and `message_dispatches` to confirm the trace sequence.
