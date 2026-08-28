-- +goose Up
-- +goose StatementBegin
ALTER TABLE recipient_sessions 
    ADD COLUMN IF NOT EXISTS last_outbound_at TIMESTAMPTZ;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE recipient_sessions 
    DROP COLUMN IF EXISTS last_outbound_at;
-- +goose StatementEnd
