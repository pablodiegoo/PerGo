# Phase 26: Implement Telegram Inline Keyboards and Forum Threads mapping - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-20
**Phase:** 26-Implement Telegram Inline Keyboards and Forum Threads mapping
**Areas discussed:** Inline vs Reply Keyboards, Forum Threads Mapping, Callback Query Routing

---

## Inline vs Reply Keyboards

| Option | Description | Selected |
|--------|-------------|----------|
| (Recommended) Only Inline Keyboards for now. | They map perfectly to the unified interactive button schema we built in Phase 25. Reply keyboards are rare in modern chat apps. | ✓ |
| Support both. | We need to add a `keyboard_type` or similar flag to the unified JSON schema. | |
| Other | write in | |

**User's choice:** (Recommended) Only Inline Keyboards for now. They map perfectly to the unified interactive button schema we built in Phase 25. Reply keyboards are rare in modern chat apps.
**Notes:** None

---

## Forum Threads Mapping

| Option | Description | Selected |
|--------|-------------|----------|
| (Recommended) Treat them as metadata (`thread_id`) on the existing Group Contact. | This keeps the Contact representing the actual group entity, and replies can just specify the `thread_id` to route correctly. | ✓ |
| Treat forum threads as separate Contacts. | This makes it easier for agents/users to manage them as distinct conversation channels, avoiding multiplexing in a single inbox view. | |
| Other | write in | |

**User's choice:** (Recommended) Treat them as metadata (`thread_id`) on the existing Group Contact. This keeps the Contact representing the actual group entity, and replies can just specify the `thread_id` to route correctly.
**Notes:** None

---

## Callback Query Routing

| Option | Description | Selected |
|--------|-------------|----------|
| (Recommended) Ingest as standard inbound messages | but with a specialized `interactive` or `button_reply` structure/flag in the inbound schema so developers can easily match button clicks. | ✓ |
| Ingest them strictly as text | (e.g. mimicking the user typing the button text). Simple, but loses context that it was a button click. | |
| Other | write in | |

**User's choice:** (Recommended) Ingest as standard inbound messages but with a specialized `interactive` or `button_reply` structure/flag in the inbound schema so developers can easily match button clicks.
**Notes:** None

---

## the agent's Discretion

None

## Deferred Ideas

None
