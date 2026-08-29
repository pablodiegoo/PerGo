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
	CampaignID       uuid.UUID                  `json:"campaign_id"`
	WorkspaceID      uuid.UUID                  `json:"workspace_id"`
	BatchIndex       int                        `json:"batch_index"`
	TotalBatches     int                        `json:"total_batches"`
	Recipients       []domain.CampaignRecipient `json:"recipients"`
	DelaySeconds     int                        `json:"delay_seconds"`
	RateLimitPerMin  *int                       `json:"rate_limit_per_min,omitempty"`
	FallbackChannels []string                   `json:"fallback_channels,omitempty"`
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

	consumeCtx, err := w.consumer.Consume(func(msg jetstream.Msg) {
		subject := msg.Subject()
		switch subject {
		case "campaigns.start":
			w.processStart(ctx, msg)
		default:
			w.processBatch(ctx, msg)
		}
	})
	if err != nil {
		slog.Error("campaign_worker: failed to start consume", "error", err)
		return
	}
	defer consumeCtx.Stop()

	slog.Info("campaign worker started", "consumer", w.consumer.CachedInfo().Config.Name)

	<-ctx.Done()
	slog.Info("campaign worker stopped")
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
		"workspace_id": event.WorkspaceID,
		"trace_id":     event.TraceID,
		"campaign_id":  event.CampaignID,
		"recipient_id": event.Recipient,
		"recipient":    event.Recipient,
		"status":       event.Status,
		"channel":      event.Channel,
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
	tagRes, err := domain.ResolveTagRecipients(ctx, w.tagLister, campaign.WorkspaceID, campaign.TagIDs, channel)
	if err != nil {
		slog.Error("campaign_worker: failed to resolve tag recipients", "campaign_id", task.CampaignID, "error", err)
		_ = w.campaignRepo.UpdateStatus(ctx, task.CampaignID, domain.CampaignStatusFailed)
		_ = msg.Ack()
		return
	}

	// 2. Merge with static CSV recipients (stored in campaign.Recipients at creation time)
	allRecords, mergedRecipients := domain.MergeTagAndCSVRecipients(tagRes, campaign.Recipients)

	// 3. Handle zero recipients (no records at all)
	if len(allRecords) == 0 {
		slog.Info("campaign_worker: campaign resolved to zero recipients", "campaign_id", task.CampaignID)
		_ = w.campaignRepo.UpdateStatus(ctx, task.CampaignID, domain.CampaignStatusCompleted)
		startTraceID := fmt.Sprintf("campaign_%s_start", task.CampaignID.String())
		if auditErr := w.emitAuditLog(auditDispatchEvent{
			WorkspaceID: campaign.WorkspaceID,
			TraceID:     startTraceID,
			EventType:   "campaign.dispatch.completed_empty",
			Status:      "completed_empty",
			Recipient:   "system",
			CampaignID:  task.CampaignID,
			Channel:     channel,
		}); auditErr != nil {
			slog.Error("campaign_worker: failed to emit completed_empty audit log", "trace_id", startTraceID, "campaign_id", task.CampaignID, "error", auditErr)
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
			recipTraceID := fmt.Sprintf("campaign_%s_%s", task.CampaignID.String(), rec.Phone)
			if auditErr := w.emitAuditLog(auditDispatchEvent{
				WorkspaceID: campaign.WorkspaceID,
				TraceID:     recipTraceID,
				EventType:   "campaign.dispatch.skipped",
				Status:      "skipped",
				Recipient:   rec.Phone,
				CampaignID:  task.CampaignID,
				Channel:     channel,
			}); auditErr != nil {
				slog.Error("campaign_worker: failed to emit skipped audit log", "trace_id", recipTraceID, "campaign_id", task.CampaignID, "recipient", rec.Phone, "error", auditErr)
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
			CampaignID:       task.CampaignID,
			WorkspaceID:      campaign.WorkspaceID,
			BatchIndex:       idx + 1,
			TotalBatches:     totalBatches,
			Recipients:       batch,
			DelaySeconds:     campaign.DelaySeconds,
			RateLimitPerMin:  campaign.RateLimitPerMin,
			FallbackChannels: campaign.FallbackChannels,
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

// createRateLimiter returns a token-bucket rate limiter configured from rateLimitPerMin (if > 0)
// or falls back to delaySeconds (defaulting to 1s if <= 0).
func createRateLimiter(rateLimitPerMin *int, delaySeconds int) *rate.Limiter {
	if rateLimitPerMin != nil && *rateLimitPerMin > 0 {
		interval := time.Minute / time.Duration(*rateLimitPerMin)
		return rate.NewLimiter(rate.Every(interval), 1)
	}
	if delaySeconds <= 0 {
		delaySeconds = 1
	}
	return rate.NewLimiter(rate.Every(time.Duration(delaySeconds)*time.Second), 1)
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

	// Token-bucket rate limiter per batch (staggered dispatch rate or precision rate limit per minute)
	rateLimit := task.RateLimitPerMin
	if campaign.RateLimitPerMin != nil && *campaign.RateLimitPerMin > 0 {
		rateLimit = campaign.RateLimitPerMin
	}
	limiter := createRateLimiter(rateLimit, task.DelaySeconds)

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

		fallbackChannels := task.FallbackChannels
		if len(fallbackChannels) == 0 && campaign != nil {
			fallbackChannels = campaign.FallbackChannels
		}

		fallbackBehavior := string(domain.FallbackBehaviorDegrade)
		if campaign.FallbackBehavior != nil && *campaign.FallbackBehavior != "" {
			fallbackBehavior = *campaign.FallbackBehavior
		}

		qMsg := domain.QueueMessage{
			WorkspaceID:      task.WorkspaceID,
			ConnectionID:     connID,
			SenderIdentity:   senderIdentity,
			TraceID:          traceID,
			To:               recipient.To,
			Channel:          channel,
			QueuedAt:         time.Now(),
			FallbackChannels: fallbackChannels,
			CampaignID:       &task.CampaignID,
			VariablesJSON:    variablesJSON,
			FallbackBehavior: fallbackBehavior,
		}

		if campaign.Interactive != nil {
			// 1. Deep variable interpolation across interactive elements
			interpolated := domain.InterpolateInteractive(campaign.Interactive, recipient.Variables)

			// 2. Post-interpolation character limit validation
			if limitErr := domain.ValidateInteractiveLimits(interpolated); limitErr != nil {
				slog.Warn("campaign_worker: interactive message limits exceeded post-interpolation",
					"campaign_id", task.CampaignID,
					"recipient", recipient.To,
					"fallback_behavior", fallbackBehavior,
					"error", limitErr,
				)

				if fallbackBehavior == string(domain.FallbackBehaviorFail) {
					// Pass through interactive payload so downstream channel fails and triggers fallback channels
					qMsg.Interactive = interpolated
					qMsg.Body = interpolated.DegradeToText()
				} else {
					// Gracefully degrade into plain formatted text
					qMsg.Body = interpolated.DegradeToText()
					qMsg.Interactive = nil
				}
			} else {
				qMsg.Interactive = interpolated
				if qMsg.Body == "" {
					qMsg.Body = interpolated.Body.Text
				}
			}
		} else {
			if channel == "whatsapp_cloud" && campaign.TemplateName != nil {
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

			if campaign.MessageBody != nil {
				qMsg.Body = domain.ResolveVariables(*campaign.MessageBody, recipient.Variables)
			} else if campaign.TemplateName != nil {
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
				EventType:   "campaign.dispatch.failed",
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
				EventType:   "campaign.dispatch.delivered",
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
				EventType:   "campaign.dispatch.sent",
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
				EventType:   "campaign.dispatch.failed",
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
				EventType:   "campaign.dispatch.failed",
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
			EventType:   "campaign.dispatch.sent",
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
	<-w.done
}
