-- +goose Up
ALTER TABLE message_dispatches ADD COLUMN provider_message_id VARCHAR(255) UNIQUE;
CREATE INDEX IF NOT EXISTS idx_message_dispatches_provider_message_id ON message_dispatches(provider_message_id) WHERE provider_message_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_message_dispatches_provider_message_id;
ALTER TABLE message_dispatches DROP COLUMN IF EXISTS provider_message_id;
