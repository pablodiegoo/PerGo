# User & API Action Logs

## Requirements

- **Polymorphic Actor Tracking**: Must record both administrative operators (users) and API keys under a single unified database representation.
- **Log Source Segregation**: Must distinguish between actions requested via the public HTTP REST API and operations initiated directly from the administrator console/dashboard.
- **Action-Specific Payloads**: Must persist detailed metadata using a JSONB column to support diverse context structures (e.g. campaign size, message recipient channel, target ID) without schema drift.
- **Access Context**: Must capture timestamps, origin IP addresses, and HTTP User-Agents for compliance and audit logging.

## How to Build It

### 1. Database Schema
Apply a database migration to create the action audit logs table with a composite index for workspace-level query performance:

```sql
CREATE TABLE user_action_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    actor_type VARCHAR(50) NOT NULL, -- 'user', 'api_key', 'system'
    actor_id VARCHAR(255) NOT NULL,   -- email/username or api_key.id
    actor_name VARCHAR(255) NOT NULL, -- cached name or email
    action VARCHAR(100) NOT NULL,    -- 'campaign.create', 'message.send', etc.
    source VARCHAR(50) NOT NULL,      -- 'dashboard' or 'api'
    ip_address VARCHAR(45),
    user_agent TEXT,
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_user_action_logs_workspace_date 
ON user_action_logs(workspace_id, created_at DESC);
```

### 2. Echo Middleware for API Logs
Write a middleware registered globally or on the `/api/v1/*` routes to log API actions:

```go
func ActionAuditMiddleware(db *pgxpool.Pool) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			path := c.Request().URL.Path
			// Skip bypass routes (root, healthz, static)
			if path == "/" || path == "/healthz" || strings.HasPrefix(c.Path(), "/static") {
				return next(c)
			}

			// Capture request metadata
			ip := c.RealIP()
			ua := c.Request().UserAgent()

			// Let the request execute
			err := next(c)

			// Extract authenticated key / workspace details from context
			// and insert user_action_log asynchronously to not block the request
			go logAPIAction(db, c, ip, ua)

			return err
		}
	}
}
```

### 3. UI Implementation
Render logs under a new "Users" tab inside settings using DaisyUI and Tailwind CSS:
- Add a logs tab option under the active sidebar configurations.
- Display a table showing the `created_at` timestamp, actor name/email, action string, source badge, IP, and a details button.
- Toggle an interactive modal to view raw JSON metadata using a DaisyUI `<dialog>` component.

## What to Avoid

- **N+1 Database Queries**: Do not perform joins on `users` or `api_keys` tables dynamically when rendering high-volume action logs. Always cache the readable `actor_name` (like user email or key name) directly in the `user_action_logs` row upon insertion.
- **Blocking Middleware**: Avoid writing logs synchronously inside the request-response lifecycle. Always offload logging inserts to background worker goroutines or queue channels (e.g. NATS) so API response latency remains sub-50ms.
- **Global Middleware Interception**: Ensure that bypass paths (like the root `/` page or health endpoints `/healthz`) are correctly skipped in the auth/logging middlewares to prevent unexpected 401/405 errors.

## Constraints

- **IP Version Support**: The `ip_address` field is set to `VARCHAR(45)` to natively support both IPv4 and IPv6 string formats.
- **Text-based Actor IDs**: Because there is no central `users` table yet (auth uses a shared admin password), `actor_id` must remain `VARCHAR(255)` to support both user emails and API Key UUID strings.

## Origin

Synthesized from spikes: 021
Source files available in: sources/021-user-action-logs/
