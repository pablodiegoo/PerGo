-- +goose Up
-- +goose StatementBegin
ALTER TABLE contacts DROP COLUMN IF EXISTS closed_at;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE contacts ADD COLUMN IF NOT EXISTS closed_at TIMESTAMPTZ;
-- +goose StatementEnd
