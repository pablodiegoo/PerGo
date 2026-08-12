-- +goose Up
ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS tag_ids UUID[] DEFAULT NULL;

-- +goose Down
ALTER TABLE campaigns DROP COLUMN IF EXISTS tag_ids;
