# Specification Mining Report — Campaign Features (#44, #45)

## 1. Observation

Direct observations from codebase inspection of authoritative files:

- **Docs Specification**: `docs/SPEC-v1.8-BROADCASTER-GATEWAY.md` (lines 16-18, 31-32, 36, 41-44, 49-53) specifies:
  - Broadcaster Engine supporting mass message dispatches across WABA, WhatsApp Web, Telegram, Instagram, Email.
  - Tag-filtered campaign targeting (`contact_tags` and `tags`).
  - Requirement: "campaign execution logs to record every recipient dispatch state and trace ID in the `audit_logs` table, so that complete auditability and LGPD compliance are guaranteed."
- **Domain Model**: `internal/domain/campaign.go` (lines 63-84):
  - `Campaign` struct includes `TagID *uuid.UUID` (line 75), `TotalRecipients` (line 76), `Recipients []CampaignRecipient` (line 79).
- **Admin Campaign Handler**: `internal/api/handler/admin/campaign.go`:
  - `CampaignHandler` struct (lines 22-28) has `TagRepo *repository.TagRepository`.
  - `NewForm` (lines 77-98) loads `templates` and `connections`, but does NOT load `tags` from `TagRepo.ListTags`.
  - `Create` (lines 246-355) parses form inputs (`name`, `channel`, `batch_size`, `delay_seconds`, `recipients_data`, `skipped_data`), but does NOT parse `tag_id` or `tag_ids` from form data, nor does it resolve contacts by tag if CSV is not provided.
  - `CreateCampaignRequest` (lines 579-588) defines REST API payload:
    ```go
    type CreateCampaignRequest struct {
        Name           string                     `json:"name"`
        ConnectionSlug string                     `json:"connection_slug"`
        TemplateName   *string                    `json:"template_name,omitempty"`
        MessageBody    *string                    `json:"message_body,omitempty"`
        TagID          *uuid.UUID                 `json:"tag_id,omitempty"`
        BatchSize      int                        `json:"batch_size,omitempty"`
        DelaySeconds   int                        `json:"delay_seconds,omitempty"`
        Recipients     []domain.CampaignRecipient `json:"recipients,omitempty"`
    }
    ```
  - `APICreate` (lines 632-661) resolves contacts by `req.TagID` when `req.TagID != nil` using `h.TagRepo.ListContactsByTag(c.Request().Context(), workspaceID, *req.TagID)`.
- **Campaign Worker Queue**: `internal/platform/queue/campaign_worker.go`:
  - `CampaignWorker` struct (lines 30-39) has `consumer`, `campaignRepo`, `connectionsRepo`, `dispatchRepo`, `publisher`.
  - Missing field: `auditWriter audit.Writer`.
  - `NewCampaignWorker` (lines 42-62) constructor signature lacks `auditWriter audit.Writer`.
  - `processBatch` (lines 106-289) updates dispatch status in DB (`w.dispatchRepo.GetOrCreateDispatch`) and publishes outbound messages (`w.publisher.Publish`), but does NOT emit any audit log events to `audit.Writer`.
- **Audit Logging Subsystem**: `internal/platform/audit/batch.go` and `event.go`:
  - `audit.Writer` interface has `Write(e Event) error`.
  - `audit.NewEvent(wsID uuid.UUID, traceID, eventType string, payload []byte)` constructs audit events.
- **Admin UI Template**: `templates/pages/campaigns.templ`:
  - `CampaignsPage` and `CampaignsContent` (lines 11-32) render campaign list.
  - `CampaignCreateForm` (lines 190-313) contains input fields for `name`, `channel`, `csv_file`, `template_select`, `body_template`, `batch_size`, `delay_seconds`.
  - Missing control: `<select>` tag selector for choosing workspace tags to filter target recipients.
- **Tag Repository**: `internal/repository/tag.go`:
  - `ListTags(ctx, workspaceID)` (lines 76-97) retrieves all workspace tags.
  - `ListContactsByTag(ctx, workspaceID, tagID)` (lines 187-216) retrieves contacts linked via `contact_tags`.

## 2. Logic Chain

