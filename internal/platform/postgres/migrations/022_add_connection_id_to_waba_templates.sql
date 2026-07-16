-- +goose Up
ALTER TABLE waba_templates
    ADD COLUMN connection_id UUID REFERENCES connections(id) ON DELETE CASCADE;

-- Since the count is 0, we can safely set it to NOT NULL directly.
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
