# Project: PerGo Code Review Fixes (R1 - R6)

## Architecture
- `internal/platform/breaker`: Circuit breaker state machine (`breaker.go`, `breaker_test.go`)
- `internal/domain/campaign` & `internal/api/handler/admin`: Shared tag-recipient resolution, CSV recipient validation, campaign admin handlers & tests (`campaign.go`, `campaign_test.go`)
- `internal/channel/telegram`: Telegram adapter S3 media download & error wrapping (`telegram.go`, `telegram_challenge_test.go`, `telegram_test.go`)
- `internal/api/handler` & `internal/platform/queue`: Ingress message idempotency & campaign worker audit log emission (`message.go`, `campaign_worker.go`, `campaign_worker_test.go`)
- `internal/api/handler/admin` & `cmd/pergo`: Tag admin handler struct & dependency injection (`tag.go`, `main.go`, `tag_test.go`)

## Feature Inventory
| # | Feature | Description | Milestone | Source | File Scope |
|---|---------|-------------|-----------|--------|------------|
| 1 | R1: Breaker Half-Open Fix | Reset consecutiveFailures on open->half-open; verify RecordSuccess zeros counters | M1 | ORIGINAL_REQUEST.md | `internal/platform/breaker/breaker.go`, `breaker_test.go` |
| 2 | R2: Tag-recipient Resolution | Extract tag->contact->phone logic to shared helper; remove SanitizePhone fallback | M2 | ORIGINAL_REQUEST.md | `internal/domain/campaign.go`, `internal/api/handler/admin/campaign.go` |
| 3 | R3: Recipient Validation | Return HTTP 400 / HTMX error fragment when len(recipients)==0 | M2 | ORIGINAL_REQUEST.md | `internal/api/handler/admin/campaign.go`, `campaign_test.go` |
| 4 | R4: Idempotency & Audit Errors | Surface idempotency/audit errors with slog.Error & trace ID; unexport emitAuditLog with struct param | M3 | ORIGINAL_REQUEST.md | `internal/api/handler/message.go`, `internal/platform/queue/campaign_worker.go`, `campaign_worker_test.go` |
| 5 | R5: Telegram Error Wrap Fix | Wrap only ErrTelegramMediaRetryable with %w in S3 download path | M4 | ORIGINAL_REQUEST.md | `internal/channel/telegram/telegram.go`, `telegram_challenge_test.go`, `telegram_test.go` |
| 6 | R6: Tag Handler Signature | Make wsRepo required param in NewTagAdminHandler; remove nil guard | M5 | ORIGINAL_REQUEST.md | `internal/api/handler/admin/tag.go`, `cmd/pergo/main.go`, `tag_test.go` |

## Milestones
| # | Name | Scope | Dependencies | Status |
|---|------|-------|-------------|--------|
| M1 | Circuit Breaker Fix (R1) | `internal/platform/breaker` | None | DONE |
| M2 | Tag Recipient Resolution & Validation (R2, R3) | `internal/domain`, `internal/api/handler/admin` | None | DONE |
| M3 | Idempotency & Audit Error Logging (R4) | `internal/api/handler/message.go`, `internal/platform/queue` | None | DONE |
| M4 | Telegram Error Wrap (R5) | `internal/channel/telegram` | None | DONE |
| M5 | Tag Admin Handler Signature (R6) | `internal/api/handler/admin/tag.go`, `cmd/pergo/main.go` | None | DONE |
| M6 | Integration Verification & Audit | Repository-wide (`go test ./...`) | M1, M2, M3, M4, M5 | DONE |

## Interface Contracts
- R2 Shared Helper: `ResolveTagRecipients(ctx, lister, wsID, tagIDs)` returning `([]CampaignRecipientRecord, []CampaignRecipient, map[string]bool, error)`. Helper `DeduplicateUUIDs(ids []uuid.UUID) []uuid.UUID`.
- R4 Struct Parameter: `type auditDispatchEvent struct { ... }` passed to unexported `emitAuditLog(event auditDispatchEvent) error`.
- R6 Constructor: `func NewTagAdminHandler(tagRepo *repository.TagRepository, contactRepo *repository.ContactRepository, wsRepo *repository.WorkspaceRepository) *TagAdminHandler`.

## Code Layout & Boundaries
- Milestone 1 exclusively owns `internal/platform/breaker/*`
- Milestone 2 exclusively owns `internal/domain/campaign.go`, `internal/api/handler/admin/campaign.go`, `internal/api/handler/admin/campaign_test.go`
- Milestone 3 exclusively owns `internal/api/handler/message.go`, `internal/platform/queue/campaign_worker.go`, `internal/platform/queue/campaign_worker_test.go`
- Milestone 4 exclusively owns `internal/channel/telegram/*`
- Milestone 5 exclusively owns `internal/api/handler/admin/tag.go`, `cmd/pergo/main.go`, `internal/api/handler/admin/tag_test.go`
