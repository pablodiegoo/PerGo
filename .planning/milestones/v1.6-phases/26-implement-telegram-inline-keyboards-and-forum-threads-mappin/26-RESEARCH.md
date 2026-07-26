# Phase 26: Implement Telegram Inline Keyboards and Forum Threads mapping - Research

## Overview
This phase requires mapping the unified interactive schema (from Phase 25) into Telegram's Bot API (Inline Keyboards) and correctly routing Telegram Forum Threads context into the platform.

### What do I need to know to PLAN this phase well?

#### 1. Inbound Mapping (Webhooks)
To handle incoming Callback Queries (button clicks) and Forum Threads:
- **Struct Updates in `internal/channel/telegram/inbound.go`:** 
  - Update `telegramMessage` to include `MessageThreadID int64 json:"message_thread_id,omitempty"`.
  - Update `telegramUpdate` to include `CallbackQuery *telegramCallbackQuery json:"callback_query,omitempty"`.
  - Add `telegramCallbackQuery` struct with `ID`, `From`, `Message`, and `Data`.
- **Parsing logic in `TelegramInboundAdapter.Parse`:**
  - Branch logic to handle `update.CallbackQuery != nil` alongside `update.Message != nil`.
  - For `CallbackQuery`, use `update.CallbackQuery.Message.Chat` for mapping the contact/group.
  - Map `update.CallbackQuery.Data` to the newly established inbound interactive schema.
  - Acknowledge the callback query by making an HTTP GET/POST to `answerCallbackQuery` in the adapter to prevent the client from showing a loading spinner indefinitely.
  - For Forum Threads, capture `MessageThreadID` (from either `update.Message` or `update.CallbackQuery.Message`) and attach it to `InboundEvent.Metadata["thread_id"]`.
- **Inbound Event Schema (`internal/inbound/processor.go`):**
  - Add `Interactive *InboundInteractive` to `InboundEvent` and `InboundEventPayload`.
  - Add `InboundInteractive` and `InboundButtonReply` structs to represent the `button_reply` interaction type payload (as specified by context D-03).
  - Add mapping in `InboundProcessor.Process()` so that this new field flows down to JetStream and the audit logs.

#### 2. Outbound Mapping (Dispatch)
To send unified interactive components as Telegram Inline Keyboards:
- **Schema Mapping in `internal/channel/telegram/telegram.go`:**
  - `MessagePayload` already holds `Interactive *domain.Interactive` (from Phase 25).
  - Update `telegramMessageRequest` to include `MessageThreadID string json:"message_thread_id,omitempty"` and `ReplyMarkup *inlineKeyboardMarkup json:"reply_markup,omitempty"`.
  - Add `inlineKeyboardMarkup` and `inlineKeyboardButton` private structs.
  - In `TelegramAdapter.Dispatch`, if `m.Metadata["thread_id"]` exists, map it to `MessageThreadID`.
  - In `TelegramAdapter.Dispatch`, if `m.Interactive != nil` and `m.Interactive.Type == "button"`, iterate over `m.Interactive.Action.Buttons` and construct an `inlineKeyboardMarkup` with each button mapped to a row. Map `Button.Reply.ID` to `callback_data` and `Button.Reply.Title` to `text`.
  
#### 3. Cross-Cutting Concerns
- Ensure tests in `telegram_webhook_test.go` and `internal/channel/telegram/telegram_test.go` are updated to simulate `CallbackQuery` events and Forum Threads metadata.
- Ensure the newly added `Interactive` struct on `InboundEventPayload` passes correctly through the webhook publisher and is formatted nicely for consumers.
