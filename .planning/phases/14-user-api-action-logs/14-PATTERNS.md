# Pattern Map: Phase 14 - User & API Action Logs

This document maps the implementation files and code snippets demonstrating the code patterns to follow.

## Files to Create/Modify

| File Path | Role | Data Flow |
|-----------|------|-----------|
| `internal/platform/postgres/migrations/021_create_user_action_logs.sql` | Migration | Creates the `user_action_logs` table and index. |
| `internal/repository/user_action_log.go` | Repository | Direct database queries and bulk inserts. |
| `internal/api/middleware/audit.go` | Middleware | Intercepts HTTP API requests and enqueues logging actions. |
| `templates/pages/user_logs.templ` | View | Renders the logs table and details modal. |
| `internal/api/handler/admin/user_logs.go` | Handler | Serves logs template and handles pagination. |

## Expected Pattern: Migration

```sql
-- +goose Up
CREATE TABLE user_action_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    actor_type VARCHAR(50) NOT NULL,
    actor_id VARCHAR(255) NOT NULL,
    actor_name VARCHAR(255) NOT NULL,
    action VARCHAR(100) NOT NULL,
    source VARCHAR(50) NOT NULL,
    ip_address VARCHAR(45),
    user_agent TEXT,
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_user_action_logs_workspace_date 
ON user_action_logs(workspace_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS user_action_logs;
```

## Expected Pattern: Repository

```go
package repository

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UserActionLog struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	ActorType   string
	ActorID     string
	ActorName   string
	Action      string
	Source      string
	IPAddress   string
	UserAgent   string
	Metadata    []byte
	CreatedAt   time.Time
}

type UserActionLogRepository struct {
	pool *pgxpool.Pool
}

func (r *UserActionLogRepository) Insert(ctx context.Context, log *UserActionLog) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO user_action_logs 
		(workspace_id, actor_type, actor_id, actor_name, action, source, ip_address, user_agent, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, log.WorkspaceID, log.ActorType, log.ActorID, log.ActorName, log.Action, log.Source, log.IPAddress, log.UserAgent, log.Metadata)
	return err
}
```
