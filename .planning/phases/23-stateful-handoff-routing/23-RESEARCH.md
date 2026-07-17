# Phase 23: Stateful Handoff Routing - Research

## Overview
This phase introduces stateful control for bot conversational handoff, preventing bots (like Typebot) from conflicting with human agents. It involves extending the `contacts` schema to track bot activity, adding a manual UI toggle, extending the webhook verbs engine to support a `pause_bot` verb, and auto-managing the pause state during human intervention or after prolonged inactivity.

## Codebase Insertion Points

1. **Database Schema & Domain Model**
   - File: `internal/domain/contact.go`
     - Add `BotActive` (bool) and `BotPausedAt` (*time.Time) to `Contact` struct.
   - File: `internal/repository/contact.go`
     - Update queries (e.g. `ResolveContact`, `GetByID`, `SearchContacts`) to scan the new columns.
     - Add a new method `UpdateBotState(ctx, workspaceID, contactID, botActive bool, pausedAt *time.Time)` to toggle the flag.
     - Alternatively, consider adding a `BotCooldownExpiresAt` column if `pause_bot` verb durations need strict DB enforcement without relying on calculating `NOW() - BotPausedAt`.

2. **Typebot Integration**
   - File: `internal/integration/typebot/forwarder.go` (`SyncInboundMessage`)
     - Check `contact.BotActive` early. If `false`, skip forwarding the message to Typebot (return `nil`). 
     - **Note**: The auto-reset of the bot state (D-07) happens in `InboundProcessor` *before* the forwarder is called, so `contact.BotActive` will always be up-to-date here.

3. **Inbound Processor & Cooldown Auto-Reset**
   - File: `internal/inbound/processor.go` (`Process`)
     - After resolving the `contact` profile, implement D-07: check if `!contact.BotActive` and `contact.BotPausedAt != nil`.
     - Calculate inactivity: `if time.Since(*contact.BotPausedAt) > 12 * time.Hour` (default, or configurable), then call `ContactRepository.UpdateBotState(..., true, nil)`.
     - Update the local `contact` pointer to reflect `BotActive = true` so downstream syncs (Chatwoot, Typebot) receive the updated state.

4. **Human Agent Auto-Pause (Chatwoot & Admin Inbox)**
   - File: `internal/api/handler/chatwoot_webhook.go` (`Handle`)
     - Under the D-06 filter (outgoing, public user messages), after verifying it's a valid human agent reply, invoke `ContactRepository.UpdateBotState(..., false, time.Now())` using the resolved `contact_id`.
   - File: `internal/api/handler/admin/inbox.go` (`SendMessage`)
     - When sending an outbound message from the native Inbox UI, invoke `ContactRepository.UpdateBotState(..., false, time.Now())`. (You may need to ensure `contact_id` is parsed or resolved within the form submission, potentially updating `templates/components/chat_panel.templ` to pass `<input type="hidden" name="contact_id" ... />`).

5. **Messaging Verbs Engine Extension**
   - File: `internal/webhook/verbs.go` (`execute` loop)
     - Add a new `case "pause_bot":` block.
     - Parse a `PauseBotParams` struct (e.g. `{"duration": "2h"}`).
     - If `duration` is missing, pause indefinitely (update DB with `BotActive=false`, `BotPausedAt=NOW()`).
     - If `duration` is present, you can parse it (e.g. `time.ParseDuration(p.Duration)`). Since the cooldown mechanism uses `BotPausedAt`, a quick implementation could offset `BotPausedAt` backwards (e.g., `NOW() - 12h + duration`) so the 12-hour check will trip exactly when the requested duration expires. A cleaner alternative is explicitly tracking a `BotResumeAt` timestamp in the database, requiring an additional DB column.

6. **Admin UI Toggle**
   - File: `templates/components/chat_panel.templ`
     - Inject a manual toggle status badge (e.g. a pill-shaped button) next to the contact name in the panel header.
     - The button should have an `hx-post="/admin/contacts/{id}/toggle-bot"` endpoint that switches `BotActive` and updates the badge.
     - Styling: e.g. `bg-emerald-50 text-emerald-700` for active, `bg-zinc-100 text-zinc-600` for paused.
   - File: `internal/api/handler/admin/contacts.go` (or similar)
     - Add the HTMX handler to flip the state and return the updated badge partial.

## Detailed Typebot API Specs

