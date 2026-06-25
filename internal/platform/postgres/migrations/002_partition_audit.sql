-- +goose Up
-- Create partition function for monthly audit_logs partitions
CREATE OR REPLACE FUNCTION create_monthly_partition(target_date date) RETURNS void AS $$
DECLARE
    partition_name text;
    start_date date;
    end_date date;
BEGIN
    partition_name := 'audit_logs_y' || to_char(target_date, 'YYYY') || 'm' || to_char(target_date, 'MM');
    start_date := date_trunc('month', target_date);
    end_date := start_date + interval '1 month';

    EXECUTE format(
        'CREATE TABLE IF NOT EXISTS %I (LIKE audit_logs INCLUDING ALL)',
        partition_name
    );

    -- Add partition constraint
    EXECUTE format(
        'ALTER TABLE %I ADD CONSTRAINT %s CHECK (created_at >= %L AND created_at < %L)',
        partition_name,
        partition_name || '_check',
        start_date,
        end_date
    );
END;
$$ LANGUAGE plpgsql;

-- Create initial partition for current month
SELECT create_monthly_partition(CURRENT_DATE);

-- Add BRIN index on created_at for append-only optimization
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at_brin ON audit_logs USING brin (created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_audit_logs_created_at_brin;
DROP FUNCTION IF EXISTS create_monthly_partition(date);
