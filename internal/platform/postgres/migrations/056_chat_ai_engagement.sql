-- +goose Up
-- +goose StatementBegin
ALTER TABLE chats ADD COLUMN IF NOT EXISTS ai_mode VARCHAR(20) NOT NULL DEFAULT 'active';
ALTER TABLE chats ADD COLUMN IF NOT EXISTS ai_paused_until TIMESTAMPTZ;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'chats' AND column_name = 'ai_disabled'
    ) THEN
        EXECUTE 'UPDATE chats SET ai_mode = ''off'' WHERE ai_disabled = TRUE';
    END IF;
END $$;

ALTER TABLE chats DROP COLUMN IF EXISTS ai_disabled;
ALTER TABLE contacts DROP COLUMN IF EXISTS bot_paused_at;
ALTER TABLE contacts DROP COLUMN IF EXISTS bot_active;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE contacts ADD COLUMN IF NOT EXISTS bot_active BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE contacts ADD COLUMN IF NOT EXISTS bot_paused_at TIMESTAMPTZ;
ALTER TABLE chats ADD COLUMN IF NOT EXISTS ai_disabled BOOLEAN NOT NULL DEFAULT FALSE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'chats' AND column_name = 'ai_mode'
    ) THEN
        EXECUTE 'UPDATE chats SET ai_disabled = TRUE WHERE ai_mode <> ''active''';
    END IF;
END $$;

ALTER TABLE chats DROP COLUMN IF EXISTS ai_paused_until;
ALTER TABLE chats DROP COLUMN IF EXISTS ai_mode;
-- +goose StatementEnd
