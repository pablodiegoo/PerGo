package queue

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/pablojhp.pergo/internal/domain"
)

// CampaignBatchTask is an alias for domain.CampaignBatchTask for backwards compatibility.
type CampaignBatchTask = domain.CampaignBatchTask

// CampaignEngine defines the contract for processing campaign start and batch tasks.
type CampaignEngine interface {
	ProcessStartTask(ctx context.Context, task domain.CampaignStartTask) error
	ProcessBatchTask(ctx context.Context, task domain.CampaignBatchTask) error
}

// CampaignWorker consumes campaign start and batch messages and delegates execution to CampaignEngine.
// It handles two subjects:
//   - campaigns.start: delegates to engine.ProcessStartTask
//   - campaigns.batches: delegates to engine.ProcessBatchTask
type CampaignWorker struct {
	consumer jetstream.Consumer
	engine   CampaignEngine
	cancel   context.CancelFunc
	done     chan struct{}
}

// NewCampaignWorker creates and starts a new CampaignWorker.
func NewCampaignWorker(
	ctx context.Context,
	consumer jetstream.Consumer,
	engine CampaignEngine,
) *CampaignWorker {
	ctx, cancel := context.WithCancel(ctx)
	w := &CampaignWorker{
		consumer: consumer,
		engine:   engine,
		cancel:   cancel,
		done:     make(chan struct{}),
	}
	if consumer != nil {
		go w.run(ctx)
	} else {
		close(w.done)
	}
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

	consumerName := "unknown"
	if info := w.consumer.CachedInfo(); info != nil && info.Config.Name != "" {
		consumerName = info.Config.Name
	}
	slog.Info("campaign worker started", "consumer", consumerName)

	<-ctx.Done()
	slog.Info("campaign worker stopped")
}

// HandleStartCampaign handles a campaigns.start message by delegating to engine.ProcessStartTask.
func (w *CampaignWorker) HandleStartCampaign(ctx context.Context, msg jetstream.Msg) {
	w.processStart(ctx, msg)
}

// HandleBatchCampaign handles a campaigns.batches message by delegating to engine.ProcessBatchTask.
func (w *CampaignWorker) HandleBatchCampaign(ctx context.Context, msg jetstream.Msg) {
	w.processBatch(ctx, msg)
}

func (w *CampaignWorker) processStart(ctx context.Context, msg jetstream.Msg) {
	var task domain.CampaignStartTask
	if err := json.Unmarshal(msg.Data(), &task); err != nil {
		slog.Error("campaign_worker: failed to unmarshal start task", "error", err)
		_ = msg.Ack()
		return
	}

	if err := w.engine.ProcessStartTask(ctx, task); err != nil {
		slog.Error("campaign_worker: engine process start task failed", "campaign_id", task.CampaignID, "error", err)
	}

	_ = msg.Ack()
}

func (w *CampaignWorker) processBatch(ctx context.Context, msg jetstream.Msg) {
	var task domain.CampaignBatchTask
	if err := json.Unmarshal(msg.Data(), &task); err != nil {
		slog.Error("campaign_worker: failed to unmarshal batch task", "error", err)
		_ = msg.Ack()
		return
	}

	if err := w.engine.ProcessBatchTask(ctx, task); err != nil {
		slog.Error("campaign_worker: engine process batch task failed", "campaign_id", task.CampaignID, "error", err)
	}

	_ = msg.Ack()
}

// Stop stops the campaign worker loop and blocks until it finishes.
func (w *CampaignWorker) Stop() {
	w.cancel()
	<-w.done
}
