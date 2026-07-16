-- +goose Up
-- Create the new webhook_subscriptions table
CREATE TABLE webhook_subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    secret BYTEA NOT NULL,
    key_id TEXT NOT NULL,
    key_version INT NOT NULL DEFAULT 1,
    event_types TEXT[] NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_webhook_subscriptions_workspace_id ON webhook_subscriptions(workspace_id);

-- Migrate existing configurations (assign catch-all '*' wildcard to maintain backwards compatibility)
INSERT INTO webhook_subscriptions (workspace_id, url, secret, key_id, key_version, event_types, active, created_at, updated_at)
SELECT workspace_id, url, secret, key_id, key_version, ARRAY['*'], TRUE, created_at, updated_at
FROM webhook_configs;

-- Add subscription_id column to webhook_dlqs
ALTER TABLE webhook_dlqs ADD COLUMN subscription_id UUID;

-- Associate existing DLQ logs with the migrated subscriptions based on workspace mapping
UPDATE webhook_dlqs d
SET subscription_id = s.id
FROM webhook_subscriptions s
WHERE d.workspace_id = s.workspace_id;

-- Purge orphan DLQ records where a configuration was deleted prior to migration
DELETE FROM webhook_dlqs WHERE subscription_id IS NULL;

-- Enforce constraints on subscription_id
ALTER TABLE webhook_dlqs ALTER COLUMN subscription_id SET NOT NULL;
ALTER TABLE webhook_dlqs ADD CONSTRAINT fk_webhook_dlqs_subscription_id 
    FOREIGN KEY (subscription_id) REFERENCES webhook_subscriptions(id) ON DELETE CASCADE;

-- Drop the legacy single-config table
DROP TABLE webhook_configs;

-- +goose Down
-- Recreate the legacy single-config table
CREATE TABLE webhook_configs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    secret BYTEA NOT NULL,
    key_id TEXT NOT NULL,
    key_version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id)
);

CREATE INDEX idx_webhook_configs_workspace_id ON webhook_configs(workspace_id);

-- Restore legacy configs (take the first active or latest subscription per workspace)
INSERT INTO webhook_configs (workspace_id, url, secret, key_id, key_version, created_at, updated_at)
SELECT DISTINCT ON (workspace_id) workspace_id, url, secret, key_id, key_version, created_at, updated_at
FROM webhook_subscriptions
ORDER BY workspace_id, created_at DESC;

-- Remove foreign key constraint and column from DLQ
ALTER TABLE webhook_dlqs DROP CONSTRAINT fk_webhook_dlqs_subscription_id;
ALTER TABLE webhook_dlqs DROP COLUMN subscription_id;

-- Drop the subscriptions table
DROP TABLE webhook_subscriptions;
