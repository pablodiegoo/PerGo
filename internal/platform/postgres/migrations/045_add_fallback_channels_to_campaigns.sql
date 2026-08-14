-- +goose Up
ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS fallback_channels TEXT[] DEFAULT '{}';

-- +goose Down
ALTER TABLE campaigns DROP COLUMN IF EXISTS fallback_channels;
