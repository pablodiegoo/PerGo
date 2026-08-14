-- +goose Up
-- +goose StatementBegin
ALTER TABLE contacts ADD COLUMN IF NOT EXISTS attributes JSONB NOT NULL DEFAULT '{}'::jsonb;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE IF EXISTS contacts DROP COLUMN IF EXISTS attributes;
-- +goose StatementEnd
