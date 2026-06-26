-- +goose Up
CREATE TABLE waba_templates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    meta_template_id TEXT NOT NULL,
    name TEXT NOT NULL,
    language TEXT NOT NULL,
    status TEXT NOT NULL,
    category TEXT NOT NULL,
    components JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, name, language)
);

CREATE INDEX idx_waba_templates_workspace ON waba_templates(workspace_id);

-- +goose Down
DROP TABLE IF EXISTS waba_templates;
