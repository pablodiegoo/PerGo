## Parent

#55 — feat: Broadcaster Engine Resolution Resilience & Edge Cases

## What to build

When a campaign is configured with both dynamic tags and static CSV recipient lists, the resolution phase merges both sets of recipients. In the event that the same phone number or channel identity exists in both the database (via tag evaluation) and the CSV payload, the database contact (tag) must take precedence.

This ensures that verified contact details, custom variables stored in the database, and opt-out statuses are preserved rather than overridden by arbitrary data in an uploaded CSV file.

## Acceptance criteria

- [ ] Deduplication between tag recipients and CSV recipients resolves collisions in favor of the tag (database) contact
- [ ] If a CSV recipient has the same identity as a tag contact, the CSV entry is dropped and the tag record is retained
- [ ] Unit/Integration test in `campaign_worker_test.go` verifies collision scenarios where tag contact and CSV contact share the same identity, asserting that the tag contact's variables are preserved

## Blocked by

None — can start immediately.
