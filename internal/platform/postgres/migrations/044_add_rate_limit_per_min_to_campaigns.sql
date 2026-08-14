-- +goose Up
ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS rate_limit_per_min INT DEFAULT NULL;

-- +goose Down
ALTER TABLE campaigns DROP COLUMN IF EXISTS rate_limit_per_min;
