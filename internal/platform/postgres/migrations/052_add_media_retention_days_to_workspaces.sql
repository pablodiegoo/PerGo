-- +goose Up
-- +goose StatementBegin
ALTER TABLE workspaces 
    ADD COLUMN IF NOT EXISTS media_retention_days INT NOT NULL DEFAULT 30;

CREATE INDEX IF NOT EXISTS idx_audit_logs_ws_created ON audit_logs(workspace_id, created_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_audit_logs_ws_created;
ALTER TABLE workspaces 
    DROP COLUMN IF EXISTS media_retention_days;
-- +goose StatementEnd
