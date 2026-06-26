-- +goose Up
-- Add WhatsApp-specific fields to devices table for Phase 4.
-- The existing devices table is generic (channel, device_id, status).
-- WhatsApp Web needs: jid, phone, connected_since.
ALTER TABLE devices ADD COLUMN IF NOT EXISTS jid TEXT;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS phone TEXT;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS connected_since TIMESTAMPTZ;
CREATE UNIQUE INDEX IF NOT EXISTS idx_devices_jid ON devices(jid) WHERE jid IS NOT NULL;

-- +goose Down
ALTER TABLE devices DROP COLUMN IF EXISTS connected_since;
ALTER TABLE devices DROP COLUMN IF EXISTS phone;
ALTER TABLE devices DROP COLUMN IF EXISTS jid;
