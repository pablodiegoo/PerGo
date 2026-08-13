## Problem Statement

The Broadcaster Engine's campaign resolution phase (handling `CampaignStartTask`) currently lacks robust fault tolerance for worker crashes during resolution and lacks transparency for contacts that fail channel identity matching. If the worker crashes while writing thousands of recipients to the database, restarting the task could result in duplicated batch payloads being sent downstream. Furthermore, contacts that belong to a tag but do not have an identity for the campaign's channel are silently ignored, violating strict auditability requirements for compliance-heavy use cases.

## Solution

Enhance the Broadcaster Engine's execution-time resolution phase with resilience and explicit auditing:
1. Implement `ON CONFLICT DO NOTHING` when persisting `campaign_recipients` so the resolution phase is safely idempotent on crash restarts.
2. Ensure JetStream publish deduplication natively handles downstream `CampaignBatchTask` duplication via `Nats-Msg-Id`.
3. Instead of silently dropping contacts that lack a matching channel identity, explicitly save them to `campaign_recipients` with a `skipped` status and emit an individual `campaign.dispatch.skipped` audit event for each.
4. Omit these `skipped` contacts from the generated `CampaignBatchTask` payloads to conserve queue bandwidth.
5. Guarantee that when deduplicating, database (Tag) contacts always win over static CSV contacts.

## User Stories

1. As a system operator, I want the campaign worker to safely resume tag resolution after a crash without sending duplicate batches, so that campaigns are delivered exactly once.
2. As a compliance officer, I want contacts without matching channel identities to be recorded as 'skipped' in the database, so that I can prove why specific contacts were not targeted.
3. As a compliance officer, I want a discrete audit log for every skipped contact, so that I have a trace-correlated record for audits.
4. As a system operator, I want skipped contacts to be excluded from NATS batch payloads, so that the message broker's bandwidth is conserved.
5. As a marketing manager, I want the system to prioritize my database contacts over manually uploaded CSV contacts when deduplicating, so that canonical variables and opt-outs are respected.

## Implementation Decisions

- **Module:** `internal/platform/queue/campaign_worker.go` (and related repositories like `internal/repository/campaign.go`).
- **Conflict Resolution:** In `CampaignWorker.processStart`, when merging `tagRecords` and `campaign.Recipients` (CSV), if there is a conflict (same Identity/Phone), the `tagRecords` data takes precedence.
- **Skipped Contacts Handling:** Modify `domain.ResolveTagRecipients` (or the logic in `processStart` calling it) to also return contacts that matched the tags but lacked the connection channel. These should be merged into `allRecords` with `Status = domain.RecipientStatusSkipped`.
- **Batch Payload Filtering:** When slicing `mergedRecipients` into batches in `processStart`, explicitly filter out any records with `Status == domain.RecipientStatusSkipped`.
- **Publish Deduplication:** Verify and ensure that `w.publisher.Publish` uses the provided `traceID` (e.g. `campaign_<id>_batch_<index>`) as the `Nats-Msg-Id` header for the JetStream publish call to guarantee exactly-once publish semantics.
- **Idempotent DB Insert:** Ensure `campaignRepo.AddRecipients` uses `ON CONFLICT DO NOTHING` so that if `processStart` runs twice, it doesn't fail on duplicate key errors for the same campaign and recipient.
- **Audit Emissions:** In `processStart`, loop over all skipped records and call `w.emitAuditLog` with `EventType: "campaign.dispatch.skipped"`, `Status: "skipped"`, and the appropriate recipient identity.

## Testing Decisions

- **Seams:** We will use the existing integration test seam in `internal/platform/queue/campaign_worker_test.go`, specifically expanding upon `TestCampaignWorker_StartTask_DynamicResolution`.
- **New Test:** `TestCampaignWorker_StartTask_SkippedContacts` to verify that a contact without the correct channel identity is recorded as `skipped` in the DB, generates an audit log, and is NOT published in the batch.
- **New Test:** `TestCampaignWorker_StartTask_TagOverridesCSV` to verify that a tag contact overrides a CSV contact with the same identity.
- **New Test:** `TestCampaignWorker_StartTask_Idempotency` to verify that calling `processStart` twice for the same campaign gracefully deduplicates DB inserts and NATS batch publishes (using JetStream deduplication).

## Out of Scope

- Modifying the downstream `processBatch` logic, which already handles `pending` records correctly.
- Changes to the API handlers (already handled in previous issues).
- Building UI for the skipped status (handled in a separate admin UI ticket).

## Further Notes

- See ADR-0010 (`docs/adr/0010-broadcaster-resolution-resilience.md`) for the architectural context behind these resilience patterns.
