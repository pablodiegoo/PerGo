# Phase 20: WABA Read Receipts & Status Updates - Research

## Proposed Database Schema Changes

Create migration `025_add_provider_message_id_to_dispatches.sql`:
```sql
-- Add provider_message_id column to message_dispatches
ALTER TABLE message_dispatches 
ADD COLUMN provider_message_id VARCHAR(255) UNIQUE;

-- Create an index to look up dispatches quickly during webhook status processing
CREATE INDEX IF NOT EXISTS idx_message_dispatches_provider_message_id 
ON message_dispatches(provider_message_id) 
WHERE provider_message_id IS NOT NULL;
```

## Database Repository Updates

In `internal/repository/dispatch.go`:
```go
// UpdateProviderMessageID associates an external provider message ID (e.g. wamid) with a dispatch record.
func (r *MessageDispatchRepository) UpdateProviderMessageID(ctx context.Context, id uuid.UUID, providerMessageID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE message_dispatches 
		 SET provider_message_id = $1, updated_at = now()
		 WHERE id = $2`,
		providerMessageID, id,
	)
	return err
}

// GetByProviderMessageID retrieves a message dispatch by its external provider message ID.
func (r *MessageDispatchRepository) GetByProviderMessageID(ctx context.Context, providerMessageID string) (*MessageDispatch, error) {
	var d MessageDispatch
	var varsRaw []byte
	err := r.pool.QueryRow(ctx,
		`SELECT id, workspace_id, trace_id, current_channel, status, fallback_index, error_message, campaign_id, template_name, variables_json, created_at, updated_at
		 FROM message_dispatches 
		 WHERE provider_message_id = $1`,
		providerMessageID,
	).Scan(&d.ID, &d.WorkspaceID, &d.TraceID, &d.CurrentChannel, &d.Status, &d.FallbackIndex, &d.ErrorMessage, &d.CampaignID, &d.TemplateName, &varsRaw, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrDispatchNotFound
		}
		return nil, err
	}

	if len(varsRaw) > 0 {
		if err := json.Unmarshal(varsRaw, &d.VariablesJSON); err != nil {
			return nil, err
		}
	}
	return &d, nil
}
```

## Outbound Flow Integration

Update `internal/channel/whatsapp/waba.go` and `internal/platform/queue/orchestrator.go`:
1. Parse the outbound WABA JSON response to extract the first `messages[0].id` string.
2. In `orchestrator.go`'s outbound dispatch loop, when a message is successfully sent via `whatsapp_cloud`, call `UpdateProviderMessageID` with the returned `wamid`.

## Webhook Inbound Flow Integration

1. Refactor `internal/channel/whatsapp/waba_inbound.go`:
   - Parse `statuses` array inside `wabaWebhookPayload.Entry[i].Changes[j].Value`.
   - Append to the returned `InboundEvent` list with `Metadata["type"] = "status_update"`, `Body = status.Status`, `MessageID = status.ID`, `From = status.RecipientID`.
2. Refactor `internal/inbound/processor.go`'s `Process` method:
   - Check if `event.Metadata["type"] == "status_update"`.
   - If true:
     - Directly call database update on `message_dispatches` table using the repository.
     - Bypass contact resolution and audit log creation (since status updates should not create new contacts or message bubbles).
     - Publish a NATS event `messages.status_updated` so the UI receives it.

## UI Indicators Integration

1. Refactor `internal/repository/audit.go`:
   - Add `Status *string` to `ThreadMessage` struct.
   - Update `ListThreadByContact` query:
     - For the `outbound` half of the query, `LEFT JOIN message_dispatches md ON md.trace_id = al.trace_id`.
     - Select `md.status` as `status` in the column selection list.
2. Refactor `templates/components/chat_panel.templ`:
   - Next to outbound message bubble timestamps, render checks according to `message.Status`:
     - If `status == "sent"`: single gray check.
     - If `status == "delivered"`: double gray check.
     - If `status == "read"`: double blue check.
     - If `status == "failed"`: red error indicator.

## Wave Execution Split
- **Wave 1**: Migration & Backend updates (schema migration, database repo methods, outbound `wamid` extraction, inbound parser status webhook extraction).
- **Wave 2**: Inbound processor status dispatching, NATS updates, database updates, `ListThreadByContact` status retrieval, and Inbox template checks UI rendering.
