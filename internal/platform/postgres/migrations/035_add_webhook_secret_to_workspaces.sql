-- +goose Up
ALTER TABLE workspaces
    ADD COLUMN IF NOT EXISTS webhook_secret VARCHAR(128);

-- +goose Down
ALTER TABLE workspaces
    DROP COLUMN IF EXISTS webhook_secret;
