---
status: resolved
trigger: "Telegram conversation inbox mapping and api delivery issues"
created: 2026-07-09
updated: 2026-07-09
symptoms:
  expected: "1. Outbound messages sent to Telegram usernames (e.g. @username) or phone numbers should merge into the same chat history as inbound messages received from numeric chat IDs.\n2. Telegram webhook and APIs should function correctly using connection details."
  actual: "1. Outbound messages sent to username create a separate, disconnected chat thread in the inbox instead of joining the inbound thread.\n2. Telegram inbound webhook returns 403 because it loads credentials from the old credentials table instead of the unified connections table."
  error_messages: "403 Forbidden on Telegram webhooks"
  timeline: "Always since Phase 10 Connection Unification."
  reproduction: "1. Add a Telegram connection in Conexões.\n2. Send a Telegram inbound message -> webhook fails with 403.\n3. Send outbound message to username -> creates a separate chat thread."
resolution: "1. Created the `telegram_contacts` table and corresponding repository/tests to map usernames and phone numbers to Telegram numeric chat IDs.\n2. Updated `telegram_webhook.go` and `waba_webhook.go` to load configs from the new unified `connections` table, resolving the 403 errors and deprecating the old `credentials` table.\n3. Hooked `telegram_contacts` into `telegram_webhook.go` to dynamically upsert user profiles from inbound updates.\n4. Hooked `telegram_contacts` into the outbound orchestrator (`orchestrator.go`) to resolve usernames/phones to numeric `chat_id`s during dispatch and normalize the audit log record.\n5. Integrated display name decoration into `inbox.go` and `conv_item.templ` to show real user names/usernames in the sidebar."
---

# Debug Session: telegram-inbox-conversation-issue - RESOLVED
