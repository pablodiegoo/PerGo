-- +goose Up
ALTER TABLE chats ADD COLUMN IF NOT EXISTS snoozed_until TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_chats_snoozed_until ON chats(workspace_id, status, snoozed_until) WHERE status = 'snoozed';

-- +goose Down
DROP INDEX IF EXISTS idx_chats_snoozed_until;
ALTER TABLE chats DROP COLUMN IF EXISTS snoozed_until;
