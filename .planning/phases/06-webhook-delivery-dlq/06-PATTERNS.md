# Phase 6: Webhook Delivery & DLQ - Pattern Mapping

## File Classification

| File Path | Role | Data Flow | Closest Analog |
|-----------|------|-----------|----------------|
| `internal/repository/webhook_dlq.go` | Repository | Database persistence / SQL query Execution | `internal/repository/dispatch.go` |
| `internal/platform/queue/webhook_worker.go` | Background Worker | JetStream event consumption / outbound HTTP dispatch | `internal/platform/queue/worker.go` |
| `internal/api/handler/admin/webhook_dlq.go` | HTTP Handler | Workspace credentials/DLQ management REST API | `internal/api/handler/admin/devices.go` |
| `templates/pages/webhooks.templ` | UI Layout | Server-rendered Templ/HTMX fragment structure | `templates/pages/devices.templ` |

## Code Analogs and Patterns

### 1. Database Repository (`internal/repository/webhook_dlq.go`)
- **Analog:** `internal/repository/dispatch.go`
- **Pattern:** Using raw SQL statements via `pgxpool.Pool` for transactional execution, scanning rows into structs, and handling `pgx.ErrNoRows` with domain-level errors.

### 2. Background Worker (`internal/platform/queue/webhook_worker.go`)
- **Analog:** `internal/platform/queue/worker.go`
- **Pattern:** Consuming from a NATS JetStream pull consumer using `consumer.Messages()`, executing `msgCtx.Next()`, and calling `msg.Ack()` on success or `msg.NakWithDelay()` on retryable errors.

### 3. Admin Controller (`internal/api/handler/admin/webhook_dlq.go`)
- **Analog:** `internal/api/handler/admin/apikeys.go`
- **Pattern:** Echo v5 HTTP handlers fetching credentials/data scoped by `workspace_id` parsed from url parameters, returning HTMX-rendered Templ fragments.

### 4. Admin UI Page (`templates/pages/webhooks.templ`)
- **Analog:** `templates/pages/devices.templ`
- **Pattern:** Compile-time type-safe HTML template layout using Templ component structures, handling empty states, table pagination, and HTMX actions (`hx-post`, `hx-delete`, `hx-target`).
