-- +goose Up
CREATE TABLE message_dispatches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    trace_id TEXT UNIQUE NOT NULL,
    current_channel TEXT NOT NULL,
    status TEXT NOT NULL,
    fallback_index INT NOT NULL DEFAULT 0,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_message_dispatches_trace ON message_dispatches(trace_id);
CREATE INDEX idx_message_dispatches_workspace ON message_dispatches(workspace_id);

-- +goose Down
DROP TABLE IF EXISTS message_dispatches;
