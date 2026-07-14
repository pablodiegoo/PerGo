---
spike: 012
name: conversational-session-schema
type: standard
validates: "Given inbound messages from multiple channels, when mapped using contacts and conversations tables, then we can track logical two-way conversations and update sessions across channel restarts."
verdict: VALIDATED
related: [001, 004, 009]
tags: [db, schema, conversation]
---

# Spike 012: Conversational Session Schema

## What This Validates
This spike validates the database-driven session management architecture for tracking bidirectional conversational exchanges between workspaces/channels and external recipients, ensuring unread statuses and session states persist across restarts.

## Research
### Competing Approaches

| Approach | Tool/Library | Pros | Cons | Status |
|----------|-------------|------|------|--------|
| In-memory tracking | Go Maps / Mutexes | Extremely fast read/write, no database overhead. | State is lost on application restarts or worker crashes. Cannot sync across multiple instances. | Rejected |
| Client-side Cookie State | Browser Sessions | Offloads storage to clients, stateless backend. | Hard to sync for multi-agent inboxes. Subject to cookie size limits and security concerns. | Rejected |
| Database Recipient Sessions | PostgreSQL Table | Persistent, multi-instance safe, tracks active/read status per tenant. | Small database query overhead. | Chosen |

## How to Run
Run the repository test suite to verify the DB mapping and upsert logic:
```bash
go test ./internal/repository -run TestRecipientSessionRepository -v
```

## What to Expect
- Creation and retrieval of recipient sessions scoped to `workspace_id`, `recipient_phone`, `channel`, and `recipient_identity`.
- Atomic upserts using `ON CONFLICT (workspace_id, recipient_phone, channel, recipient_identity) DO UPDATE SET last_inbound_at = EXCLUDED.last_inbound_at`.
- Tracking of `last_read_at` to support unread badges in the Inbox UI.

## Investigation Trail
- **Iteration 1**: Initialized the database schema in `006_create_recipient_sessions.sql`.
- **Iteration 2**: Unified recipient identity columns in `013_recipient_sessions_to_column.sql` to support multiple connection instances cleanly.
- **Iteration 3**: Implemented `CountActiveByWorkspace` to compute dynamic onboarding status based on connected channel records.

## Results
- **Verdict**: **VALIDATED**
- **Evidence**: Verified that recipient sessions persist successfully in PostgreSQL, allowing the inbox to query unread badges and last inbound activity accurately.
