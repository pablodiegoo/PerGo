-- +goose Up
ALTER TABLE contacts ADD COLUMN bot_active BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE contacts ADD COLUMN bot_paused_at TIMESTAMP WITH TIME ZONE;

-- +goose Down
ALTER TABLE contacts DROP COLUMN bot_paused_at;
ALTER TABLE contacts DROP COLUMN bot_active;
