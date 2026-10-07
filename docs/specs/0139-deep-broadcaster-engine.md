# Spec: Deep Broadcaster Engine & State Machine

## Problem Statement

In PerGo CPaaS, bulk campaign management is fragmented across four disconnected modules: the HTML admin handler, the JSON REST API handler, the campaign scheduler cron, and the NATS background worker. Each of these modules enforces its own conflicting copy of lifecycle rules, parameter defaults, and status transitions:
- The HTML handler permits `Start` only for `draft` campaigns, while the REST API permits `Start` for both `draft` and `paused` campaigns (re-evaluating recipients from scratch and causing duplicate message dispatches).
- The `Pause` and `Resume` endpoints have no status guards, allowing cancelled or completed campaigns to be resurrected into `sending` status.
- The worker prematurely marks a campaign as `completed` as soon as the batch with `BatchIndex == TotalBatches` finishes, corrupting campaign states whenever batches complete out of order or when earlier batches retry.
- In-flight pause handling enters a busy 1-second sleep loop inside the worker, executing up to 100 identical database queries per batch.
- Testing campaign behavior requires standing up real NATS testcontainers, resulting in a slow, brittle 2,202-line test suite.

## Solution

A consolidated, deep **Broadcaster Engine** module in `internal/campaign` that owns the entire campaign lifecycle end-to-end.

The engine enforces a formal, strict state transition matrix where `completed` and `cancelled` are terminal states and invalid transitions return HTTP 409 Conflict (`INVALID_CAMPAIGN_TRANSITION`). The engine handles execution-time recipient resolution, batching, variable interpolation, and atomic completion detection (verifying that zero recipients remain in pending or processing states). HTTP handlers, schedulers, and queue workers become thin adapters delegating directly to the engine. All asynchronous messaging is hidden behind a `Publisher` port, enabling fast, deterministic in-memory testing without NATS infrastructure.

## User Stories

1. As a Campaign Operator, I want to create a draft campaign with target tags or static CSV lists, so that I can prepare dispatches ahead of time.
2. As a Campaign Operator, I want campaign parameters (such as batch size, delay seconds, and interactive buttons) validated consistently across both the web console and REST API, so that invalid configurations are rejected upfront.
3. As a Campaign Operator, I want to immediately start a draft campaign, so that recipient evaluation and batch dispatch begin immediately.
4. As a Campaign Operator, I want to schedule a campaign for a future execution date and time, so that it launches automatically without manual intervention.
5. As a Campaign Operator, I want scheduled campaigns to evaluate tags dynamically at the exact moment of execution, so that recipients reflect the real-time state of the audience segment.
6. As a Campaign Operator, I want audience contacts with matching tags to be merged and deduplicated against static CSV recipients by primary identity, so that recipients never receive duplicate messages.
7. As a Campaign Operator, I want to pause an active sending campaign, so that outbound dispatches halt safely without busy-waiting or hammering the database.
8. As a Campaign Operator, I want to resume a paused campaign, so that dispatch continues for remaining pending recipients without re-evaluating tags or repeating already sent messages.
9. As a Campaign Operator, I want to cancel an active or scheduled campaign, so that all remaining pending batches are permanently halted.
10. As a Campaign Operator, I want the system to reject any attempt to resume, pause, or start a cancelled or completed campaign with an HTTP 409 Conflict, so that completed campaign history cannot be corrupted.
11. As a Campaign Operator, I want a campaign to transition to completed only after all batches and recipients have reached terminal states (sent, failed, or skipped), so that out-of-order batch processing does not prematurely report completion.
12. As a Campaign Operator, I want a campaign whose audience resolves to zero contacts to transition directly to completed and record an audit event (`campaign.dispatch.completed_empty`), so that scheduled recurring campaigns handle empty audience segments gracefully.
13. As a System Administrator running multiple PerGo server instances, I want the scheduler to atomically claim due scheduled campaigns using database row locks (`SKIP LOCKED`), so that concurrent scheduler ticks never trigger duplicate campaign dispatches.
14. As an API Client, I want all campaign control endpoints to require and enforce an authoritative `WorkspaceScope`, so that cross-tenant access to campaigns is impossible.
15. As a Backend Developer, I want to test the entire campaign lifecycle, batch calculations, and transition rules in memory using test doubles without running NATS, so that test suites run in milliseconds.

## Implementation Decisions

