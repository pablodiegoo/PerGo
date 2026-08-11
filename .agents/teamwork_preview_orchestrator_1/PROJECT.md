# Project: PerGo Open Issues (#39, #41, #42, #43, #44, #45)

## Architecture
PerGo is an open-source Omnichannel CPaaS built in Go (Echo v5, NATS JetStream, PostgreSQL via pgx/v5, templ/HTMX admin UI).
- `cmd/pergo`: Main application entry point.
- `internal/platform/echo`: HTTP Echo server setup and platform middleware.
- `internal/api/handler`: REST API and HTMX admin handlers.
- `internal/repository`: Data access repositories (idempotency, workspace, tag, campaign, audit).
- `internal/webhook`: Outbound webhook dispatcher & signature generation (`X-PerGo-Signature`).
- `internal/platform/queue`: NATS worker dispatches (campaign worker, dispatch orchestrator).
- `internal/platform/audit`: Structured audit log writer.

## Feature Inventory
Every feature from the Survey phase appears here with its assigned milestone.
| # | Feature | Description | Milestone | Source |
|---|---------|-------------|-----------|--------|
| 1 | Move SecurityHeaders Middleware | Move `SecurityHeaders` to `internal/platform/echo/` to eliminate `internal/api/` imports in `echo.go` | M1 | survey |
| 2 | Refactor Telegram Error Wrapping | Update `telegram.go:119` error wrapping to use `%w` for underlying errors | M1 | survey |
| 3 | Refactor Fat CSV Handlers | Delegate CSV export logic in `audit.go`, `tag.go`, `campaign.go` to helper functions | M1 | survey |
| 4 | Isolate Idempotency Checks | Extract SHA256 hashing, lookup, and ledger recording out of `SendMessage` in `message.go` | M1 | survey |
| 5 | Relocate Inline /tags Closure | Move inline closure in `main.go:663-680` to `TagAdminHandler` method | M1 | survey |
| 6 | Fix Idempotency SQL Placeholders | Restore `$1, $2, ...` positional placeholders across 5 queries in `internal/repository/idempotency.go` | M2 | survey |
| 7 | Outbound Webhook HMAC-SHA256 | Verify `X-PerGo-Signature` HMAC-SHA256 calculation and workspace secret fallback | M3 | survey |
| 8 | Campaign Tag Filtering API | Update `POST /api/v1/campaigns` to accept `tag_ids` (`[]uuid.UUID`) and filter contacts by tags | M4 | survey |
| 9 | Campaign Admin UI Tag Selector | Update `NewForm` in `CampaignHandler` and `CampaignCreateForm` in `templates/pages/campaigns.templ` with `<select name="tag_id">` | M4 | survey |
| 10 | Campaign Worker Audit Log Emissions | Inject `auditWriter` in `CampaignWorker` and emit audit events on state changes (`sent`, `delivered`, `failed`) | M5 | survey |

## Milestones
| # | Name | Scope | Dependencies | Status |
|---|------|-------|-------------|--------|
| 1 | Refactoring & Import Cycle Fixes (#39, #42) | Features 1-5 | none | DONE |
| 2 | Idempotency SQL Fixes (#41) | Feature 6 | none | PLANNED |
| 3 | Outbound Webhook Signature Verification (#43) | Feature 7 | none | PLANNED |
| 4 | Campaign Tag Filtering & Admin UI Selector (#44) | Features 8-9 | M1 | PLANNED |
| 5 | Campaign Worker Audit Log Emissions (#45) | Feature 10 | M4 | PLANNED |

## Interface Contracts
### `internal/platform/echo` ↔ `internal/api/middleware`
- `SecurityHeaders()` relocated to `internal/platform/echo/security.go` under `package echosrv`. Zero imports of `internal/api/middleware` inside `echo.go`.

### REST API `POST /api/v1/campaigns`
- Payload: accepts `tag_ids` (`[]uuid.UUID`) and `tag_id` (`*uuid.UUID`). When provided, resolves contacts via `TagRepo.ListContactsByTag(s)`.

### Campaign Worker ↔ Audit Subsystem
- `CampaignWorker` takes `auditWriter audit.Writer` in `NewCampaignWorker`. Calls `auditWriter.Write(audit.NewEvent(workspaceID, traceID, eventType, payloadBytes))` for state changes `sent`, `delivered`, `failed`.

## Code Layout
- `internal/platform/echo/`: `echo.go`, `security.go`, `security_test.go`
- `internal/channel/telegram/`: `telegram.go`
- `internal/api/handler/`: `message.go`
- `internal/api/handler/admin/`: `audit.go`, `tag.go`, `campaign.go`
- `cmd/pergo/`: `main.go`
- `internal/repository/`: `idempotency.go`
- `internal/webhook/`: `dispatcher.go`, `dispatcher_test.go`
- `internal/platform/queue/`: `campaign_worker.go`, `campaign_worker_test.go`
- `templates/pages/`: `campaigns.templ`
