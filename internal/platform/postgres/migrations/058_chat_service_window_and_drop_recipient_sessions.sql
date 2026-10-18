-- +goose Up
-- +goose StatementBegin
ALTER TABLE chats ADD COLUMN IF NOT EXISTS entry_point_type VARCHAR(20) NOT NULL DEFAULT 'standard';
ALTER TABLE chats ADD COLUMN IF NOT EXISTS last_outbound_at TIMESTAMPTZ;
ALTER TABLE chats ADD COLUMN IF NOT EXISTS notified_expiring_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_chats_expiring_window
    ON chats(service_window_expires_at)
    WHERE notified_expiring_at IS NULL AND service_window_expires_at IS NOT NULL;

DROP TABLE IF EXISTS recipient_sessions;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_chats_expiring_window;
ALTER TABLE chats DROP COLUMN IF EXISTS notified_expiring_at;
ALTER TABLE chats DROP COLUMN IF EXISTS last_outbound_at;
ALTER TABLE chats DROP COLUMN IF EXISTS entry_point_type;

CREATE TABLE IF NOT EXISTS recipient_sessions (
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    recipient_phone TEXT NOT NULL,
    channel TEXT NOT NULL,
    recipient_identity TEXT NOT NULL DEFAULT '',
    last_inbound_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_read_at TIMESTAMPTZ,
    entry_point_type VARCHAR(20) NOT NULL DEFAULT 'standard',
    notified_expiring_at TIMESTAMPTZ,
    last_outbound_at TIMESTAMPTZ,
    PRIMARY KEY (workspace_id, recipient_phone, channel, recipient_identity)
);
-- +goose StatementEnd
