-- +goose Up
-- +goose StatementBegin
-- Rename existing non-partitioned audit_logs table
ALTER TABLE audit_logs RENAME TO audit_logs_old;

-- Drop old indices to avoid naming conflicts
DROP INDEX IF EXISTS idx_audit_logs_trace_id;
DROP INDEX IF EXISTS idx_audit_logs_created_at;
DROP INDEX IF EXISTS idx_audit_logs_created_at_brin;

-- Create partitioned parent table
CREATE TABLE audit_logs (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    trace_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

-- Recreate indices on the partitioned parent table
CREATE INDEX idx_audit_logs_trace_id ON audit_logs(trace_id);
CREATE INDEX idx_audit_logs_created_at ON audit_logs(created_at);

-- Drop old non-declarative function and trigger
DROP FUNCTION IF EXISTS create_monthly_partition(date);

-- Create new function with declarative partition attachment
CREATE OR REPLACE FUNCTION create_monthly_partition(target_date date) RETURNS void AS $func$
DECLARE
    partition_name text;
    start_date date;
    end_date date;
BEGIN
    partition_name := 'audit_logs_y' || to_char(target_date, 'YYYY') || 'm' || to_char(target_date, 'MM');
    start_date := date_trunc('month', target_date);
    end_date := start_date + interval '1 month';
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I PARTITION OF audit_logs FOR VALUES FROM (%L) TO (%L)', 
                   partition_name, start_date, end_date);
END;
$func$ LANGUAGE plpgsql;

-- Drop old non-declarative partition if exists
DROP TABLE IF EXISTS audit_logs_y2026m06;

-- Recreate partitions for the current and next month
SELECT create_monthly_partition(CURRENT_DATE);
SELECT create_monthly_partition((CURRENT_DATE + interval '1 month')::date);

-- Dynamically create partitions for any months present in existing data
DO $$
DECLARE
    r RECORD;
BEGIN
    FOR r IN SELECT DISTINCT date_trunc('month', created_at)::date AS m FROM audit_logs_old LOOP
        PERFORM create_monthly_partition(r.m);
    END LOOP;
END;
$$;

-- Recreate BRIN index on parent table
CREATE INDEX idx_audit_logs_created_at_brin ON audit_logs USING brin (created_at);

-- Migrate existing data
INSERT INTO audit_logs (id, workspace_id, trace_id, event_type, payload, created_at)
SELECT id, workspace_id, trace_id, event_type, payload, created_at FROM audit_logs_old;

-- Drop old table
DROP TABLE audit_logs_old;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Rename partitioned table to old
ALTER TABLE audit_logs RENAME TO audit_logs_partitioned;

-- Drop new indices
DROP INDEX IF EXISTS idx_audit_logs_trace_id;
DROP INDEX IF EXISTS idx_audit_logs_created_at;
DROP INDEX IF EXISTS idx_audit_logs_created_at_brin;

-- Recreate original non-partitioned table
CREATE TABLE audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    trace_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_logs_trace_id ON audit_logs(trace_id);
CREATE INDEX idx_audit_logs_created_at ON audit_logs(created_at);
CREATE INDEX idx_audit_logs_created_at_brin ON audit_logs USING brin (created_at);

-- Restore function
DROP FUNCTION IF EXISTS create_monthly_partition(date);
CREATE OR REPLACE FUNCTION create_monthly_partition(target_date date) RETURNS void AS $func$
DECLARE
    partition_name text;
    start_date date;
    end_date date;
BEGIN
    partition_name := 'audit_logs_y' || to_char(target_date, 'YYYY') || 'm' || to_char(target_date, 'MM');
    start_date := date_trunc('month', target_date);
    end_date := start_date + interval '1 month';
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I (LIKE audit_logs INCLUDING ALL)', partition_name);
    EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %s CHECK (created_at >= %L AND created_at < %L)', partition_name, partition_name || '_check', start_date, end_date);
END;
$func$ LANGUAGE plpgsql;

-- Migrate data back
INSERT INTO audit_logs (id, workspace_id, trace_id, event_type, payload, created_at)
SELECT id, workspace_id, trace_id, event_type, payload, created_at FROM audit_logs_partitioned;

-- Drop new tables
DROP TABLE audit_logs_partitioned CASCADE;
-- +goose StatementEnd
