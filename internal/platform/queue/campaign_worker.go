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
	"github.com/pablojhp.pergo/internal/domain"
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
	consumer     jetstream.Consumer
	cancel       context.CancelFunc
	done         chan struct{}
	campaignRepo *repository.CampaignRepository
	dispatchRepo *repository.MessageDispatchRepository
	publisher    *JetStreamPublisher
	msgCtx       jetstream.MessagesContext
}

// NewCampaignWorker creates and starts a new CampaignWorker.
func NewCampaignWorker(
	ctx context.Context,
	consumer jetstream.Consumer,
	campaignRepo *repository.CampaignRepository,
	dispatchRepo *repository.MessageDispatchRepository,
	publisher    *JetStreamPublisher,
) *CampaignWorker {
	ctx, cancel := context.WithCancel(ctx)
	w := &CampaignWorker{
		consumer:     consumer,
		cancel:       cancel,
		done:         make(chan struct{}),
		campaignRepo: campaignRepo,
		dispatchRepo: dispatchRepo,
		publisher:    publisher,
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

	if campaign.Status == domain.CampaignStatusCancelled {
		slog.Info("campaign_worker: campaign is cancelled, skipping batch", "campaign_id", task.CampaignID, "batch_index", task.BatchIndex)
		_ = msg.Ack()
		return
	}

	channel := "whatsapp"
	if campaign.Channel != nil {
		channel = *campaign.Channel
	}

	slog.Info("campaign_worker: processing batch", "campaign_id", task.CampaignID, "batch_index", task.BatchIndex, "recipients_count", len(task.Recipients))

	for _, recipient := range task.Recipients {
		// Double-check cancellation before sending each message
		if recipientCampaign, err := w.campaignRepo.GetByID(ctx, task.CampaignID); err == nil {
			if recipientCampaign.Status == domain.CampaignStatusCancelled {
				slog.Info("campaign_worker: campaign cancelled mid-batch, halting batch processing", "campaign_id", task.CampaignID)
				_ = msg.Ack()
				return
			}
		}

		traceID := fmt.Sprintf("campaign_%s_%s", task.CampaignID.String(), recipient.To)

		// Resolve message details
		var templateName *string
		var variablesJSON map[string]string = recipient.Variables

		qMsg := domain.QueueMessage{
			WorkspaceID:   task.WorkspaceID,
			TraceID:       traceID,
			To:            recipient.To,
			Channel:       channel,
			QueuedAt:      time.Now(),
			CampaignID:    &task.CampaignID,
			VariablesJSON: variablesJSON,
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
		_, err := w.dispatchRepo.GetOrCreateDispatch(
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
			continue
		}

		// Publish to NATS MESSAGES stream
		payload, err := json.Marshal(qMsg)
		if err != nil {
			slog.Error("campaign_worker: failed to marshal QueueMessage", "trace_id", traceID, "error", err)
			continue
		}

		err = w.publisher.Publish(ctx, "messages.outbound", payload, traceID)
		if err != nil {
			slog.Error("campaign_worker: failed to publish message to JetStream", "trace_id", traceID, "error", err)
			continue
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
