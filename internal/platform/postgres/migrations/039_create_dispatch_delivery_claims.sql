-- +goose Up
CREATE TABLE IF NOT EXISTS dispatch_delivery_claim (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    trace_id VARCHAR(128) NOT NULL,
    connection_id UUID,
    message_subject VARCHAR(255) NOT NULL,
    claimed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    worker_instance_id VARCHAR(128) NOT NULL,
    CONSTRAINT dispatch_delivery_claim_ws_trace_unique UNIQUE (workspace_id, trace_id)
);

CREATE INDEX IF NOT EXISTS idx_delivery_claim_claimed_at ON dispatch_delivery_claim(claimed_at);
CREATE INDEX IF NOT EXISTS idx_delivery_claim_worker ON dispatch_delivery_claim(worker_instance_id);

-- +goose Down
DROP TABLE IF EXISTS dispatch_delivery_claim;
