-- +goose Up
ALTER TABLE waba_templates
    ADD COLUMN connection_id UUID REFERENCES connections(id) ON DELETE CASCADE;

-- Backfill connection_id for existing templates matching the workspace's whatsapp_cloud connection
UPDATE waba_templates wt
SET connection_id = (
    SELECT c.id 
    FROM connections c 
    WHERE c.workspace_id = wt.workspace_id 
      AND c.channel = 'whatsapp_cloud'
    LIMIT 1
);

-- Fallback: Use the first connection of the workspace if no whatsapp_cloud connection exists
UPDATE waba_templates wt
SET connection_id = (
    SELECT c.id 
    FROM connections c 
    WHERE c.workspace_id = wt.workspace_id 
    LIMIT 1
)
WHERE connection_id IS NULL;

-- Cleanup: Delete orphaned templates belonging to workspaces with zero connections
DELETE FROM waba_templates WHERE connection_id IS NULL;

ALTER TABLE waba_templates
    ALTER COLUMN connection_id SET NOT NULL;

-- Remove old constraint
ALTER TABLE waba_templates
    DROP CONSTRAINT IF EXISTS waba_templates_workspace_id_name_language_key;

-- Add new unique constraint
ALTER TABLE waba_templates
    ADD CONSTRAINT waba_templates_connection_id_name_language_key UNIQUE (connection_id, name, language);

-- Add index
CREATE INDEX idx_waba_templates_connection ON waba_templates(connection_id);

-- Add connection_id to campaigns
ALTER TABLE campaigns
    ADD COLUMN connection_id UUID REFERENCES connections(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE campaigns
    DROP COLUMN IF EXISTS connection_id;

ALTER TABLE waba_templates
    DROP CONSTRAINT IF EXISTS waba_templates_connection_id_name_language_key;

ALTER TABLE waba_templates
    DROP COLUMN IF EXISTS connection_id;

ALTER TABLE waba_templates
    ADD CONSTRAINT waba_templates_workspace_id_name_language_key UNIQUE (workspace_id, name, language);

DROP INDEX IF EXISTS idx_waba_templates_connection;
