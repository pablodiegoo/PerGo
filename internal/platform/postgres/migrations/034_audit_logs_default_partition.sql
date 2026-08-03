-- +goose Up
CREATE TABLE IF NOT EXISTS audit_logs_default PARTITION OF audit_logs DEFAULT;

-- +goose Down
DROP TABLE IF EXISTS audit_logs_default;
