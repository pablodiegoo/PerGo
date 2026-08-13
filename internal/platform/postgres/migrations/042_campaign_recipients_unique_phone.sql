-- +goose Up
CREATE UNIQUE INDEX IF NOT EXISTS idx_campaign_recipients_campaign_phone ON campaign_recipients(campaign_id, phone);

-- +goose Down
DROP INDEX IF EXISTS idx_campaign_recipients_campaign_phone;