The Typebot integration uses the standard `/api/v1/sessions` endpoints. 
- During `StartChat`, the bot opens a session. 
- During `ContinueChat`, messages flow back and forth. 
Because the Typebot engine is stateless between webhook events from our perspective, simply NOT calling `StartChat` or `ContinueChat` when `BotActive == false` is sufficient to "pause" the bot. Typebot sessions expire naturally based on Typebot's internal TTLs, so when `BotActive` becomes `true` again, the customer's next message will either seamlessly continue their session (if still valid) or trigger a new `StartChat`.

## Reusable Patterns
- **Database Modifiers:** Look at `ContactRepository.AddTags` or `CloseThread` as patterns for `UpdateBotState`. They use `UPDATE contacts SET ... WHERE workspace_id = $1 AND id = $2`.
- **Verbs Parsing:** The pattern for parsing `WaitParams` via `time.ParseDuration` in `internal/webhook/verbs.go` can be perfectly reused for parsing the `duration` parameter of the `pause_bot` verb.
- **Chatwoot Syncer:** We already intercept messages in `chatwoot_webhook.go`. Using the mapping logic to resolve `mapping.ContactID` and updating the contact repo inline matches the architectural flow of how `SendMessage` currently resolves recipients.

## Validation Architecture

To ensure the handoff routing doesn't cause regressions, the following testing architecture is required:

### Unit Tests
- **Verbs Engine (`internal/webhook/verbs_test.go`)**: 
  - Mock `ContactRepository.UpdateBotState`.
  - Assert that a webhook task containing the `pause_bot` verb correctly invokes the mock with `BotActive = false`.
  - Validate duration parsing (e.g., `duration: "2h"` results in the correct pause logic).
- **Inbound Processor Cooldown (`internal/inbound/processor_test.go`)**:
  - Mock an incoming message for a contact with `BotActive = false` and `BotPausedAt` set to 13 hours ago.
  - Assert that `ContactRepository.UpdateBotState(..., true, nil)` is called.
  - Mock an incoming message with `BotPausedAt` set to 1 hour ago.
  - Assert that `UpdateBotState` is NOT called.

### Integration Tests
- **Typebot Forwarder (`internal/integration/typebot/forwarder_test.go`)**:
  - Test `SyncInboundMessage` with a mocked `contact` where `BotActive = false`. 
  - Assert that no network calls to the Typebot API are made, and no messages are published to `messages.outbound`.
- **Database Verification**:
  - Validate that `BotActive` properly defaults to `true` on row creation.

### Edge Cases to Cover
- **Invalid Durations**: A webhook payload with `{"duration": "invalid"}` should gracefully log a failure in `ExecutedVerbLog` and skip pausing without crashing the engine.
- **Concurrent Updates**: Ensure `UpdateBotState` is safe under concurrent incoming messages (e.g., rapid bursts from a customer). Updating flags on the `contacts` table is generally safe via PostgreSQL row-level locks.
- **Missing `contact_id` in Inbox form**: Ensure the Inbox UI's HTMX request correctly includes the `contact_id` so that the admin's manual replies can accurately target the correct record.

## Risks and Mitigations

1. **Risk:** The `pause_bot` duration hack (offsetting `BotPausedAt` to manipulate the 12-hour fixed cooldown) is brittle if the global 12-hour cooldown becomes configurable per workspace.
   - **Mitigation:** Best practice is to add a `BotResumeAt` (`TIMESTAMP NULL`) column. If `BotResumeAt` is in the past, or if it's `NULL` but `BotPausedAt` is older than 12 hours, then reset the bot. This isolates manual explicit durations from the fallback inactivity timer.
2. **Risk:** Typebot sessions expire in Typebot's backend while the bot is paused, causing errors when the bot is unpaused.
   - **Mitigation:** Our `TypebotForwarder` already catches HTTP 404s on `ContinueChat`, deletes the stale session, and gracefully falls back to `StartChat`. This self-healing mechanism naturally protects against pause-induced session expiry.
3. **Risk:** Chatwoot outgoing webhook latency vs. Typebot response times. If a human replies, there might be a race condition where Typebot fires a response before the Chatwoot webhook pauses the bot.
   - **Mitigation:** The system guarantees eventual consistency. If exact serialization is required, we would need to pause the bot directly inside Chatwoot API wrappers, but for v1.3 webhook-based toggling is sufficient and standard practice for these integrations.
