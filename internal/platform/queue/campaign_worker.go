package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"golang.org/x/time/rate"

	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/platform/audit"
	"github.com/pablojhp.pergo/internal/repository"
)

// CampaignBatchTask represents the payload for a campaign batch message.
type CampaignBatchTask struct {
	CampaignID   uuid.UUID                  `json:"campaign_id"`
	WorkspaceID  uuid.UUID                  `json:"workspace_id"`
	BatchIndex   int                        `json:"batch_index"`
	TotalBatches int                        `json:"total_batches"`
	Recipients   []domain.CampaignRecipient `json:"recipients"`
	DelaySeconds int                        `json:"delay_seconds"`
}

// CampaignWorker consumes campaign start and batch messages sequentially.
// It handles two subjects:
//   - campaigns.start: dynamically resolves tags → recipients → publishes batch tasks
//   - campaigns.batches: dispatches individual messages per recipient
type CampaignWorker struct {
	consumer        jetstream.Consumer
	cancel          context.CancelFunc
	done            chan struct{}
	campaignRepo    *repository.CampaignRepository
	connectionsRepo *repository.ConnectionRepository
	dispatchRepo    *repository.MessageDispatchRepository
	publisher       *JetStreamPublisher
	auditWriter     audit.Writer
	tagLister       domain.TagContactLister
	msgCtx          jetstream.MessagesContext
}

// NewCampaignWorker creates and starts a new CampaignWorker.
func NewCampaignWorker(
	ctx context.Context,
	consumer jetstream.Consumer,
	campaignRepo *repository.CampaignRepository,
	connectionsRepo *repository.ConnectionRepository,
	dispatchRepo *repository.MessageDispatchRepository,
	publisher *JetStreamPublisher,
	auditWriter audit.Writer,
	tagLister domain.TagContactLister,
) *CampaignWorker {
	ctx, cancel := context.WithCancel(ctx)
	w := &CampaignWorker{
		consumer:        consumer,
		cancel:          cancel,
		done:            make(chan struct{}),
		campaignRepo:    campaignRepo,
		connectionsRepo: connectionsRepo,
		dispatchRepo:    dispatchRepo,
		publisher:       publisher,
		auditWriter:     auditWriter,
		tagLister:       tagLister,
	}
	go w.run(ctx)
	return w
}

func (w *CampaignWorker) run(ctx context.Context) {
	defer close(w.done)

	msgCtx, err := w.consumer.Messages()
	if err != nil {
		slog.Error("campaign_worker: failed to create messages context", "error", err)
		return
	}
	w.msgCtx = msgCtx
	defer msgCtx.Stop()

	slog.Info("campaign worker started", "consumer", w.consumer.CachedInfo().Config.Name)

	for {
		msg, err := msgCtx.Next()
		if err != nil {
			if ctx.Err() != nil {
				slog.Info("campaign worker stopped")
				return
			}
			slog.Error("campaign_worker: failed to get next message, recreating messages context", "error", err)
			msgCtx.Stop()

			select {
			case <-ctx.Done():
				return
			case <-time.After(1 * time.Second):
			}

			newMsgCtx, err := w.consumer.Messages()
			if err != nil {
				slog.Error("campaign_worker: failed to recreate messages context", "error", err)
				continue
			}
			msgCtx = newMsgCtx
			continue
		}

		subject := msg.Subject()
		switch subject {
		case "campaigns.start":
			w.processStart(ctx, msg)
		default:
			w.processBatch(ctx, msg)
		}
	}
}

// auditDispatchEvent bundles parameters for emitting an audit log event during campaign dispatch.
type auditDispatchEvent struct {
	WorkspaceID uuid.UUID
	TraceID     string
	EventType   string
	Status      string
	Recipient   string
	CampaignID  uuid.UUID
	Channel     string
	ErrStr      string
}

