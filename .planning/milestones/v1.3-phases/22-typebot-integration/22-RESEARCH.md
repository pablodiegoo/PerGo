# Phase 22: Typebot Integration - Research & Implementation Plan

## 1. Typebot API Specifications

To integrate with Typebot's execution engine, we need to interact with its public chat APIs. Assuming standard Typebot V3 API specifications:

- **Start Chat**
  - **Endpoint:** `POST {API_URL}/api/v1/typebots/{bot_id}/startChat`
  - **Headers:** `Authorization: Bearer {PublicToken}` (if required/configured).
  - **Payload:** Can include `prefilledVariables` (e.g., passing the contact's name or phone number).
  - **Response:** Returns a `sessionId` and an array of `messages` to display to the user.

- **Continue Chat**
  - **Endpoint:** `POST {API_URL}/api/v1/sessions/{session_id}/continueChat`
  - **Headers:** `Authorization: Bearer {PublicToken}`
  - **Payload:** `{"message": {"type": "text", "text": "Customer reply..."}}`
  - **Response:** Returns the next batch of `messages` from the bot.

## 2. Codebase Insertion Points

The following areas of the PerGo codebase will need to be updated or created:

1. **Database Schema:** 
   - Add `028_create_typebot_sessions.sql` in `internal/platform/postgres/migrations/` to track active sessions.
   - Table: `typebot_sessions` with PK `(workspace_id, contact_id, connection_id)`, plus `typebot_session_id TEXT` and `updated_at TIMESTAMPTZ`.

2. **Integration Client & Forwarder:**
   - Create `internal/integration/typebot/` containing `client.go` (HTTP client for Typebot API) and `forwarder.go` (logic for session state and mapping).
   - Create `internal/repository/typebot_session.go` to manage CRUD operations for the active sessions.

3. **Inbound Processor Hook:**
   - In `internal/inbound/processor.go`, inject `TypebotForwarder`.
   - Inside `Process()`, add an async block (similar to `chatwootSyncer`) to check for an active session or a trigger keyword match, and forward the message to Typebot.

4. **Webhook Handler (Outbound from Typebot):**
   - Create `internal/api/handler/typebot_webhook.go` to receive asynchronous messages (e.g., triggered by Typebot's "Webhook" block).
   - Register route `POST /api/integrations/typebot` in `cmd/pergo/main.go`.

5. **Admin Configuration UI:**
   - Create `internal/api/handler/admin/typebot.go` to handle `GetSettings` and `PostSettings`.
   - Register `GET/POST /workspaces/:workspace_id/integrations/typebot` in `cmd/pergo/main.go`.
   - Create templ templated views in `templates/pages/typebot_settings.templ`.

## 3. Reusable Patterns from Existing Integrations

We will heavily reuse patterns established by the Chatwoot integration in Phase 21:

- **Credentials Storage:** Use `repository.IntegrationRepository` and the `integrations` table. Typebot configurations will be serialized into a JSON array and encrypted inside the `config` BYTEA column.
  ```go
  type TypebotBotsConfig struct {
      Bots []TypebotBot `json:"bots"`
  }
  ```
- **Async Execution:** Following the `p.chatwootSyncer.SyncInboundMessage(ctx, c, e)` pattern, Typebot forwarding will happen in a detached goroutine with a timeout context to avoid blocking the main JetStream consumer thread.
- **Queue Publishing:** The webhook handler `TypebotWebhookHandler` will construct a `domain.QueueMessage` and publish it via `h.publisher.Publish(ctx, "messages.outbound", payload, traceID)` to push messages out to the user.

## 4. Validation Architecture

To ensure the bot integration functions reliably and gracefully handles edge cases, the following validation and testing architecture must be implemented:

### Unit Tests
- **Typebot API Client (`client_test.go`):** Use `httptest.Server` to mock the Typebot execution API. Test parsing of various message types (text, images, choices) returned by `startChat` and `continueChat`.
- **Webhook Handler (`typebot_webhook_test.go`):** Test that valid webhook payloads correctly map to the target connection/contact and successfully publish to the `messages.outbound` NATS JetStream subject. Test failure paths (missing workspace API key).

### Integration Tests
- **Session Persistence (`typebot_session_test.go`):** Validate the composite primary key constraint on `(workspace_id, contact_id, connection_id)`. Test the `updated_at` upsert logic.

### Edge Cases to Validate
- **Remote 404 (Session Expired):** If `continueChat` returns a 404 or session invalid error, the system MUST catch this, delete the local `typebot_sessions` row, and automatically retry by initiating a `startChat` request.
- **Trigger Keyword Collisions:** If a user sends a trigger word while *already* in an active session, the system MUST ignore the trigger and route the input to the existing session.
- **Local Inactivity Timeout:** Validate that messages received after a configured local timeout (e.g., 30 minutes since `updated_at`) automatically invalidate the old session and start a new one.

## 5. Risks and Mitigations

- **Risk:** **Synchronous API Bottlenecks.** Slow responses from Typebot could delay message processing if not handled correctly.
  - **Mitigation:** Wrap the Typebot HTTP calls in a goroutine with a strict `context.WithTimeout` (e.g., 5-10 seconds), returning early from the inbound processor.

- **Risk:** **Complex UI for Multiple Bots.** Managing multiple bot configurations (with trigger words and default fallbacks) inside a single page form can be complex.
  - **Mitigation:** Implement a clear list-based UI in HTMX/Templ. Use JSON form arrays or a simple "Add Bot" modal to structure the payload correctly before hitting the `PostSettings` endpoint. 

- **Risk:** **Media Ingestion.** Typebot doesn't natively consume raw WhatsApp binary media seamlessly in its text input.
  - **Mitigation:** When a customer sends media, PerGo should format the message text as the public S3 proxy URL of the media (e.g., `[Image: https://pergo.../media/xyz]`) so the bot can process or log it as a URL string.
