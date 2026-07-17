-- +goose Up
-- Create the integrations table
CREATE TABLE integrations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    provider TEXT NOT NULL, -- e.g., 'chatwoot', 'typebot'
    active BOOLEAN NOT NULL DEFAULT TRUE,
    config BYTEA NOT NULL,
    key_id TEXT NOT NULL,
    key_version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, provider)
);

CREATE INDEX idx_integrations_workspace ON integrations(workspace_id);

-- Create the chatwoot_mappings table
CREATE TABLE chatwoot_mappings (
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    contact_id UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    connection_id UUID NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    chatwoot_contact_id BIGINT NOT NULL,
    chatwoot_conversation_id BIGINT NOT NULL,
    channel TEXT NOT NULL,
    sender_identity TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, contact_id, connection_id)
);

CREATE INDEX idx_chatwoot_mappings_contact ON chatwoot_mappings(contact_id);
CREATE INDEX idx_chatwoot_mappings_conv ON chatwoot_mappings(chatwoot_conversation_id);

-- +goose Down
DROP INDEX IF EXISTS idx_chatwoot_mappings_conv;
DROP INDEX IF EXISTS idx_chatwoot_mappings_contact;
DROP TABLE IF EXISTS chatwoot_mappings;
DROP INDEX IF EXISTS idx_integrations_workspace;
DROP TABLE IF EXISTS integrations;
