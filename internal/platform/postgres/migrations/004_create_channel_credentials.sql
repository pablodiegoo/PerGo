-- +goose Up
CREATE TABLE channel_credentials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel TEXT NOT NULL,
    credentials BYTEA NOT NULL,
    key_id TEXT NOT NULL,
    key_version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, channel)
);

CREATE INDEX idx_channel_credentials_workspace ON channel_credentials(workspace_id);

-- +goose Down
DROP TABLE IF EXISTS channel_credentials;
