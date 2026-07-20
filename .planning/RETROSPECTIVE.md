# Project Retrospective

*A living document updated after each milestone. Lessons feed forward into future planning.*

## Milestone: v1.2 — PRD Gaps Integration

**Shipped:** 2026-07-17
**Phases:** 4 | **Plans:** 8

### What Was Built
- Multi-Webhook Subscriptions: multi-endpoint webhook configuration, wildcard glob filtering, parallel NATS JetStream workers, DLQ diagnostics settings dashboard.
- Omnichannel Contact Merging: contact/identities database schema, auto-resolution on inbound/outbound events, transactional merge consolidation with rollbacks.
- Webhook Response Verbs Engine: sequential JSON verbs execution loop with 10s wait and 30s context limits, logged to workspace audits.
- Meta WhatsApp Status Updates & Inbox Badges: Meta callback parsing, message status updates in database, and visual delivery checkmarks in Chat UI bubbles.

### What Worked
- Decoupling webhook response verbs execution asynchronously via goroutines using `context.Background()` prevented blocking worker resources.
- Safe transactional contact merge consolidation (`ContactRepository.MergeContacts`) prevented orphan messages or identities during merge operations.
- Writing E2E integration tests matching real callback status lifecycles allowed robust testing of asynchronous status receipts.

### What Was Inefficient
- Incomplete quick tasks lacked standard frontmatter in their summaries, causing audit check warnings until corrected.

### Patterns Established
- Storing tags using TEXT[] arrays with GIN indexing in PostgreSQL for highly efficient filter operations.
- Storing provider message IDs (`provider_message_id`) with composite index keys on outbound dispatches for fast Meta webhook callback matching.

### Key Lessons
- Decoupling execution context via `context.Background()` is crucial when spawning asynchronous tasks from request contexts.
- Keep quick task SUMMARY.md files structured with frontmatter (`status: complete`) from the beginning to pass automatic audit scanners.

## Milestone: v1.3 — Chatwoot & Typebot Integrations

**Shipped:** 2026-07-20
**Phases:** 7 | **Plans:** 10

### What Was Built
- Chatwoot integration: Workspace-scoped configuration dashboard, native outbound webhook receiver, bidirectional client/syncer sync engine mapping contacts and messages.
- Typebot integration: Settings panel, postgres session mapping, asynchronous customer message forwarder, and bot replies webhook receiver.
- Stateful Handoff Routing: Contact `bot_active`/`bot_paused_at` state model, automatic agent reply interceptors (webhooks + composer), manual status toggle HTMX badge, `pause_bot` verb, and 12h lazy inactivity reset.
- Polymorphic Verbs Refactoring: Decoupled monolithic switch block into testable polymorphic `VerbHandler` structs wired statically in the constructor.
- Typebot Ingestion & E2E Tracing: Reconciled settings schema and wired TypebotForwarder inside the composition root (Phase 24.1). Enriched forwarder queue messages with ConnectionID, SenderIdentity, and TraceID (Phase 24.2). Implemented media placeholder mapping and unique session formats (Phase 24.2.1).

### What Worked
- Reusing the same `integrations` table with encrypted JSON configurations kept the schema clean and modular.
- Isolating bot execution control to a single interceptor layer in `TypebotForwarder` simplified conditional checks.
- Mapping webhook integration tests using mocks (mockPublisher, mockRouteResolver) allowed testing handler routing logic without NATS or DB dependencies.

### What Was Inefficient
- Serializing tests sequentially with `-p 1` was required due to parallel package test runs conflicting on the same dev PostgreSQL database migration state.

### Patterns Established
- Polymorphic Command Pattern: Encapsulating individual execution steps into self-contained handlers implementing a common interface to improve code leverage.
- Stateful Session Handoff: Managing conversational ownership between bot and human agents using database status states and lazy-evaluated inactivity resets.

### Key Lessons
- Static mapping maps within constructors are effective for resolving polymorphic interfaces in Go without dependency injection frameworks.
- Lazy evaluation of timeouts (e.g. cooldown reset on incoming message) avoids running persistent background crons/daemons for state management.

## Milestone: v1.4 — Omnichannel Integrations

**Shipped:** 2026-07-20
**Phases:** 3 | **Plans:** 3

### What Was Built
- Unified interactive message mapping and override handling for WABA and Whatsmeow with graceful degradation fallbacks.
- Telegram inline keyboards and forum threads support via `Interactive` payloads and `thread_id` metadata.
- Instagram channel adapters implemented with full support for Outbound messaging, Story Mentions, and Quick Replies.

### What Worked
- Domain separation between the unified schema (Interactive) and channel-specific adapters (WABA, Whatsmeow, Telegram, Instagram) proved highly effective for omnichannel support without leaky abstractions.
- Leveraging `channel_overrides` provided a robust escape hatch for features not yet unified in the platform.

### What Was Inefficient
- Requirement tracking fell out of sync with actual development velocity and had to be caught during the audit phase.

### Patterns Established
- Channel overrides escape hatch for vendor-specific JSON configurations.

### Key Lessons
- Regular tracking and maintenance of requirements traceability matrix prevents audit surprises at milestone completion.

---

## Cross-Milestone Trends

### Process Evolution

| Milestone | Phases | Key Change |
|-----------|--------|------------|
| v1.1 | 6 | Campaign Engine bulk messaging with JetStream batch throttling. |
| v1.2 | 4 | PRD gaps integration: webhook subscriptions, contact merging, verbs engine, read receipts. |
| v1.3 | 7 | Chatwoot & Typebot integrations, stateful handoff routing, polymorphic VerbHandlers, and gap closures. |
| v1.4 | 3 | Unified Interactive schemas, Telegram threads/keyboards, Instagram Stories/replies. |

### Cumulative Quality

| Milestone | Tests | Zero-Dep Additions |
|-----------|-------|-------------------|
| v1.1 | Passed | goose, uuid |
| v1.2 | Passed | mark3labs/mcp-go |
| v1.3 | Passed | *(none)* |
| v1.4 | Passed | *(none)* |
