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

---

## Cross-Milestone Trends

### Process Evolution

| Milestone | Phases | Key Change |
|-----------|--------|------------|
| v1.1 | 6 | Campaign Engine bulk messaging with JetStream batch throttling. |
| v1.2 | 4 | PRD gaps integration: webhook subscriptions, contact merging, verbs engine, read receipts. |

### Cumulative Quality

| Milestone | Tests | Zero-Dep Additions |
|-----------|-------|-------------------|
| v1.1 | Passed | goose, uuid |
| v1.2 | Passed | mark3labs/mcp-go |
