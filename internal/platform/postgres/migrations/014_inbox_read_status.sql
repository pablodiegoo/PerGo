-- +goose Up
ALTER TABLE recipient_sessions ADD COLUMN last_read_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE recipient_sessions DROP COLUMN last_read_at;
