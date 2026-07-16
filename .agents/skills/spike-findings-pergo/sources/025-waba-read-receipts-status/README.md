---
spike: 025
name: waba-read-receipts-status
type: standard
validates: "Given a WABA webhook status update event, when parsed by the WABA inbound adapter and processed by the inbound processor, then the corresponding message dispatch record status is updated in the database."
verdict: VALIDATED
related: []
tags: [waba, status, receipts]
---

# Spike 025: WABA Read Receipts and Status Updates

## What This Validates
- **Given** a WABA webhook status update event (`sent`, `delivered`, `read`),
- **When** received and parsed by the WABA inbound webhook flow,
- **Then** the corresponding message dispatch record in the database is located and updated to match the new status.

## Research
Meta's WhatsApp Cloud API sends asynchronous webhook updates to our callback URL when a message's status changes:
- `sent`: Message has been processed by Meta.
- `delivered`: Message has been delivered to the recipient's device.
- `read`: Message has been opened/read by the recipient.

These events are payload updates on the `messages` subscription with a `statuses` array containing objects like:
```json
{
  "id": "wamid.HBgLNTUxMTk5OTk5ODg4OBIVAgY0NkE0NkUzQzc5RjNFRDIyMkUAAg==",
  "status": "delivered",
  "timestamp": "1675276634",
  "recipient_id": "5511999998888"
}
```

### Critical Gap
The `message_dispatches` table currently does not store the external `provider_message_id` returned by Meta's outbound API response. Without this field, there is no way to relate the webhook status update (`statuses[i].id`) back to our local dispatch record.

**Solution to Spike:**
1. Propose adding a `provider_message_id` VARCHAR/TEXT column to the `message_dispatches` table.
2. Update the outbound dispatcher flow to extract the `id` from the Meta API response and store it.
3. Update the inbound webhook parser to parse status events instead of skipping them.
4. Update the database state for the matching dispatch.

## How to Run
We built a prototype test script `test_status_receipts.go` that:
1. Automatically sets up a temporary database table with the new schema columns.
2. Simulates sending a WABA template message, returning a mock `wamid` response.
3. Stores the `wamid` on the dispatch record.
4. Feeds a mock WABA status update webhook payload (`statuses` containing that `wamid`) into the webhook processor.
5. Asserts that the dispatch record status updates correctly.

To run:
```bash
go run .planning/spikes/025-waba-read-receipts-status/test_status_receipts.go
```

## Investigation Trail
- **Iteration 1**: Discovered that `statuses` webhook payload contains `id` which corresponds to the message ID (`wamid...`) returned in the HTTP response when dispatching outbound messages.
- **Iteration 2**: Found that `message_dispatches` table has no field to store this ID, which makes status matching impossible.
- **Iteration 3**: Prototyped a migration adding a `provider_message_id` column to `message_dispatches`.
- **Iteration 4**: Verified the flow of extracting `statuses` from the webhook, matching by `provider_message_id`, and successfully updating status to `delivered`.

## Results
- **Verdict**: **VALIDATED**
- **Outcome**: The database status updates perfectly when matching by `provider_message_id`. We successfully parsed the `statuses` updates.
- **Signal for build**: We need a migration to add `provider_message_id` column to `message_dispatches` table and update the WABA adapter/dispatcher to capture and update the dispatch with `provider_message_id` when sending messages.
