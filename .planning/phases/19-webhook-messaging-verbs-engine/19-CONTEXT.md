# Phase 19: Webhook Messaging Verbs Engine - Context

**Gathered:** 2026-07-16
**Status:** Ready for planning

<domain>
## Phase Boundary

This phase implements a declarative messaging verbs engine. When PerGo delivers an inbound message webhook to a configured endpoint, the endpoint can respond with a JSON array of sequential verbs. PerGo parses and executes these verbs on behalf of the client application.

Verbs supported:
- `reply`: Dispatches an outbound message reply.
- `wait`: Blocks the execution flow with standard select delays.
- `forward`: Redirects the incoming message to another recipient or channel.
- `tag`: Appends one or more tags to the resolved Contact's metadata tags.
- `close`: Marks the resolved Contact's thread as closed/resolved in the Inbox.

## Key Decisions

1. **Background Async Execution**: Verb execution occurs asynchronously in a separate goroutine spawned by the webhook worker/dispatcher after a successful 2xx response, keeping NATS worker threads free from blocking waits.
2. **Execution Timeout**: The execution context has a maximum timeout of 30 seconds to prevent resource exhaustion.
3. **Wait Cap Enforcement**: Individual `wait` durations are capped at a maximum of 10 seconds.
4. **Data Model Updates**:
   - `contacts` table is modified to include `tags TEXT[] NOT NULL DEFAULT '{}'` and `closed_at TIMESTAMPTZ`.
   - `tag` verb appends tags to `contacts.tags` using SQL `array_cat` or similar array operations.
   - `close` verb sets `contacts.closed_at = NOW()`. Opening/sending a new message resets `closed_at = NULL`.
5. **NATS Queue Integration**: Outbound actions (`reply` and `forward`) construct `domain.QueueMessage` payloads and publish them to `messages.outbound` NATS subject.
6. **Audit Logs**: Execution results (successes and validation/runtime errors) are stored in the user action logs (`logs/actions` / `user_action_logs` table) under category `webhook.verbs`.

## Tech Stack Alignment
- **Go**: 1.25+ standard library (`time.ParseDuration`, JSON unmarshalling, context propagation).
- **PostgreSQL**: Native array queries and indexes for fast tag lookups.
- **NATS**: Publisher injection into the webhook dispatcher/executor.
</domain>
