# WABA Status Receipts and Read Confirmations

## Requirements
- Must track individual delivery and read status updates (`sent`, `delivered`, `read`) for messages dispatched via WABA (WhatsApp Cloud API).
- Must record status transitions in the database without disrupting the active customer messaging flow.

## How to Build It

### 1. Database Schema Migration
Add `provider_message_id` to the `message_dispatches` table so that we can map asynchronous webhook status updates back to the original database record:
```sql
ALTER TABLE message_dispatches 
ADD COLUMN provider_message_id VARCHAR(255) UNIQUE;
```

### 2. Outbound Dispatch Recording
When dispatching outbound messages in `internal/channel/whatsapp/waba.go`, extract the `id` from the successful JSON response body:
```json
{
  "messages": [{"id": "wamid.HBgLNTUx..."}]
}
```
And save it to the database:
```go
// Parse response
type wabaResponse struct {
	Messages []struct {
		ID string `json:"id"`
	} `json:"messages"`
}
var resp wabaResponse
if err := json.Unmarshal(respBytes, &resp); err == nil && len(resp.Messages) > 0 {
	providerMessageID := resp.Messages[0].ID
	_ = r.dispatchRepo.UpdateProviderMessageID(ctx, dispatch.ID, providerMessageID)
}
```

### 3. Inbound Webhook Parser
Update `WABAInboundAdapter.Parse` to detect and parse the `statuses` array from the webhook:
```go
type wabaStatus struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	Timestamp   string `json:"timestamp"`
	RecipientID string `json:"recipient_id"`
}

// In ValueData struct:
Statuses []wabaStatus `json:"statuses,omitempty"`
```
Instead of skipping them, map them to an `InboundEvent` with metadata to distinguish them from standard inbound messages:
```go
for _, status := range change.Value.Statuses {
	events = append(events, &inbound.InboundEvent{
		MessageID: status.ID,
		Channel:   "whatsapp_cloud",
		From:      status.RecipientID,
		Body:      status.Status,
		Metadata: map[string]string{
			"type":      "status_update",
			"status":    status.Status,
			"timestamp": status.Timestamp,
		},
	})
}
```

### 4. Inbound Processing and UI Updates
In `inbound/processor.go`, skip contact resolution/creation for events with `"type": "status_update"`. Directly update the `message_dispatches` status in the database and publish a NATS event to trigger real-time UI/CRM updates (like sending a `"message-status-updated"` HTMX trigger to the client).

## What to Avoid
- **Do NOT** assume status updates only arrive after a delay; they can sometimes arrive out of order.
- **Do NOT** skip or fail webhooks if a status update belongs to a message sent outside PerGo (where no matching `provider_message_id` exists in the local database). Handle `pgx.ErrNoRows` or 0 rows affected gracefully.

## Constraints
- Status updates (`sent`, `delivered`, `read`) are WABA-specific in this context. WhatsMeow (WhatsApp Web) uses separate socket confirmation packets which are handled differently.

## Origin
Synthesized from spikes: 025
Source files available in: sources/025-waba-read-receipts-status/
