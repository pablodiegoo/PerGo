# Phase 23: Stateful Handoff Routing - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-17
**Phase:** 23-Stateful Handoff Routing
**Areas discussed:** Inbox Chat UI Toggle Presentation (HAND-05), Auto-Reset Cooldown Reset Mechanism (HAND-06), Behavior of `pause_bot` Messaging Verb (HAND-04), Inbox Composed Message Pause Behavior

---

## Inbox Chat UI Toggle Presentation (HAND-05)

| Option | Description | Selected |
|--------|-------------|----------|
| Status Badge Toggle | A colored status badge (e.g. green 'Bot Ativo' or grey 'Bot Pausado') next to the contact name in the header that toggles immediately on click. | ✓ |
| Toggle Slider Switch | A sliding switch component in the header next to the 'Mesclar Contato' button. | |
| Dropdown Menu Option | Add 'Pausar Bot' / 'Ativar Bot' as options inside a new 'Ações' dropdown menu in the header. | |
| You decide. | The agent resolves the visual styling and placement. | |

**User's choice:** Status Badge Toggle
**Notes:** Provides a highly visual, zero-friction status indicator in the chat thread header.

---

## Auto-Reset Cooldown Reset Mechanism (HAND-06)

| Option | Description | Selected |
|--------|-------------|----------|
| Lazy Evaluation | Evaluated when a new customer message arrives. If the bot is currently paused and more than 12 hours have elapsed since the last human message, reset `bot_active` to `true` and process the message with the bot. (Simplest, no background running overhead). | ✓ |
| Background Cron Job | Run a database cleanup worker periodically (e.g. every hour) that checks for paused contacts exceeding 12 hours and resets them. (Slightly heavier, but keeps state up to date in real-time without needing an inbound message). | |
| You decide. | The agent chooses the implementation. | |

**User's choice:** Lazy Evaluation
**Notes:** Avoids background daemon overhead by checking elapsed time dynamically during inbound processing.

---

## Behavior of `pause_bot` Messaging Verb (HAND-04)

| Option | Description | Selected |
|--------|-------------|----------|
| Pause Indefinitely | The bot remains paused until either a manual toggle in PerGo UI or a human agent interaction occurs. | ✓ |
| Pause for default duration | Automatically apply the default 12-hour cooldown if duration is omitted. | |
| You decide. | The agent chooses the default. | |

**User's choice:** Pause Indefinitely
**Notes:** Omitted duration parameter is treated as a manual/permanent pause until explicitly unpaused.

---

## Inbox Composed Message Pause Behavior

| Option | Description | Selected |
|--------|-------------|----------|
| Auto-pause on compose | Yes, automatically flip `bot_active` to `false` and set `bot_paused_at = NOW()` when an operator sends a message directly from PerGo's Inbox page. (Ensures that manual human intervention always halts bot automation). | ✓ |
| Manual toggle only | Do not auto-pause when composing messages in PerGo UI; only auto-pause on Chatwoot webhook events and manual toggle button click. (Allows sending occasional messages without stopping the bot). | |
| You decide. | The agent resolves the behavior. | |

**User's choice:** Auto-pause on compose
**Notes:** Guarantees that any human agent messaging (either via Chatwoot sync or direct PerGo Inbox UI compose) pauses the bot automatically to avoid bot-human crosstalk.

---

## the agent's Discretion

- Styling colors and transitions of the status badge component to ensure premium visual aesthetics.
- Specific SQL queries and indexes to retrieve the contact's last outgoing human message.

## Deferred Ideas

None — discussion stayed within phase scope.
