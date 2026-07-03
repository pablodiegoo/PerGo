-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
    -- 1. Add column if not exists
    IF NOT EXISTS (
        SELECT 1 
        FROM information_schema.columns 
        WHERE table_name='recipient_sessions' AND column_name='recipient_identity'
    ) THEN
        ALTER TABLE recipient_sessions ADD COLUMN recipient_identity TEXT NOT NULL DEFAULT '';
    END IF;

    -- 2. Drop primary key constraint if it exists and is the old one
    IF EXISTS (
        SELECT 1 
        FROM information_schema.table_constraints tc
        JOIN information_schema.key_column_usage kcu 
          ON tc.constraint_name = kcu.constraint_name 
          AND tc.table_schema = kcu.table_schema
        WHERE tc.table_name = 'recipient_sessions' 
          AND tc.constraint_type = 'PRIMARY KEY'
          AND kcu.column_name = 'recipient_phone'
    ) AND NOT EXISTS (
        SELECT 1 
        FROM information_schema.table_constraints tc
        JOIN information_schema.key_column_usage kcu 
          ON tc.constraint_name = kcu.constraint_name 
          AND tc.table_schema = kcu.table_schema
        WHERE tc.table_name = 'recipient_sessions' 
          AND tc.constraint_type = 'PRIMARY KEY'
          AND kcu.column_name = 'recipient_identity'
    ) THEN
        ALTER TABLE recipient_sessions DROP CONSTRAINT IF EXISTS recipient_sessions_pkey;
        ALTER TABLE recipient_sessions ADD PRIMARY KEY (workspace_id, recipient_phone, channel, recipient_identity);
    ELSIF NOT EXISTS (
        SELECT 1 
        FROM information_schema.table_constraints 
        WHERE table_name = 'recipient_sessions' 
          AND constraint_type = 'PRIMARY KEY'
    ) THEN
        ALTER TABLE recipient_sessions ADD PRIMARY KEY (workspace_id, recipient_phone, channel, recipient_identity);
    END IF;

    -- 3. Fix the legacy audit_logs_y2026m07 partition if it is not actually a partition
    IF EXISTS (
        SELECT 1 
        FROM pg_class 
        WHERE relname = 'audit_logs_y2026m07' AND relispartition = false
    ) THEN
        DROP TABLE IF EXISTS audit_logs_y2026m07 CASCADE;
        PERFORM create_monthly_partition('2026-07-01'::date);
    END IF;
END $$;
-- +goose StatementEnd

CREATE INDEX IF NOT EXISTS idx_audit_logs_inbound_grouping
  ON audit_logs (workspace_id, event_type, (payload->>'from'), (payload->>'channel'), (payload->>'to'), created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_audit_logs_inbound_grouping;
ALTER TABLE recipient_sessions DROP CONSTRAINT IF EXISTS recipient_sessions_pkey;
ALTER TABLE recipient_sessions ADD PRIMARY KEY (workspace_id, recipient_phone, channel);
ALTER TABLE recipient_sessions DROP COLUMN IF EXISTS recipient_identity;
