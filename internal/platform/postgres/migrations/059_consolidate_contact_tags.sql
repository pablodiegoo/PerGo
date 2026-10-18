-- +goose Up
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_contacts_tags;
ALTER TABLE contacts DROP COLUMN IF EXISTS tags;
ALTER TABLE chats DROP COLUMN IF EXISTS tags;

ALTER TABLE tags ALTER COLUMN name TYPE VARCHAR(64);
ALTER TABLE tags DROP CONSTRAINT IF EXISTS tags_workspace_name_unique;
CREATE UNIQUE INDEX IF NOT EXISTS idx_tags_workspace_lower_name ON tags (workspace_id, LOWER(name));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_tags_workspace_lower_name;
ALTER TABLE tags ADD CONSTRAINT tags_workspace_name_unique UNIQUE (workspace_id, name);
ALTER TABLE tags ALTER COLUMN name TYPE VARCHAR(50);

ALTER TABLE chats ADD COLUMN IF NOT EXISTS tags TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE contacts ADD COLUMN IF NOT EXISTS tags TEXT[] NOT NULL DEFAULT '{}';
CREATE INDEX IF NOT EXISTS idx_contacts_tags ON contacts USING gin(tags);
-- +goose StatementEnd
