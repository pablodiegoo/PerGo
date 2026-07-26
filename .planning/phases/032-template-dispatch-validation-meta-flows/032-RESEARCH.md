# Phase 32 Research: Template Dispatch, Validation Engine & Meta Flows

## 1. Meta Template Message API

**Endpoint**: `POST https://graph.facebook.com/v18.0/{phone_number_id}/messages`

**Payload Structure**:
To send a template message, the payload must specify `type: "template"` and include the `template` object containing the `name`, `language`, and `components`.

```json
{
  "messaging_product": "whatsapp",
  "recipient_type": "individual",
  "to": "PHONE_NUMBER",
  "type": "template",
  "template": {
    "name": "template_name_here",
    "language": {
      "code": "en_US"
    },
    "components": [
      {
        "type": "header",
        "parameters": [
          {
            "type": "image",
            "image": {
              "link": "https://example.com/img.jpg"
            }
          }
        ]
      },
      {
        "type": "body",
        "parameters": [
          {
            "type": "text",
            "text": "Variable 1 Value"
          }
        ]
      },
      {
        "type": "button",
        "sub_type": "quick_reply",
        "index": 0,
        "parameters": [
          {
            "type": "payload",
            "payload": "DEVELOPER_DEFINED_PAYLOAD"
          }
        ]
      }
    ]
  }
}
```

**Error Codes**:
- **131000 or 470**: Message Permission Denied / Re-engagement message. This indicates the 24-hour session window has expired and a free-form message was blocked. **Resolution**: Send an approved Template Message instead.
- **190 (subcode 463)**: Access Token Expired.
- **131030 / 131047 / 132000**: Various parameter validation/account restriction errors.

**Rate Limits**:
Meta enforces tier-based messaging limits starting at 1,000 marketing/utility/auth conversations per 24 hours (unlimited service conversations within the 24h window). Exceeding this returns HTTP 429 or specific 130429 API errors.

---

## 2. Meta Flows API & Data Exchange Protocol

**Sending a Flow Message via Cloud API**:
Flows are sent as interactive messages.

```json
{
  "messaging_product": "whatsapp",
  "to": "PHONE_NUMBER",
  "type": "interactive",
  "interactive": {
    "type": "flow",
    "header": { "type": "text", "text": "Header text" },
    "body": { "text": "Body text" },
    "footer": { "text": "Footer text" },
    "action": {
      "name": "flow",
      "parameters": {
        "flow_message_version": "3",
        "flow_token": "unique_session_token",
        "flow_id": "1234567890",
        "flow_cta": "Open Flow",
        "flow_action": "navigate",
        "flow_action_payload": {
          "screen": "SCREEN_NAME",
          "data": {
            "key": "value"
          }
        }
      }
    }
  }
}
```

**Data Exchange Encryption Protocol**:
1. **Key Setup**: Business generates a 2048-bit RSA key pair. Public key is registered with Meta via `/PHONE_NUMBER_ID/whatsapp_business_encryption`.
2. **Inbound Webhook (`nfm_reply`)**: Meta sends `encrypted_flow_data`, `encrypted_aes_key`, and `initial_vector` (IV).
3. **Decryption**:
   - Decrypt `encrypted_aes_key` using the RSA Private Key.
   - Decrypt `encrypted_flow_data` using AES-128-GCM with the decrypted AES key and the provided IV (Auth tag is typically appended to the ciphertext).
4. **Encryption (Response)**:
   - Perform a bitwise XOR with `0xFF` on the original IV to generate a new IV.
   - Encrypt the JSON response payload using AES-GCM with the original AES key and the new IV.
   - Base64-encode the result and return it to Meta.

