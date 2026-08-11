-- +goose Up
ALTER TABLE webhook_dlqs ADD COLUMN IF NOT EXISTS encrypted_payload BYTEA;
ALTER TABLE webhook_dlqs ADD COLUMN IF NOT EXISTS encryption_nonce BYTEA;

-- +goose Down
ALTER TABLE webhook_dlqs DROP COLUMN IF EXISTS encrypted_payload;
ALTER TABLE webhook_dlqs DROP COLUMN IF EXISTS encryption_nonce;
