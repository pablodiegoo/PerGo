package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/pablojhp.pergo/internal/repository"
	"github.com/pablojhp.pergo/internal/webhook"
)

type WebhookEvent struct {
	Event       string `json:"event"`
	TraceID     string `json:"trace_id"`
	MessageID   string `json:"message_id"`
	Channel     string `json:"channel"`
	Timestamp   string `json:"timestamp"`
	WorkspaceID string `json:"workspace_id"`
	Error       string `json:"error,omitempty"`
}

type WebhookWorker struct {
	nc               *nats.Conn
	js               jetstream.JetStream
	consumer         jetstream.Consumer
	inboundConsumer  jetstream.Consumer
	deliveryConsumer jetstream.Consumer
	dispatcher       webhook.WebhookDispatcher
	subRepo          *repository.WebhookSubscriptionRepository
	cancel           context.CancelFunc
	wg               sync.WaitGroup
	done             chan struct{}
}

func NewWebhookWorker(ctx context.Context, nc *nats.Conn, dispatcher webhook.WebhookDispatcher, subRepo *repository.WebhookSubscriptionRepository) (*WebhookWorker, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("jetstream.New: %w", err)
	}

	// Ensure WEBHOOKS stream exists
	stream, err := EnsureWebhookStream(ctx, nc)
	if err != nil {
		return nil, fmt.Errorf("ensure webhook stream: %w", err)
	}

	// Create pull consumer
	consumer, err := createConsumerWithRetry(ctx, stream, jetstream.ConsumerConfig{
		Durable:       "webhooks-consumer",
		Description:   "Webhook delivery worker consumer",
		DeliverPolicy: jetstream.DeliverAllPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       15 * time.Second,
		MaxDeliver:    10,
		FilterSubject: "webhooks.events",
	})
	if err != nil {
		return nil, fmt.Errorf("create webhook consumer: %w", err)
	}

	// Ensure INBOUND stream exists
	inboundStream, err := EnsureInboundStream(ctx, nc)
	if err != nil {
		return nil, fmt.Errorf("ensure inbound stream: %w", err)
	}

	// Create inbound consumer
	inboundConsumer, err := createConsumerWithRetry(ctx, inboundStream, jetstream.ConsumerConfig{
		Durable:       "inbound-webhooks-consumer",
		Description:   "Inbound webhook delivery worker consumer",
		DeliverPolicy: jetstream.DeliverAllPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       15 * time.Second,
		MaxDeliver:    10,
		FilterSubject: "inbound.events.>",
	})
	if err != nil {
		return nil, fmt.Errorf("create inbound webhook consumer: %w", err)
	}

	// Ensure WEBHOOK_DELIVERIES stream exists
	deliveryStream, err := EnsureWebhookDeliveryStream(ctx, nc)
	if err != nil {
		return nil, fmt.Errorf("ensure webhook delivery stream: %w", err)
	}

	// Create delivery consumer
	deliveryConsumer, err := createConsumerWithRetry(ctx, deliveryStream, jetstream.ConsumerConfig{
		Durable:       "webhooks-deliveries-consumer",
		Description:   "Webhook delivery task worker consumer",
		DeliverPolicy: jetstream.DeliverAllPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       15 * time.Second,
		MaxDeliver:    10,
		FilterSubject: "webhooks.deliveries.>",
	})
	if err != nil {
		return nil, fmt.Errorf("create webhook delivery consumer: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	w := &WebhookWorker{
		nc:               nc,
		js:               js,
		consumer:         consumer,
		inboundConsumer:  inboundConsumer,
		deliveryConsumer: deliveryConsumer,
		dispatcher:       dispatcher,
		subRepo:          subRepo,
		cancel:           cancel,
		done:             make(chan struct{}),
	}

	w.wg.Add(3)
	go w.run(ctx, w.consumer, "outbound")
	go w.run(ctx, w.inboundConsumer, "inbound")
	go w.run(ctx, w.deliveryConsumer, "delivery")
	return w, nil
}

func (w *WebhookWorker) Stop() {
	w.cancel()
	w.wg.Wait()
}

func (w *WebhookWorker) SetWorkspaceRepository(repo *repository.WorkspaceRepository) {
	// Deprecated: workspace opt-in checks are now handled directly inside the WebhookDispatcher.
}

func (w *WebhookWorker) run(ctx context.Context, cons jetstream.Consumer, mode string) {
	defer w.wg.Done()
	slog.Info("webhook worker thread started", "mode", mode)

	for {
		select {
		case <-ctx.Done():
			slog.Info("webhook worker thread stopping", "mode", mode)
			return
		default:
			msgs, err := cons.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
					continue
				}
				slog.Error("webhook worker: fetch error", "error", err, "mode", mode)
				time.Sleep(1 * time.Second)
				continue
			}

			for msg := range msgs.Messages() {
				if mode == "delivery" {
					w.processDelivery(ctx, msg)
				} else {
					w.processEvent(ctx, msg, mode)
				}
			}
		}
	}
}