// emitAuditLog writes an audit log event for a campaign dispatch state change.
func (w *CampaignWorker) emitAuditLog(event auditDispatchEvent) error {
	if w.auditWriter == nil {
		return nil
	}
	payload := map[string]any{
		"campaign_id": event.CampaignID,
		"recipient":   event.Recipient,
		"status":      event.Status,
		"channel":     event.Channel,
	}
	if event.ErrStr != "" {
		payload["error"] = event.ErrStr
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return w.auditWriter.Write(audit.NewEvent(event.WorkspaceID, event.TraceID, event.EventType, payloadBytes))
}

// processStart handles a campaigns.start message: dynamically resolves tags,
// merges with static CSV recipients, deduplicates, persists to campaign_recipients,
// and self-publishes CampaignBatchTask messages to campaigns.batches.
func (w *CampaignWorker) processStart(ctx context.Context, msg jetstream.Msg) {
	var task domain.CampaignStartTask
	if err := json.Unmarshal(msg.Data(), &task); err != nil {
		slog.Error("campaign_worker: failed to unmarshal start task", "error", err)
		_ = msg.Ack()
		return
	}

	campaign, err := w.campaignRepo.GetByID(ctx, task.CampaignID)
	if err != nil {
		slog.Error("campaign_worker: failed to get campaign for start", "campaign_id", task.CampaignID, "error", err)
		_ = msg.Ack()
		return
	}

	if campaign.Status == domain.CampaignStatusCancelled || campaign.Status == domain.CampaignStatusFailed {
		slog.Info("campaign_worker: campaign is inactive, skipping start", "campaign_id", task.CampaignID, "status", campaign.Status)
		_ = msg.Ack()
		return
	}

	channel := "whatsapp"
	if campaign.Channel != nil {
		channel = *campaign.Channel
	}

	slog.Info("campaign_worker: processing start task", "campaign_id", task.CampaignID, "tag_ids_count", len(campaign.TagIDs), "csv_recipients_count", len(campaign.Recipients))

	// 1. Resolve tag recipients with channel filter
	tagRecords, validTagRecipients, seenPhones, err := domain.ResolveTagRecipients(ctx, w.tagLister, campaign.WorkspaceID, campaign.TagIDs, channel)
	if err != nil {
		slog.Error("campaign_worker: failed to resolve tag recipients", "campaign_id", task.CampaignID, "error", err)
		_ = w.campaignRepo.UpdateStatus(ctx, task.CampaignID, domain.CampaignStatusFailed)
		_ = msg.Ack()
		return
	}

	// 2. Merge with static CSV recipients (stored in campaign.Recipients at creation time)
	allRecords := make([]domain.CampaignRecipientRecord, 0, len(tagRecords)+len(campaign.Recipients))
	allRecords = append(allRecords, tagRecords...)

	mergedRecipients := make([]domain.CampaignRecipient, 0, len(validTagRecipients)+len(campaign.Recipients))
	mergedRecipients = append(mergedRecipients, validTagRecipients...)

	for _, csvRec := range campaign.Recipients {
		phone := csvRec.To
		if clean, valid := domain.SanitizePhone(phone); valid {
			phone = clean
		}
		if seenPhones[phone] {
			continue // deduplicate against tag-resolved recipients (tag wins)
		}
		seenPhones[phone] = true
		allRecords = append(allRecords, domain.CampaignRecipientRecord{
			Phone:     phone,
			Status:    domain.RecipientStatusPending,
			Variables: csvRec.Variables,
		})
		mergedRecipients = append(mergedRecipients, domain.CampaignRecipient{
			To:        phone,
			Variables: csvRec.Variables,
		})
	}

	// 3. Handle zero recipients (no records at all)
	if len(allRecords) == 0 {
		slog.Info("campaign_worker: campaign resolved to zero recipients", "campaign_id", task.CampaignID)
		_ = w.campaignRepo.UpdateStatus(ctx, task.CampaignID, domain.CampaignStatusCompleted)
		if auditErr := w.emitAuditLog(auditDispatchEvent{
			WorkspaceID: campaign.WorkspaceID,
			TraceID:     fmt.Sprintf("campaign_%s_start", task.CampaignID.String()),
			EventType:   "campaign.dispatch.completed_empty",
			Status:      "completed_empty",
			CampaignID:  task.CampaignID,
			Channel:     channel,
		}); auditErr != nil {
			slog.Error("campaign_worker: failed to emit completed_empty audit log", "campaign_id", task.CampaignID, "error", auditErr)
		}
		_ = msg.Ack()
		return
	}

	// 4. Persist resolved recipients to campaign_recipients table (idempotent via ON CONFLICT)
	if err := w.campaignRepo.AddRecipients(ctx, task.CampaignID, allRecords); err != nil {
		slog.Error("campaign_worker: failed to persist campaign recipients", "campaign_id", task.CampaignID, "error", err)
		_ = w.campaignRepo.UpdateStatus(ctx, task.CampaignID, domain.CampaignStatusFailed)
		_ = msg.Ack()
		return
	}

	// 5. Emit audit logs for skipped contacts
	for _, rec := range allRecords {
		if rec.Status == domain.RecipientStatusSkipped {
			if auditErr := w.emitAuditLog(auditDispatchEvent{
				WorkspaceID: campaign.WorkspaceID,
				TraceID:     fmt.Sprintf("campaign_%s_%s", task.CampaignID.String(), rec.Phone),
				EventType:   "campaign.dispatch.skipped",
				Status:      "skipped",
				Recipient:   rec.Phone,
				CampaignID:  task.CampaignID,
				Channel:     channel,
			}); auditErr != nil {
				slog.Error("campaign_worker: failed to emit skipped audit log", "campaign_id", task.CampaignID, "recipient", rec.Phone, "error", auditErr)
			}
		}
	}

	// 6. Handle case where all resolved contacts were skipped (no batches to send)
	if len(mergedRecipients) == 0 {
		slog.Info("campaign_worker: all campaign recipients were skipped", "campaign_id", task.CampaignID)
		_ = w.campaignRepo.UpdateStatus(ctx, task.CampaignID, domain.CampaignStatusCompleted)
		_ = msg.Ack()
		return
	}

	// 7. Update status to sending
	if err := w.campaignRepo.UpdateStatus(ctx, task.CampaignID, domain.CampaignStatusSending); err != nil {
		slog.Error("campaign_worker: failed to update campaign status to sending", "campaign_id", task.CampaignID, "error", err)
		_ = msg.Ack()
		return
	}

	// 8. Slice into batches and publish CampaignBatchTask messages
	batchSize := campaign.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	var batches [][]domain.CampaignRecipient
	for i := 0; i < len(mergedRecipients); i += batchSize {
		end := i + batchSize
		if end > len(mergedRecipients) {
			end = len(mergedRecipients)
		}
		batches = append(batches, mergedRecipients[i:end])
	}

	totalBatches := len(batches)
	for idx, batch := range batches {
		batchTask := CampaignBatchTask{
			CampaignID:   task.CampaignID,
			WorkspaceID:  campaign.WorkspaceID,
			BatchIndex:   idx + 1,
			TotalBatches: totalBatches,
			Recipients:   batch,
			DelaySeconds: campaign.DelaySeconds,
		}
		payload, err := json.Marshal(batchTask)
		if err != nil {
			slog.Error("campaign_worker: failed to marshal batch task during start", "campaign_id", task.CampaignID, "error", err)
			_ = msg.NakWithDelay(time.Second)
			return
		}
		traceID := fmt.Sprintf("campaign_%s_batch_%d", task.CampaignID, idx+1)
		if err := w.publisher.Publish(ctx, "campaigns.batches", payload, traceID); err != nil {
			slog.Error("campaign_worker: failed to publish batch task during start", "campaign_id", task.CampaignID, "batch", idx+1, "error", err)
			_ = msg.NakWithDelay(time.Second)
			return
		}
	}

	slog.Info("campaign_worker: start task completed, batches published",
		"campaign_id", task.CampaignID,
		"total_recipients", len(allRecords),
		"valid_recipients", len(mergedRecipients),
		"total_batches", totalBatches,
	)
	_ = msg.Ack()
}

func (w *CampaignWorker) processBatch(ctx context.Context, msg jetstream.Msg) {
	var task CampaignBatchTask
	if err := json.Unmarshal(msg.Data(), &task); err != nil {
		slog.Error("campaign_worker: failed to unmarshal batch task", "error", err)
		_ = msg.Ack()
		return
	}

	campaign, err := w.campaignRepo.GetByID(ctx, task.CampaignID)
	if err != nil {
		slog.Error("campaign_worker: failed to get campaign from DB", "campaign_id", task.CampaignID, "error", err)
		_ = msg.Ack()
		return
	}

	if campaign.Status == domain.CampaignStatusCancelled || campaign.Status == domain.CampaignStatusFailed {
		slog.Info("campaign_worker: campaign is inactive, skipping batch", "campaign_id", task.CampaignID, "status", campaign.Status)
		_ = msg.Ack()
		return
	}

	channel := "whatsapp"
	if campaign.Channel != nil {
		channel = *campaign.Channel
	}

	slog.Info("campaign_worker: processing batch", "campaign_id", task.CampaignID, "batch_index", task.BatchIndex, "recipients_count", len(task.Recipients))

	var connID uuid.UUID
	var senderIdentity string
	if campaign.ConnectionID != nil {
		connID = *campaign.ConnectionID
		if conn, err := w.connectionsRepo.GetByID(ctx, connID); err == nil && conn != nil {
			senderIdentity = conn.SenderIdentity
		}
	}

	// Token-bucket rate limiter per batch (staggered dispatch rate)
	delaySec := task.DelaySeconds
	if delaySec <= 0 {
		delaySec = 1
	}
	limiter := rate.NewLimiter(rate.Every(time.Duration(delaySec)*time.Second), 1)

	for _, recipient := range task.Recipients {
		// 1. Enforce rate limit wait
		if err := limiter.Wait(ctx); err != nil {
			slog.Info("campaign_worker: context cancelled during rate limit wait", "campaign_id", task.CampaignID)
			return
		}

		// 2. Check campaign status & handle Paused state
		if recipientCampaign, err := w.campaignRepo.GetByID(ctx, task.CampaignID); err == nil {
			if recipientCampaign.Status == domain.CampaignStatusCancelled || recipientCampaign.Status == domain.CampaignStatusFailed {
				slog.Info("campaign_worker: campaign halted mid-batch", "campaign_id", task.CampaignID, "status", recipientCampaign.Status)
				_ = msg.Ack()
				return
			}

			// Handle Paused state: wait until resumed or cancelled
			for recipientCampaign.Status == domain.CampaignStatusPaused {
				slog.Info("campaign_worker: campaign paused, waiting for resume", "campaign_id", task.CampaignID)
				select {
				case <-ctx.Done():
					return
				case <-time.After(1 * time.Second):
					cCheck, err := w.campaignRepo.GetByID(ctx, task.CampaignID)
					if err == nil && cCheck != nil {
						recipientCampaign = cCheck
					}
				}
			}
		}

		traceID := fmt.Sprintf("campaign_%s_%s", task.CampaignID.String(), recipient.To)

		// Resolve message details
		var templateName *string
		var variablesJSON map[string]string = recipient.Variables

		qMsg := domain.QueueMessage{
			WorkspaceID:    task.WorkspaceID,
			ConnectionID:   connID,
			SenderIdentity: senderIdentity,
			TraceID:        traceID,
			To:             recipient.To,
			Channel:        channel,
			QueuedAt:       time.Now(),
			CampaignID:     &task.CampaignID,
			VariablesJSON:  variablesJSON,
		}

		if channel == "whatsapp_cloud" {
			if campaign.TemplateName != nil {
				templateName = campaign.TemplateName
				qMsg.TemplateName = *campaign.TemplateName
				qMsg.Language = "pt_BR" // default language

				var params []domain.TemplateParameter
				for i := 1; ; i++ {
					val, ok := recipient.Variables[fmt.Sprintf("%d", i)]
					if !ok {
						break
					}
					params = append(params, domain.TemplateParameter{
						Type: "text",
						Text: val,
					})
				}
				if len(params) > 0 {
					qMsg.Components = []domain.TemplateComponent{
						{
							Type:       "body",
							Parameters: params,
						},
					}
				}
			}
		} else {
			if campaign.TemplateName != nil {
				qMsg.Body = domain.ResolveVariables(*campaign.TemplateName, recipient.Variables)
			}
		}

		// Create database dispatch record
		dispatch, err := w.dispatchRepo.GetOrCreateDispatch(
			ctx,
			task.WorkspaceID,
			traceID,
			channel,
			&task.CampaignID,
			templateName,
			variablesJSON,
		)
		if err != nil {
			slog.Error("campaign_worker: failed to get or create dispatch", "trace_id", traceID, "error", err)
			_ = w.campaignRepo.UpdateCounters(ctx, task.CampaignID, 0, 1)
			if auditErr := w.emitAuditLog(auditDispatchEvent{
				WorkspaceID: task.WorkspaceID,
				TraceID:     traceID,
				EventType:   "campaign_dispatch",
				Status:      "failed",
				Recipient:   recipient.To,
				CampaignID:  task.CampaignID,
				Channel:     channel,
				ErrStr:      err.Error(),
			}); auditErr != nil {
				slog.Error("campaign_worker: failed to emit audit log", "trace_id", traceID, "error", auditErr)
			}
			continue
		}

		if dispatch != nil && dispatch.Status == "delivered" {
			slog.Info("campaign_worker: recipient dispatch state delivered", "trace_id", traceID)
			if auditErr := w.emitAuditLog(auditDispatchEvent{
				WorkspaceID: task.WorkspaceID,
				TraceID:     traceID,
				EventType:   "campaign_dispatch",
				Status:      "delivered",
				Recipient:   recipient.To,
				CampaignID:  task.CampaignID,
				Channel:     channel,
			}); auditErr != nil {
				slog.Error("campaign_worker: failed to emit audit log", "trace_id", traceID, "error", auditErr)
			}
			continue
		}

		if dispatch != nil && dispatch.Status == "sent" {
			slog.Info("campaign_worker: recipient dispatch state already sent", "trace_id", traceID)
			if auditErr := w.emitAuditLog(auditDispatchEvent{
				WorkspaceID: task.WorkspaceID,
				TraceID:     traceID,
				EventType:   "campaign_dispatch",
				Status:      "sent",
				Recipient:   recipient.To,
				CampaignID:  task.CampaignID,
				Channel:     channel,
			}); auditErr != nil {
				slog.Error("campaign_worker: failed to emit audit log", "trace_id", traceID, "error", auditErr)
			}
			continue
		}

		// Publish to NATS MESSAGES stream
		payload, err := json.Marshal(qMsg)
		if err != nil {
			slog.Error("campaign_worker: failed to marshal QueueMessage", "trace_id", traceID, "error", err)
			_ = w.campaignRepo.UpdateCounters(ctx, task.CampaignID, 0, 1)
			if auditErr := w.emitAuditLog(auditDispatchEvent{
				WorkspaceID: task.WorkspaceID,
				TraceID:     traceID,
				EventType:   "campaign_dispatch",
				Status:      "failed",
				Recipient:   recipient.To,
				CampaignID:  task.CampaignID,
				Channel:     channel,
				ErrStr:      err.Error(),
			}); auditErr != nil {
				slog.Error("campaign_worker: failed to emit audit log", "trace_id", traceID, "error", auditErr)
			}
			continue
		}

		err = w.publisher.Publish(ctx, "messages.outbound", payload, traceID)
		if err != nil {
			slog.Error("campaign_worker: failed to publish message to JetStream", "trace_id", traceID, "error", err)
			_ = w.campaignRepo.UpdateCounters(ctx, task.CampaignID, 0, 1)
			if auditErr := w.emitAuditLog(auditDispatchEvent{
				WorkspaceID: task.WorkspaceID,
				TraceID:     traceID,
				EventType:   "campaign_dispatch",
				Status:      "failed",
				Recipient:   recipient.To,
				CampaignID:  task.CampaignID,
				Channel:     channel,
				ErrStr:      err.Error(),
			}); auditErr != nil {
				slog.Error("campaign_worker: failed to emit audit log", "trace_id", traceID, "error", auditErr)
			}
			continue
		}

		// Increment sent counters
		_ = w.campaignRepo.UpdateCounters(ctx, task.CampaignID, 1, 0)
		if auditErr := w.emitAuditLog(auditDispatchEvent{
			WorkspaceID: task.WorkspaceID,
			TraceID:     traceID,
			EventType:   "campaign_dispatch",
			Status:      "sent",
			Recipient:   recipient.To,
			CampaignID:  task.CampaignID,
			Channel:     channel,
		}); auditErr != nil {
			slog.Error("campaign_worker: failed to emit audit log", "trace_id", traceID, "error", auditErr)
		}
	}

	// Dynamic Sleep: delay_seconds + uniform random jitter in [-0.5s, +0.5s]
	jitter := (rand.Float64() - 0.5) * 1.0 // float value between -0.5 and +0.5
	sleepDur := time.Duration(float64(task.DelaySeconds)+jitter) * time.Second
	if sleepDur < 0 {
		sleepDur = 0
	}

	select {
	case <-ctx.Done():
		return
	case <-time.After(sleepDur):
	}

	// Update campaign status to completed if this was the last batch
	if task.BatchIndex == task.TotalBatches {
		err := w.campaignRepo.UpdateStatus(ctx, task.CampaignID, domain.CampaignStatusCompleted)
		if err != nil {
			slog.Error("campaign_worker: failed to update campaign status to completed", "campaign_id", task.CampaignID, "error", err)
		} else {
			slog.Info("campaign_worker: campaign marked as completed", "campaign_id", task.CampaignID)
		}
	}

	_ = msg.Ack()
}

// Stop stops the campaign worker loop and blocks until it finishes.
func (w *CampaignWorker) Stop() {
	w.cancel()
	if w.msgCtx != nil {
		w.msgCtx.Stop()
	}
	<-w.done
}
