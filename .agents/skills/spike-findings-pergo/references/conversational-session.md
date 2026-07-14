# Conversational Session Management

## Requirements

- Must support tracking bidirectional conversational sessions with recipients on a channel.
- Must persist the last inbound timestamp and last read status to compute unread notifications.
- Must handle multiple sender connections per workspace dynamically.

## How to Build It

1. **Database Schema:** Create a `recipient_sessions` table with composite key tracking.
   ```sql
   CREATE TABLE recipient_sessions (
       workspace_id UUID NOT NULL,
       recipient_phone VARCHAR(50) NOT NULL,
       channel VARCHAR(50) NOT NULL,
       recipient_identity VARCHAR(100) NOT NULL,
       last_inbound_at TIMESTAMPTZ NOT NULL,
       last_read_at TIMESTAMPTZ,
       PRIMARY KEY (workspace_id, recipient_phone, channel, recipient_identity)
   );
   ```

2. **Repository Implementation:** Provide an `Upsert` method to update session timestamps on new inbound messages without throwing duplication errors:
   ```go
   func (r *RecipientSessionRepository) Upsert(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string, recipientIdentity string, lastInboundAt time.Time) error {
       _, err := r.pool.Exec(ctx,
           `INSERT INTO recipient_sessions (workspace_id, recipient_phone, channel, recipient_identity, last_inbound_at)
            VALUES ($1, $2, $3, $4, $5)
            ON CONFLICT (workspace_id, recipient_phone, channel, recipient_identity)
            DO UPDATE SET last_inbound_at = EXCLUDED.last_inbound_at`,
           workspaceID, recipientPhone, channel, recipientIdentity, lastInboundAt,
       )
       return err
   }
   ```

3. **Read Status Tracking:** Update the `last_read_at` timestamp when the operator views the active conversation panel:
   ```go
   func (r *RecipientSessionRepository) UpdateLastReadAt(ctx context.Context, workspaceID uuid.UUID, recipientPhone, channel, recipientIdentity string, lastReadAt time.Time) error {
       _, err := r.pool.Exec(ctx,
           `UPDATE recipient_sessions
            SET last_read_at = $5
            WHERE workspace_id = $1 AND recipient_phone = $2 AND channel = $3 AND recipient_identity = $4`,
           workspaceID, recipientPhone, channel, recipientIdentity, lastReadAt,
       )
       return err
   }
   ```

## What to Avoid

- **In-Memory Cache Only:** Do not rely on purely in-memory states for conversations, as restarts will wipe the read statuses and cause duplicate unread counters.
- **Strict Timestamps on Fetch:** Avoid using local server timestamps for cursor pagination. Always rely on Postgres transaction-safe timestamps or auto-incrementing serial IDs.

## Constraints

- Ensure the database connection pooling uses `pgx/v5` and supports concurrent writes from multiple worker routines.

## Origin

Synthesized from spikes: 012
Source files available in: sources/012-conversational-session-schema/
