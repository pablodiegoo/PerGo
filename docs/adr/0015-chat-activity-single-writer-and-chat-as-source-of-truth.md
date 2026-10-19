# 0015. Chat Activity as Single Writer; the Chat Owns AI Engagement and the Customer Service Window

Status: accepted (partially supersedes ADR 0014 decision #5)

## Context

ADR 0014 extracted the **Chat Engine**. An architecture review then found that activity-driven Chat state was still written in five places: `chat.Engine` (`SendMessage`, `persistOutboundChatMessage`, `SetAIEnabled`, `ToggleContactBot`), `OutboundProcessor.recordOutboundChatActivity`, and the chat block of `InboundProcessor`, plus the Chatwoot webhook and the `pause_bot` / `close` webhook verbs. ADR 0014 decisions #2 and #6 were therefore never fully carried out. Two kinds of problem followed.

The first kind is duplicated logic and the bugs it has produced:

- Reopen-on-activity, the **Human Handoff Trigger**, and AI resume are each hand-copied in 3–5 places, and the handoff payload is copied verbatim.
- `chat.Engine` writes the outbound message itself whenever the Outbound Processor did not. Production and tests therefore run different paths.

The second kind is double state. The same facts are kept both per Contact and per Chat, and the **Customer Service Window** has two stores:

- **AI Engagement** is split between `chats.ai_disabled` and `contacts.bot_active` / `bot_paused_at`. A Contact has one Chat per Connection, so silencing AI on one Chat also silenced every other Chat of that Contact. The Inbound Debounce Buffer was keyed by Contact, so a handoff on one Chat also cancelled pending turns on another.
- Open/closed state is split between `chats.status` and `contacts.closed_at`. Nothing reads `closed_at` for business logic, and campaigns or verbs reopened the Contact without reopening the Chat.
- The window lives both in `recipient_sessions` and in `chats.service_window_expires_at`, and the window rule is copied three times. `recipient_sessions` also opened windows on *outbound* dispatch and on Inbox-initiated chats. It matched without the connection identity, so two WABA numbers could share one window.
- The window was enforced in 8+ places, selected per adapter through a `StrictServiceWindow` bool, and detected by matching error strings. `chat.Engine` re-checked the window after the message was already queued. The "default template fallback" had no writer at all.

## Decisions

1. **Chat Activity (`internal/chat/activity`) is the only writer of activity-driven Chat state.** That state covers inbound turns, outbound turns, the Human Handoff Trigger, manual AI off/resume, pauses, and Chat Snooze normalization. Triage, Internal Notes and Message Reactions stay in the Chat Engine.
   - Interface: `Outbound`, `Inbound`, `SetAI`, `DispatchFailed`, `TouchOutbound`, `Snapshot`, `ClaimExpiring`.
   - Chats are addressed by `ChatID`, by `ChatOf(connection, contact)`, which may create the Chat, or by `ChatAt(connection, address)`, which is read-only.
   - The rules live in pure functions in `domain`.
   - Each transition runs in one Postgres transaction owned by the module, using the `SELECT … FOR UPDATE` pattern. There is no generic transaction abstraction. Side effects run after commit and are best-effort.
   - The only ports are the Publisher and the debounce canceller. Neither the Store nor the clock is a port.
2. **The Chat is the single source of truth.**
   - **AI Engagement** is a Chat state: active, paused until T, or off. Off dominates.
   - A Human Handoff Trigger sets T = max(T, now + 12h), and T slides with each human message. Resume is derived from time, so there are no reset writes.
   - The `chat.handoff.*` event fires only when the state changes from engaged to not engaged.
   - Chat Snooze expiry is derived on read and normalized on the next transition. Reads never write.
   - `contacts.bot_active`, `bot_paused_at` and `closed_at` are dropped. The Inbound Debounce Buffer is keyed by Chat.
3. **Retire `recipient_sessions`.**
   - The window is materialized on the Chat as `window_expires_at` (`service_window_expires_at`). Only the inbound turn writes it, via `domain.ExtendServiceWindow(prev, at)`, which always grants a 24-hour **Customer Service Window** and never shortens an already open window. **Entry Point Type** (`standard` or `ctwa`) is recorded on the Chat as attribution metadata and preserved across follow-up messages while the 24-hour window remains open (see `docs/research/meta-ctwa-free-entry-point-window.md`).
   - No Chat means the window is closed.
   - The expiring-window ticker reads Chats, only for `whatsapp_cloud`.
4. **The Outbound Processor is the only window enforcer at ingest; the WABA adapter enforces at dispatch only for paths that bypass ingest.**
   - Enforcement returns one typed "template required" error.
   - The default template fallback and `StrictServiceWindow` are removed.
   - Outbound order is: gate and record as `queued` → commit → publish; a publish failure marks the message `failed`.
5. **The Human Handoff Trigger is derived from Sender Type (`human_agent`); `DisableAI` is removed.**
   - When no Sender Type is given, operator sessions default to `human_agent` and API keys to `ai_agent`.
6. Delivery in three slices: S1 covers AI Engagement and the outbound turn, S2 the inbound turn and Chat Snooze, S3 the Customer Service Window. PerGo is not yet in production, so columns and tables are dropped directly in new migrations, with no backfill and no deprecation period.

## Consequences

- This supersedes the part of ADR 0014 #5 that kept `recipient_sessions` as a fallback for stateless dispatches. Future reviews should not reintroduce a per-recipient window store or Contact-level AI or closed flags. Both were tried, and both diverged from the Chat.
- Paths that publish straight to the queue do not create Chats: campaigns, webhook verbs, Typebot, Chatwoot and device. They only touch an existing Chat's last outbound time, and their first-touch reply latency is lost. Routing them through a single outbound entry point is tracked separately.
- The derived-snooze SQL predicate used by Chat Engine list queries is exported from `activity`, and a parity test pins it to the `domain` rule.
- Tests target the Chat Activity interface against real Postgres (testcontainers) with in-memory Publisher and debounce adapters. Nobody mocks Chat Activity. Tests that exercised the non-production paths are deleted.
