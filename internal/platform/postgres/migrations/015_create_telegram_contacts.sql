-- +goose Up
-- +goose StatementBegin
CREATE TABLE telegram_contacts (
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    chat_id TEXT NOT NULL,
    username TEXT,
    phone_number TEXT,
    first_name TEXT,
    last_name TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, chat_id)
);

CREATE UNIQUE INDEX idx_telegram_contacts_username ON telegram_contacts(workspace_id, username) WHERE username IS NOT NULL;
CREATE UNIQUE INDEX idx_telegram_contacts_phone ON telegram_contacts(workspace_id, phone_number) WHERE phone_number IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS telegram_contacts;
-- +goose StatementEnd
