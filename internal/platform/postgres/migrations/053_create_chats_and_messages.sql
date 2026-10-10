-- +goose Up
CREATE TABLE IF NOT EXISTS chats (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    connection_id UUID REFERENCES connections(id) ON DELETE SET NULL,
    contact_id UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    status VARCHAR(50) NOT NULL DEFAULT 'open', -- 'open', 'closed'
    assigned_user_id UUID,
    assigned_email VARCHAR(255),
    tags TEXT[] NOT NULL DEFAULT '{}',
    unread_count INT NOT NULL DEFAULT 0,
    ai_disabled BOOLEAN NOT NULL DEFAULT FALSE,
    service_window_expires_at TIMESTAMPTZ,
    last_message_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE NULLS NOT DISTINCT (workspace_id, connection_id, contact_id)
);

CREATE INDEX IF NOT EXISTS idx_chats_ws_status_last_msg ON chats(workspace_id, status, last_message_at DESC);
CREATE INDEX IF NOT EXISTS idx_chats_contact ON chats(contact_id);

CREATE TABLE IF NOT EXISTS chat_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_id UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    uid VARCHAR(128) NOT NULL UNIQUE,
    direction VARCHAR(30) NOT NULL, -- 'inbound', 'outbound', 'internal_note'
    sender_type VARCHAR(50) NOT NULL DEFAULT 'contact', -- 'contact', 'human_agent', 'ai_agent', 'system'
    sender_name VARCHAR(255),
    sender_id VARCHAR(255),
    body TEXT NOT NULL DEFAULT '',
    media_url TEXT,
    media_type VARCHAR(50),
    is_private BOOLEAN NOT NULL DEFAULT FALSE,
    reactions JSONB NOT NULL DEFAULT '[]',
    reply_to_uid VARCHAR(128),
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_chat_messages_chat_created ON chat_messages(chat_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_chat_messages_ws_uid ON chat_messages(workspace_id, uid);

-- +goose Down
DROP TABLE IF EXISTS chat_messages;
DROP TABLE IF EXISTS chats;
