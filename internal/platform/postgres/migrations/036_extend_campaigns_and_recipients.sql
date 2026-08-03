-- +goose Up
ALTER TABLE campaigns
    ADD COLUMN IF NOT EXISTS connection_slug VARCHAR(100),
    ADD COLUMN IF NOT EXISTS message_body TEXT,
    ADD COLUMN IF NOT EXISTS tag_id UUID REFERENCES tags(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS total_recipients INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS sent_recipients INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS failed_recipients INT NOT NULL DEFAULT 0;

ALTER TABLE campaigns DROP CONSTRAINT IF EXISTS campaigns_status_check;
ALTER TABLE campaigns ADD CONSTRAINT campaigns_status_check CHECK (status IN ('draft', 'scheduled', 'sending', 'running', 'paused', 'completed', 'failed', 'cancelled'));

CREATE TABLE IF NOT EXISTS campaign_recipients (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    contact_id UUID REFERENCES contacts(id) ON DELETE CASCADE,
    phone VARCHAR(50) NOT NULL,
    variables JSONB NOT NULL DEFAULT '{}',
    status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'sent', 'failed', 'skipped')),
    error_message TEXT,
    sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_campaign_recipients_campaign_status ON campaign_recipients(campaign_id, status);

-- +goose Down
DROP INDEX IF EXISTS idx_campaign_recipients_campaign_status;
DROP TABLE IF EXISTS campaign_recipients;

ALTER TABLE campaigns
    DROP COLUMN IF EXISTS connection_slug,
    DROP COLUMN IF EXISTS message_body,
    DROP COLUMN IF EXISTS tag_id,
    DROP COLUMN IF EXISTS total_recipients,
    DROP COLUMN IF EXISTS sent_recipients,
    DROP COLUMN IF EXISTS failed_recipients;
