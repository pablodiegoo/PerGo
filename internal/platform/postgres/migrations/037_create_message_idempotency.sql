-- +goose Up
CREATE TABLE IF NOT EXISTS message_idempotency (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    key_hash VARCHAR(64) NOT NULL,
    trace_id VARCHAR(128) NOT NULL,
    status_code INT,
    response_body JSONB,
    provider_message_id VARCHAR(128),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '24 hours'),
    CONSTRAINT message_idempotency_ws_key_unique UNIQUE (workspace_id, key_hash)
);

CREATE INDEX IF NOT EXISTS idx_message_idempotency_workspace_key ON message_idempotency(workspace_id, key_hash);
CREATE INDEX IF NOT EXISTS idx_message_idempotency_expires_at ON message_idempotency(expires_at);

-- +goose Down
DROP TABLE IF EXISTS message_idempotency;
