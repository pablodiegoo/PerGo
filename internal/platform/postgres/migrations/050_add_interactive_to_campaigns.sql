-- +goose Up
ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS interactive JSONB DEFAULT NULL;
ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS fallback_behavior VARCHAR(32) DEFAULT NULL;

-- +goose Down
ALTER TABLE campaigns DROP COLUMN IF EXISTS interactive;
ALTER TABLE campaigns DROP COLUMN IF EXISTS fallback_behavior;