**Webhook `nfm_reply` Structure**:
```json
{
  "type": "interactive",
  "interactive": {
    "type": "nfm_reply",
    "nfm_reply": {
      "response_json": "{\"flow_token\":\"...\",\"encrypted_flow_data\":\"...\",\"encrypted_aes_key\":\"...\",\"initial_vector\":\"...\"}",
      "body": "User submitted the flow",
      "name": "flow"
    }
  }
}
```
*Note: `response_json` is an escaped JSON string requiring a two-stage decode.*

---

## 3. Codebase Integration Points

1. **`internal/api/handler/message.go`**
   - **Key Func**: `func (h *MessageHandler) Create(c *echo.Context) error`
   - **Integration**: Extracts `req.TemplateName`. No direct changes needed here as long as `domain.CreateMessageRequest` already supports template components, which it does.
2. **`internal/outbound/processor.go`**
   - **Key Func**: `func (p *Processor) Ingest(ctx, workspaceID, traceID, req) (*domain.QueueMessage, error)`
   - **Integration**: The session window check is already partially implemented (lines 168-179). It correctly bypasses the check if `req.TemplateName != ""`.
3. **`internal/channel/whatsapp/waba.go`**
   - **Key Func**: `func (a *WABAAdapter) Dispatch(ctx context.Context, m *channel.MessagePayload) (string, error)`
   - **Integration**: Template dispatch logic exists (lines 191-280) mapping `m.Components` to `wabaTemplate`. Needs expansion to support `type: "flow"` interactive types for Meta Flows.
4. **`internal/session/window.go`**
   - **Key Func**: `func (w *WindowChecker) IsWindowOpen(...) (*WindowStatus, error)`
   - **Integration**: Supports 24h standard and 72h CTWA windows. This is stable.
5. **`internal/repository/waba_template.go`**
   - **Key Funcs**: `Upsert()`, `UpdateStatus()`
   - **Integration**: Tracks templates natively.
6. **`internal/api/handler/waba_webhook.go`**
   - **Key Func**: `func (h *WABAWebhookHandler) HandlePost(c *echo.Context) error`
   - **Integration**: Currently handles `message_template_status_update` events natively. Hook for Data Exchange (Flow decryption) needs to be inserted here, likely inspecting inbound interactive `nfm_reply` messages before handing off to the `inboundProcessor`.
7. **`internal/client/waba_meta.go`**
   - **Key Func**: `func (c *WABAMetaClient) SyncTemplates(...)`
   - **Integration**: Handles Meta Graph API sync.

---

## 4. Template Validation Rules

- **Name**: Max 512 characters. Lowercase alphanumeric and underscores only.
- **Header**: Max 60 characters for text. No markdown formatting allowed.
- **Body**: Max 4,096 characters. Standard markdown supported (`*bold*`, `_italic_`).
- **Footer**: Max 60 characters typically.
- **Variables**: No strict global limit, but all `{{#}}` variables must have sample data submitted.
- **Buttons**:
  - Max 3 Quick Reply buttons.
  - Max 2 Call-To-Action (CTA) buttons (typically one URL and one Phone Number).
- **Categories**: Must be exactly `MARKETING`, `UTILITY`, or `AUTHENTICATION`. Misclassification is a primary reason for rejection.

---

## 5. Key Findings & Risks

1. **JSON Decoding Risk**: The `nfm_reply.response_json` in webhooks is stringified JSON. Handlers must perform a double `json.Unmarshal` to access `encrypted_flow_data`.
2. **Encryption Overhead**: The Flows Data Exchange requires RSA + AES-GCM operations synchronously on the webhook HTTP path. This adds latency; the handler must respond within 15 seconds.
3. **IV Inversion**: A common pitfall in Flow response encryption is failing to correctly invert the Initialization Vector (XOR `0xFF`) before encrypting the response.
4. **WABA Dispatch Extension**: `internal/channel/whatsapp/waba.go` currently handles interactive buttons/lists, but needs the `type: "flow"` and `action: { name: "flow", parameters: {...} }` structs mapped in `wabaInteractiveAction` to dispatch Meta Flows via API.