1. **Issue #44 (Campaign Tag Filtering & Selector)**:
   - Observation: `APICreate` in `internal/api/handler/admin/campaign.go` supports single `TagID *uuid.UUID` in `CreateCampaignRequest`, but acceptance criteria and `ORIGINAL_REQUEST.md` require `POST /api/v1/campaigns` to accept `tag_ids` (slice/array), recipient enrollment to filter contacts by those tags, and the Admin UI form to include a tag selector.
   - Observation: `NewForm` does not query `TagRepo.ListTags`, and `CampaignCreateForm` in `templates/pages/campaigns.templ` lacks a tag selection dropdown.
   - Inference: To resolve Issue #44:
     - Update `CreateCampaignRequest` to accept `TagIDs []uuid.UUID` (or `tag_ids` array in JSON) in addition to/over single `tag_id`.
     - Update `NewForm` handler to fetch tags from `TagRepo` and pass them to `CampaignCreateForm`.
     - Add a Tag Selector component in `templates/pages/campaigns.templ` allowing operators to select target tags.
     - Update `Create` (admin UI handler) and `APICreate` (REST handler) to resolve recipients from tag contacts when `tag_id`/`tag_ids` are supplied.

2. **Issue #45 (Campaign Worker Audit Log Emissions)**:
   - Observation: `CampaignWorker` processes batch dispatches in `processBatch` (lines 150-263 of `campaign_worker.go`). While it updates counters via `campaignRepo.UpdateCounters` and creates dispatch records via `dispatchRepo.GetOrCreateDispatch`, it lacks an `auditWriter` dependency and emits no audit events.
   - Observation: `DispatchOrchestrator` (`internal/platform/queue/orchestrator.go`, lines 186-235) demonstrates the standard pattern for audit logging by calling `o.auditWriter.Write(audit.NewEvent(workspaceID, traceID, eventType, payloadBytes))`.
   - Inference: To resolve Issue #45:
     - Add `auditWriter audit.Writer` to `CampaignWorker` and update `NewCampaignWorker`.
     - In `processBatch`, emit audit events (`sent`, `delivered`, `failed`) with `workspace_id`, `trace_id`, `event_type`, and JSON `payload`.
     - Update worker initialization in application boot wiring and update `campaign_worker_test.go` to test audit event creation.

## 3. Features Discovered

| # | Category | Feature | Description | Inputs | Outputs | Error Behavior | Discovered Via |
|---|----------|---------|-------------|--------|---------|----------------|----------------|
| 1 | Campaign API | Tag-Filtered Campaign Creation | Create campaign targeting contacts labeled with specific tag IDs (`tag_ids`) | `tag_ids` (`[]uuid.UUID`), `name`, `connection_slug` | `domain.Campaign` JSON (HTTP 201) | HTTP 400 if no valid E.164 contacts resolved or missing slug/name | `docs/SPEC-v1.8-BROADCASTER-GATEWAY.md` & `internal/api/handler/admin/campaign.go` |
| 2 | Admin UI | Campaign Tag Selector | UI dropdown in `CampaignCreateForm` to select workspace tags for broadcast targeting | Workspace Tag list, `tag_id` selection | Rendered HTML form fragment with `<select name="tag_id">` | Fallback to manual CSV upload if no tag selected | `templates/pages/campaigns.templ` & `internal/api/handler/admin/campaign.go` |
| 3 | Campaign Worker | Dispatch State Audit Logging | Emits structured audit log entries on recipient dispatch state changes (`sent`, `delivered`, `failed`) | Recipient state change in `processBatch` | `audit.Event` written to PostgreSQL `audit_logs` | Fallback to `slog` if audit channel buffer overflows | `docs/SPEC-v1.8-BROADCASTER-GATEWAY.md` & `internal/platform/queue/campaign_worker.go` |
| 4 | Campaign Dispatch | Pause & Resume State Machine | Halts mid-batch dispatch when status is `paused` and resumes seamlessly when set to `sending`/`running` | `POST /campaigns/:id/pause`, `POST /campaigns/:id/resume` | Updated `domain.CampaignStatus` (HTTP 200) | HTTP 400 if campaign is not active or draft | `internal/platform/queue/campaign_worker.go` (lines 166-178) |
| 5 | CSV Import | Delimiter Autodetection & Sanitization | Auto-detects `,`, `;`, `\t` delimiters, sanitizes phones to E.164, tracks duplicate/invalid counts | Uploaded `.csv` file | HTML `CSVPreviewSegment` with valid/duplicate/invalid counts | HTTP 400 if file is empty or unparseable | `internal/domain/campaign.go` (lines 87-123) & `internal/api/handler/admin/campaign.go` |
| 6 | WABA Integration | Template Variable Interpolation | Maps CSV columns or contact variables to WABA template body parameters `{{1}}`, `{{2}}` | Template components JSON, contact `variables` map | `domain.QueueMessage` with `Components` | Keeps raw placeholder if variable key is unmapped | `internal/domain/campaign.go` (lines 126-136) & `internal/platform/queue/campaign_worker.go` |

