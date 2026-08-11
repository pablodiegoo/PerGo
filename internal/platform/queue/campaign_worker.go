package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
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

// CampaignWorker consumes campaign batches sequentially and publishes individual messages.
type CampaignWorker struct {
	consumer        jetstream.Consumer
	cancel          context.CancelFunc
	done            chan struct{}
	campaignRepo    *repository.CampaignRepository
	connectionsRepo *repository.ConnectionRepository
	dispatchRepo    *repository.MessageDispatchRepository
	publisher       *JetStreamPublisher
	auditWriter     audit.Writer
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

		w.processBatch(ctx, msg)
	}
}

// EmitAuditLog writes an audit log event for a campaign dispatch state change.
func (w *CampaignWorker) EmitAuditLog(workspaceID uuid.UUID, traceID, eventType, status, recipient string, campaignID uuid.UUID, channel, errStr string) error {
	if w.auditWriter == nil {
		return nil
	}
	payload := map[string]any{
		"campaign_id": campaignID,
		"recipient":   recipient,
		"status":      status,
		"channel":     channel,
	}
	if errStr != "" {
		payload["error"] = errStr
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return w.auditWriter.Write(audit.NewEvent(workspaceID, traceID, eventType, payloadBytes))
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
			_ = w.EmitAuditLog(task.WorkspaceID, traceID, "campaign_dispatch", "failed", recipient.To, task.CampaignID, channel, err.Error())
			continue
		}

		if dispatch != nil && dispatch.Status == "delivered" {
			slog.Info("campaign_worker: recipient dispatch state delivered", "trace_id", traceID)
			_ = w.EmitAuditLog(task.WorkspaceID, traceID, "campaign_dispatch", "delivered", recipient.To, task.CampaignID, channel, "")
			continue
		}

		if dispatch != nil && dispatch.Status == "sent" {
			slog.Info("campaign_worker: recipient dispatch state already sent", "trace_id", traceID)
			_ = w.EmitAuditLog(task.WorkspaceID, traceID, "campaign_dispatch", "sent", recipient.To, task.CampaignID, channel, "")
			continue
		}

		// Publish to NATS MESSAGES stream
		payload, err := json.Marshal(qMsg)
		if err != nil {
			slog.Error("campaign_worker: failed to marshal QueueMessage", "trace_id", traceID, "error", err)
			_ = w.campaignRepo.UpdateCounters(ctx, task.CampaignID, 0, 1)
			_ = w.EmitAuditLog(task.WorkspaceID, traceID, "campaign_dispatch", "failed", recipient.To, task.CampaignID, channel, err.Error())
			continue
		}

		err = w.publisher.Publish(ctx, "messages.outbound", payload, traceID)
		if err != nil {
			slog.Error("campaign_worker: failed to publish message to JetStream", "trace_id", traceID, "error", err)
			_ = w.campaignRepo.UpdateCounters(ctx, task.CampaignID, 0, 1)
			_ = w.EmitAuditLog(task.WorkspaceID, traceID, "campaign_dispatch", "failed", recipient.To, task.CampaignID, channel, err.Error())
			continue
		}

		// Increment sent counters
		_ = w.campaignRepo.UpdateCounters(ctx, task.CampaignID, 1, 0)
		_ = w.EmitAuditLog(task.WorkspaceID, traceID, "campaign_dispatch", "sent", recipient.To, task.CampaignID, channel, "")
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
