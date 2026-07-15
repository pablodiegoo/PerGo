# Technical Research: Phase 14 - User & API Action Logs

This research documents the schema, routing, and UI design verified in Spike 021 for implementing administrative activity logging in PerGo.

## 1. Verified Schema Design

We will introduce a new table `user_action_logs` to record polymorphic agent actions across both the HTTP API and the administrator console:

```sql
CREATE TABLE user_action_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    actor_type VARCHAR(50) NOT NULL, -- 'user', 'api_key', 'system'
    actor_id VARCHAR(255) NOT NULL,   -- email/username or api_key.id
    actor_name VARCHAR(255) NOT NULL, -- cached name/email
    action VARCHAR(100) NOT NULL,    -- e.g. 'campaign.create', 'message.send'
    source VARCHAR(50) NOT NULL,      -- 'dashboard' or 'api'
    ip_address VARCHAR(45),
    user_agent TEXT,
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_user_action_logs_workspace_date 
ON user_action_logs(workspace_id, created_at DESC);
```

## 2. Capture Pipeline

### Public API Logs (Middleware)
We will implement an Echo middleware that intercept incoming requests on `/api/v1/*`. The middleware will:
- Extract authenticated key details from the request context (injected by `AuthMiddleware`).
- Capture client IP (`c.RealIP()`) and User-Agent.
- Perform asynchronous logging insertion by offloading to a worker or executing inside a Go goroutine to preserve low response latency.
- Correctly bypass internal/uncertified routes (like `/healthz`, `/readyz`, static files, and the root `/` path).

### Dashboard Logs (Handlers)
We will update administrative post handlers (e.g. campaign creation, connection updates, webhook additions) to explicitly invoke the log service, passing `'user'` as the actor type and their session email as the actor identifier.

## 3. UI logs View
We will create a new sub-page `/admin/workspaces/:workspace_id/settings/user-logs` (or similar) in the admin console under configurations:
- Serves a paginated table of user action logs.
- Uses Tailwind CSS and DaisyUI styling.
- Features a DaisyUI dialog modal to render raw JSONB metadata formatted nicely.