### 1. Dedicated Deep Package: `internal/campaign`
* Implement `BroadcasterEngine` interface and struct in `internal/campaign/engine.go`:
  ```go
  type BroadcasterEngine interface {
      Create(ctx context.Context, scope domain.WorkspaceScope, params CreateCampaignParams) (*domain.Campaign, error)
      Start(ctx context.Context, scope domain.WorkspaceScope, campaignID uuid.UUID) (*domain.Campaign, error)
      Pause(ctx context.Context, scope domain.WorkspaceScope, campaignID uuid.UUID) (*domain.Campaign, error)
      Resume(ctx context.Context, scope domain.WorkspaceScope, campaignID uuid.UUID) (*domain.Campaign, error)
      Cancel(ctx context.Context, scope domain.WorkspaceScope, campaignID uuid.UUID) (*domain.Campaign, error)
      Delete(ctx context.Context, scope domain.WorkspaceScope, campaignID uuid.UUID) error
      TriggerDue(ctx context.Context) ([]uuid.UUID, error)
      ProcessStartTask(ctx context.Context, task domain.CampaignStartTask) error
      ProcessBatchTask(ctx context.Context, task domain.CampaignBatchTask) error
  }
  ```
* All administrative methods require an authenticated `domain.WorkspaceScope` and reject mismatches.

### 2. Strict State Transition Invariants
* State transition table:
  * `draft` ➔ `sending`, `scheduled`
  * `scheduled` ➔ `sending`, `cancelled`
  * `sending` ➔ `paused`, `cancelled`, `completed`, `failed`
  * `paused` ➔ `sending`, `cancelled`
  * `completed` (terminal, immutable)
  * `cancelled` (terminal, immutable)
* Transition violations return typed `domain.ErrInvalidCampaignTransition`:
  * Mapped in HTTP handlers to **HTTP 409 Conflict** with canonical JSON:
    ```json
    {
      "code": "INVALID_CAMPAIGN_TRANSITION",
      "message": "campaign cannot transition from <from> to <to>"
    }
    ```

### 3. Execution & Atomic Completion Invariants
* **Dynamic Recipient Evaluation**: Evaluates tags via OR/Union, deduplicates against CSV contacts by primary channel identity, and inserts rows into `campaign_recipients`. If 0 recipients found, immediately transitions to `completed` and logs `campaign.dispatch.completed_empty`.
* **Atomic Completion**: After each batch completes, queries `COUNT(*) FROM campaign_recipients WHERE campaign_id = $1 AND status IN ('pending', 'processing')`. If count == 0, updates campaign status to `completed`.
* **Fail-Safe Pause**: Eliminates worker polling loops. If campaign is paused, remaining recipients in the batch are left in `pending` and worker exits cleanly.
* **Concurrency Locking in Scheduler**: `TriggerDue` claims due campaigns using:
  ```sql
  UPDATE campaigns SET status = 'sending', updated_at = NOW()
  WHERE id IN (
      SELECT id FROM campaigns 
      WHERE status = 'scheduled' AND scheduled_at <= NOW() 
      FOR UPDATE SKIP LOCKED
  )
  RETURNING id, workspace_id;
  ```

### 4. Adapter Refactors
* `internal/api/handler/admin/campaign.go`: Refactored to delegate all lifecycle operations (`Create`, `Start`, `Pause`, `Resume`, `Cancel`, `Delete`) to `BroadcasterEngine`.
* `internal/platform/queue/campaign_worker.go`: Refactored into a thin NATS consumer adapter delegating to `engine.ProcessStartTask` and `engine.ProcessBatchTask`.
* `internal/platform/queue/campaign_scheduler.go`: Refactored into a thin ticker invoking `engine.TriggerDue(ctx)`.

## Testing Decisions

* **Unit & Integration Suite (`internal/campaign/engine_test.go`)**:
  * Tests black-box behavior through `BroadcasterEngine` using a test database pool and an in-memory `FakePublisher`.
  * Verifies 100% of transitions, invalid transition rejections, tag/CSV dedup, dynamic delay with jitter, and atomic completion detection.
* **HTTP API & Admin UI Suite**:
  * Verifies HTTP 200, 202, 400, and 409 Conflict status codes and canonical error JSON payloads.
* **Smoke Suite (`internal/platform/queue/campaign_worker_test.go`)**:
  * Reduced from 2,202 lines to a single lean smoke test verifying that the NATS queue consumer decodes tasks and invokes the engine.

## Out of Scope

* Introducing new messaging channel protocols or modifying channel adapters (WABA, Telegram, whatsmeow).
* Altering the database schema for contacts, identities, or tags.
* Modifying the Outbound Processor or Dispatch Orchestrator.

## Further Notes

* Fulfills the deep module promise codified in ADR-0009 and the dynamic execution-time recipient resolution in ADR-0001.
* Fully integrates with the `WorkspaceScope` security context established in Spec #133.
