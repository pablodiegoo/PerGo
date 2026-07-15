# Summary: Phase 14 - User & API Action Logs

Implemented administrative action audit logging for PerGo, covering both API Key requests (public REST API) and dashboard operations (admin console).

## Key Deliverables

1. **Database Migration & Schema**:
   - Created `021_create_user_action_logs.sql` establishing the `user_action_logs` table.
   - Added a composite index `(workspace_id, created_at DESC)` optimized for fast sub-page queries.
2. **Repository Layer**:
   - Developed `UserActionLogRepository` inside `internal/repository/user_action_log.go` containing methods `Insert`, `ListByWorkspace` (supporting dynamic database-level filtering by actor type and source), and `GetByID`.
   - Added database integration tests in `user_action_log_test.go` verifying pagination and filtering queries.
3. **Echo Audit Middlewares**:
   - Implemented `AuditMiddleware` inside `internal/api/middleware/audit.go` to capture `/api/v1/*` requests (using authenticated API key context prefix and payload metadata) asynchronously.
   - Implemented `DashboardAuditMiddleware` to capture state-changing dashboard routes (POST, PUT, DELETE), redacting credentials and enqueuing logs.
   - Registered `DashboardAuditMiddleware` on the `/admin` protected routing group and `AuditMiddleware` globally.
   - Added complete mock/context tests in `audit_test.go` verifying the pipeline.
4. **Console UI & Routing (Unified Tabs)**:
   - Created a unified `LogsHeaderTabs` component in `templates/pages/audit.templ` rendering tabs for Outbound, Inbound, and Action logs.
   - Built route handler `internal/api/handler/admin/user_logs.go` mapping `/admin/logs/actions` and `/admin/logs/actions/:id/metadata`.
   - Embedded the Action Logs view inside the existing Logs page layout, avoiding sidebar submenu expansion clutter.

## Verification Details
- Successfully compiled templates via `templ generate ./...`.
- Verified repository queries via `go test -v ./internal/repository/...` (all passed).
- Verified middleware logic via `go test -v ./internal/api/middleware/...` (all passed).
- Verified handler rendering and routing via `go test -v ./internal/api/handler/admin/...` (all passed).
