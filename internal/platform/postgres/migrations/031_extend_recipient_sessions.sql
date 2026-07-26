-- +goose Up
-- +goose StatementBegin
ALTER TABLE recipient_sessions 
    ADD COLUMN IF NOT EXISTS entry_point_type VARCHAR(20) NOT NULL DEFAULT 'standard',
    ADD COLUMN IF NOT EXISTS notified_expiring_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_recipient_sessions_expiring 
    ON recipient_sessions(last_inbound_at) 
    WHERE notified_expiring_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_recipient_sessions_expiring;
ALTER TABLE recipient_sessions 
    DROP COLUMN IF EXISTS notified_expiring_at,
    DROP COLUMN IF EXISTS entry_point_type;
-- +goose StatementEnd
