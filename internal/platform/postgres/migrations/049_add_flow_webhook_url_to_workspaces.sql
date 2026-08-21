-- +goose Up
ALTER TABLE workspaces
    ADD COLUMN IF NOT EXISTS flow_webhook_url TEXT;

-- +goose Down
ALTER TABLE workspaces
    DROP COLUMN IF EXISTS flow_webhook_url;
