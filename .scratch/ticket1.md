## Parent

#55 — feat: Broadcaster Engine Resolution Resilience & Edge Cases

## What to build

During dynamic campaign recipient resolution (`CampaignStartTask`), contacts associated with the target tags that do not have an identity matching the campaign's channel (e.g. contact has only email for a WhatsApp campaign) must not be silently dropped.

Instead, the Broadcaster Engine records these contacts in `campaign_recipients` with a `skipped` status and emits an individual `campaign.dispatch.skipped` audit log for each skipped contact with the appropriate trace ID and workspace context. These skipped recipients are excluded from the `CampaignBatchTask` messages sent downstream to `campaigns.batches` to conserve queue throughput.

## Acceptance criteria

- [ ] Contacts matching tags without an identity for the campaign's channel are identified during dynamic resolution
- [ ] Skipped contacts are persisted in `campaign_recipients` with status `skipped`
- [ ] An individual `campaign.dispatch.skipped` audit log is emitted via `audit.Writer` for each skipped contact with full trace ID and payload details
- [ ] Skipped recipients are filtered out from `CampaignBatchTask` payloads published to `campaigns.batches`
- [ ] Integration test in `campaign_worker_test.go` verifies that a campaign targeting a tag with channel-mismatched contacts stores them as `skipped`, emits audit logs, and omits them from outbound batches

## Blocked by

None — can start immediately.
