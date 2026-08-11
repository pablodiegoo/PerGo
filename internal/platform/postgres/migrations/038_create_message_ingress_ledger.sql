-- +goose Up
CREATE TABLE IF NOT EXISTS message_ingress_ledger (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    trace_id VARCHAR(128) NOT NULL,
    idempotency_key VARCHAR(128) NOT NULL,
    channel VARCHAR(50) NOT NULL,
    recipient VARCHAR(100) NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'accepted' CHECK (status IN ('accepted', 'enqueued', 'delivered', 'failed')),
    error_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ingress_ledger_ws_trace ON message_ingress_ledger(workspace_id, trace_id);
CREATE INDEX IF NOT EXISTS idx_ingress_ledger_ws_status ON message_ingress_ledger(workspace_id, status);

-- +goose Down
DROP TABLE IF EXISTS message_ingress_ledger;
