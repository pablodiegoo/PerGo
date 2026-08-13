## Parent

#55 — feat: Broadcaster Engine Resolution Resilience & Edge Cases

## What to build

Ensure that worker crashes or restarts during dynamic campaign resolution (`CampaignStartTask`) do not leave the system in an inconsistent state or produce duplicate outbound batches.

1. **Idempotent Recipient Persistence:** Ensure repository logic inserting into `campaign_recipients` uses `ON CONFLICT DO NOTHING` (or equivalent idempotent handling) so that re-running resolution for an active campaign does not fail with database constraint errors.
2. **Deterministic JetStream Batch Publishing:** Ensure batch task publications to `campaigns.batches` include a deterministic message ID (e.g. `Nats-Msg-Id: campaign_<id>_batch_<index>`) so that JetStream's native message deduplication drops duplicate batch publications during retry.

## Acceptance criteria

- [ ] `AddRecipients` on the campaign repository handles duplicate recipient inserts gracefully without returning a constraint violation error
- [ ] Publishing `CampaignBatchTask` messages sets a deterministic `Nats-Msg-Id` based on campaign ID and batch index
- [ ] Integration test in `campaign_worker_test.go` simulates a re-delivered `CampaignStartTask` and verifies that duplicate batches are rejected by JetStream / not re-dispatched to recipients

## Blocked by

- #56 — feat: capture skipped channel contacts with discrete audit logs
