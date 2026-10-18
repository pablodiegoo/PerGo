-- +goose Up
-- +goose StatementBegin
ALTER TABLE chats ADD COLUMN IF NOT EXISTS ai_mode VARCHAR(20) NOT NULL DEFAULT 'active';
ALTER TABLE chats ADD COLUMN IF NOT EXISTS ai_paused_until TIMESTAMPTZ;

ALTER TABLE chats DROP COLUMN IF EXISTS ai_disabled;
ALTER TABLE contacts DROP COLUMN IF EXISTS bot_paused_at;
ALTER TABLE contacts DROP COLUMN IF EXISTS bot_active;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE contacts ADD COLUMN IF NOT EXISTS bot_active BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE contacts ADD COLUMN IF NOT EXISTS bot_paused_at TIMESTAMPTZ;
ALTER TABLE chats ADD COLUMN IF NOT EXISTS ai_disabled BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE chats DROP COLUMN IF EXISTS ai_paused_until;
ALTER TABLE chats DROP COLUMN IF EXISTS ai_mode;
-- +goose StatementEnd
