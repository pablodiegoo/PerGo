-- +goose Up
-- +goose StatementBegin
ALTER TABLE contacts ADD COLUMN tags TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE contacts ADD COLUMN closed_at TIMESTAMPTZ;
CREATE INDEX idx_contacts_tags ON contacts USING gin(tags);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_contacts_tags;
ALTER TABLE contacts DROP COLUMN IF EXISTS tags;
ALTER TABLE contacts DROP COLUMN IF EXISTS closed_at;
-- +goose StatementEnd