func (w *WebhookWorker) processEvent(ctx context.Context, msg jetstream.Msg, mode string) {
	var evt WebhookEvent
	if err := json.Unmarshal(msg.Data(), &evt); err != nil {
		slog.Error("webhook worker: failed to unmarshal event", "error", err)
		_ = msg.Ack()
		return
	}

	wsID, err := uuid.Parse(evt.WorkspaceID)
	if err != nil {
		slog.Error("webhook worker: invalid workspace ID", "error", err, "workspace_id", evt.WorkspaceID)
		_ = msg.Ack()
		return
	}

	// Look up active subscriptions for this workspace
	subs, err := w.subRepo.ListByWorkspace(ctx, wsID)
	if err != nil {
		slog.Error("webhook worker: failed to list subscriptions", "error", err, "workspace_id", wsID)
		_ = msg.NakWithDelay(5 * time.Second)
		return
	}

	publisher := NewJetStreamPublisher(w.nc)

	for _, sub := range subs {
		if !sub.Active {
			continue
		}

		if webhook.MatchesAny(sub.EventTypes, evt.Event) {
			task := webhook.WebhookDeliveryTask{
				ID:             uuid.New(),
				SubscriptionID: sub.ID,
				WorkspaceID:    wsID,
				Event:          evt.Event,
				TraceID:        evt.TraceID,
				MessageID:      evt.MessageID,
				Payload:        msg.Data(),
				Mode:           mode,
			}

			taskData, err := json.Marshal(task)
			if err != nil {
				slog.Error("webhook worker: failed to marshal delivery task", "error", err, "subscription_id", sub.ID)
				continue
			}

			subject := fmt.Sprintf("webhooks.deliveries.%s.%s", wsID, sub.ID)
			err = publisher.Publish(ctx, subject, taskData, task.ID.String())
			if err != nil {
				slog.Error("webhook worker: failed to publish delivery task", "error", err, "subject", subject)
				_ = msg.NakWithDelay(5 * time.Second)
				return
			}
			slog.Info("webhook worker: fanned out task", "subject", subject, "trace_id", evt.TraceID)
		}
	}

	_ = msg.Ack()
}

func (w *WebhookWorker) processDelivery(ctx context.Context, msg jetstream.Msg) {
	var task webhook.WebhookDeliveryTask
	if err := json.Unmarshal(msg.Data(), &task); err != nil {
		slog.Error("webhook worker: failed to unmarshal delivery task", "error", err)
		_ = msg.Ack()
		return
	}

	err := w.dispatcher.Dispatch(ctx, task)
	if err != nil {
		slog.Warn("webhook worker: delivery dispatch failed", "error", err, "trace_id", task.TraceID, "subscription_id", task.SubscriptionID)
		w.handleDeliveryRetry(msg, err, &task)
		return
	}

	slog.Info("webhook worker: delivered successfully", "trace_id", task.TraceID, "subscription_id", task.SubscriptionID)
	_ = msg.Ack()
}

func (w *WebhookWorker) handleDeliveryRetry(msg jetstream.Msg, err error, task *webhook.WebhookDeliveryTask) {
	numDelivered := uint64(1)
	if meta, metadataErr := msg.Metadata(); metadataErr == nil && meta != nil {
		numDelivered = meta.NumDelivered
	}

	// Check if this error is terminal or we have exhausted max retries (10)
	var httpErr *webhook.HTTPError
	isTerminalErr := false
	if errors.As(err, &httpErr) {
		// Terminal status codes: 400, 401, 403, 404
		if httpErr.StatusCode == 400 || httpErr.StatusCode == 401 || httpErr.StatusCode == 403 || httpErr.StatusCode == 404 {
			isTerminalErr = true
		}
	}
	if errors.Is(err, webhook.ErrSubscriptionNotFound) || errors.Is(err, webhook.ErrSubscriptionInactive) {
		isTerminalErr = true
	}

	if isTerminalErr || numDelivered >= 10 {
		slog.Error("webhook worker: moving delivery to DLQ", "error", err, "trace_id", task.TraceID, "attempts", numDelivered, "subscription_id", task.SubscriptionID)

		failReason := err.Error()

		// Archive permanently failed event via WebhookDispatcher
		dlqErr := w.dispatcher.WriteToDLQ(
			context.Background(),
			task.WorkspaceID,
			task.SubscriptionID,
			task.TraceID,
			task.MessageID,
			task.Event,
			task.Payload,
			int(numDelivered),
			failReason,
		)
		if dlqErr != nil {
			slog.Error("webhook worker: failed to write delivery to DLQ", "error", dlqErr, "trace_id", task.TraceID)
			_ = msg.NakWithDelay(5 * time.Second)
			return
		}

		_ = msg.Ack()
		return
	}

	// Calculate exponential backoff: 2^(numDelivered-1) * 1s
	delay := time.Duration(1<<(numDelivered-1)) * time.Second
	if delay > 10*time.Minute {
		delay = 10 * time.Minute
	}

	slog.Info("webhook worker: scheduling delivery retry", "trace_id", task.TraceID, "attempt", numDelivered, "backoff", delay)
	_ = msg.NakWithDelay(delay)
}
