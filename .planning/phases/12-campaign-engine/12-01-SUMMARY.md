---
requirements_completed:
  - CAMP-02
  - CAMP-03
  - CAMP-04
  - CAMP-05
  - CAMP-06
  - CAMP-07
  - CAMP-08
---
# Phase 12-01 Summary — Campaign Engine Foundation

## 1. What was built

- **PostgreSQL Database Schema Migration (`016_create_campaigns.sql`)**:
  - Created `campaigns` table to persist bulk mailing configurations, workspace mapping, status (draft, scheduled, sending, completed, cancelled), batch parameters, recipients list, and skipped records.
  - Added indexes: `idx_campaigns_workspace_status` and `idx_campaigns_scheduled_at` (for active scheduled entries).
  - Enriched `message_dispatches` table (Option A) with `campaign_id`, `template_name`, and `variables_json`.
  - Added composite partial index `idx_message_dispatches_campaign` to keep transactional send lookups extremely fast.
- **Go Domain Entities & Helper Logic (`internal/domain/campaign.go`)**:
  - Implemented `SniffDelimiter` to dynamically detect CSV separators (comma, semicolon, tab).
  - Implemented `SanitizePhone` validating E.164 phone length boundaries (10-15 digits).
  - Implemented `ResolveVariables` replacing `{{variable}}` templates case-insensitively.
  - Implemented `CalculateDuration` for dynamic dispatch estimations.
- **Campaign Repository (`internal/repository/campaign.go`)**:
  - Developed type-safe pgx/v5 CRUD repository methods (`Create`, `GetByID`, `UpdateStatus`, `UpdateRecipients`, `ListByWorkspace`, `Delete`).
- **NATS JetStream stream/consumer configurations (`internal/platform/queue/jetstream.go`)**:
  - Implemented `EnsureCampaignStream` configuring the WorkQueue stream `CAMPAIGNS` with subject `campaigns.>`.
  - Implemented `EnsureCampaignConsumer` setting `MaxAckPending: 1` to guarantee sequential batch delivery across replicas.
- **Sequential Campaign Worker (`internal/platform/queue/campaign_worker.go`)**:
  - Implemented sequential background batch processor handling batch messages.
  - Checks database status and skips/ACKs immediately if `cancelled`.
  - Format messages, log dispatches, publish outbound messages, and apply delay with uniform random jitter.
- **Wiring composition root (`cmd/pergo/main.go`)**:
  - Wired up campaign stream, consumer, repository, worker startup, and shutdown registration.

## 2. Diffs & Code Map

- [016_create_campaigns.sql](file:///home/pablo/Coding/PerGo/internal/platform/postgres/migrations/016_create_campaigns.sql): Migration logic.
- [campaign.go](file:///home/pablo/Coding/PerGo/internal/domain/campaign.go): Core models and parsers.
- [campaign_worker.go](file:///home/pablo/Coding/PerGo/internal/platform/queue/campaign_worker.go): Background worker.
- [main.go](file:///home/pablo/Coding/PerGo/cmd/pergo/main.go): Composition root initialization.

## 3. Verification Results

All tests compiled and passed cleanly:
- `go test -v ./internal/domain/...` (tests snippet sniffer, phone sanitizer, resolver, and duration estimation).
- `go test -v ./internal/repository/...` (tests campaigns database CRUD operations).
- `go test -v ./internal/platform/queue/...` (tests sequential batch processing, random jitter sleeps, cancellation verification, and log enrichment).
- `go test -race ./...` (full test suite passes without any data races).
