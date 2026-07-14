-- +goose Up
CREATE TABLE campaigns (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('draft', 'scheduled', 'sending', 'completed', 'cancelled')),
    batch_size INT NOT NULL DEFAULT 100,
    delay_seconds INT NOT NULL DEFAULT 5,
    template_name TEXT,
    channel TEXT,
    recipients JSONB NOT NULL DEFAULT '[]',
    skipped_rows JSONB NOT NULL DEFAULT '[]',
    scheduled_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_campaigns_workspace_status ON campaigns(workspace_id, status);
CREATE INDEX idx_campaigns_scheduled_at ON campaigns(scheduled_at) WHERE status = 'scheduled';

ALTER TABLE message_dispatches
    ADD COLUMN campaign_id UUID REFERENCES campaigns(id) ON DELETE SET NULL,
    ADD COLUMN template_name VARCHAR(100),
    ADD COLUMN variables_json JSONB;

CREATE INDEX idx_message_dispatches_campaign ON message_dispatches(workspace_id, campaign_id) WHERE campaign_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_message_dispatches_campaign;

ALTER TABLE message_dispatches
    DROP COLUMN IF EXISTS campaign_id,
    DROP COLUMN IF EXISTS template_name,
    DROP COLUMN IF EXISTS variables_json;

DROP INDEX IF EXISTS idx_campaigns_scheduled_at;
DROP INDEX IF EXISTS idx_campaigns_workspace_status;
DROP TABLE IF EXISTS campaigns;