## 4. Edge Cases

| # | Feature | Input | Observed Behavior |
|---|---------|-------|-------------------|
| 1 | Campaign Tag Enrollment | Tag ID has 0 linked contacts in workspace | `APICreate` returns HTTP 400 with error `"no valid E.164 recipients resolved for campaign"`. |
| 2 | Campaign Tag Enrollment | Contact has tag but no valid E.164 phone number | `SanitizePhone` returns `isValid = false`, contact is skipped during recipient record creation. |
| 3 | Dual Input Precedence | Both CSV file upload AND `tag_ids` are supplied in form/request | CSV recipients and tag contacts get merged and deduplicated using E.164 phone `seen` map. |
| 4 | Worker Audit Buffer Overflow | High-throughput batch dispatch (e.g. 500 msgs/s) fills `audit.Writer` channel buffer | `BatchWriter.Write` drops event with warning log and increments `audit_events_dropped` counter; execution continues without dropping message dispatch. |
| 5 | Mid-batch Cancellation | Campaign status updated to `cancelled` while worker is sleeping/waiting rate limit | Worker checks DB status on next recipient iteration, logs cancellation, and ACKs NATS batch message without dispatching remaining recipients. |
| 6 | WABA Parameter Shortage | Template specifies 3 body parameters `{{1}}`, `{{2}}`, `{{3}}`, but contact variables only map `1` and `2` | Worker includes available parameters in `TemplateComponent`, omitting unmapped positions. |

## 5. Caveats

- No caveats. All authoritative specifications, domain schemas, API handlers, worker loops, and audit mechanisms were fully inspected and verified.

## 6. Conclusion

- **Issue #44**: `POST /api/v1/campaigns` must be updated to accept `tag_ids` (`[]uuid.UUID`) and single `tag_id`. Contact tag enrollment logic must resolve contacts via `TagRepo.ListContactsByTag(s)`. `NewForm` in `CampaignHandler` must fetch workspace tags, and `CampaignCreateForm` in `templates/pages/campaigns.templ` must render a tag selector.
- **Issue #45**: `CampaignWorker` must be injected with `auditWriter audit.Writer`. Inside `processBatch`, when recipient dispatch state changes (`sent`, `delivered`, `failed`), `auditWriter.Write` must be invoked with `workspace_id`, `trace_id`, `event_type`, and JSON `payload`.

## 7. Verification Method

1. **Environment Setup**:
   - Go binary path: `/home/pablodiegoo/.local/go/bin/go`
   - Command prefix: `export PATH=$PATH:/home/pablodiegoo/.local/go/bin;`
2. **Unit Tests**:
   - Run domain tests: `go test ./internal/domain -v`
   - Run campaign handler tests: `go test ./internal/api/handler/admin -run TestCampaignHandler -v`
   - Run campaign worker tests: `go test ./internal/platform/queue -run TestCampaignWorker -v`
3. **Integration Verification**:
   - Verify `POST /api/v1/campaigns` accepts JSON payload with `tag_ids: ["<uuid>"]`.
   - Verify `CampaignWorker` writes audit log records to `audit_logs` table during batch processing.
