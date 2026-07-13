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
	nc              *nats.Conn
	js              jetstream.JetStream
	consumer        jetstream.Consumer
	inboundConsumer jetstream.Consumer
	dispatcher      webhook.WebhookDispatcher
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	done            chan struct{}
}

func NewWebhookWorker(ctx context.Context, nc *nats.Conn, dispatcher webhook.WebhookDispatcher) (*WebhookWorker, error) {
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
	inboundStream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      "INBOUND",
		Subjects:  []string{"inbound.events.>"},
		Retention: jetstream.LimitsPolicy,
		MaxMsgs:   10000,
		Storage:   jetstream.FileStorage,
		MaxAge:    7 * 24 * time.Hour,
	})
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

	ctx, cancel := context.WithCancel(ctx)
	w := &WebhookWorker{
		nc:              nc,
		js:              js,
		consumer:        consumer,
		inboundConsumer: inboundConsumer,
		dispatcher:      dispatcher,
		cancel:          cancel,
		done:            make(chan struct{}),
	}

	w.wg.Add(2)
	go w.run(ctx, w.consumer, "outbound")
	go w.run(ctx, w.inboundConsumer, "inbound")
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
				w.processEvent(ctx, msg, mode)
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

	// Delegate compliance, signing, and HTTP dispatch to the WebhookDispatcher
	err := w.dispatcher.Dispatch(ctx, mode, msg.Data())
	if err != nil {
		slog.Warn("webhook worker: dispatch failed", "error", err, "trace_id", evt.TraceID)
		w.handleRetry(msg, err, &evt)
		return
	}

	slog.Info("webhook worker: dispatched successfully", "trace_id", evt.TraceID, "event", evt.Event)
	_ = msg.Ack()
}

func (w *WebhookWorker) handleRetry(msg jetstream.Msg, err error, evt *WebhookEvent) {
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

	if isTerminalErr || numDelivered >= 10 {
		slog.Error("webhook worker: moving webhook to DLQ", "error", err, "trace_id", evt.TraceID, "attempts", numDelivered)

		wsID, _ := uuid.Parse(evt.WorkspaceID)
		failReason := err.Error()

		// Archive permanently failed event via WebhookDispatcher
		dlqErr := w.dispatcher.WriteToDLQ(
			context.Background(),
			wsID,
			evt.TraceID,
			evt.MessageID,
			evt.Event,
			msg.Data(),
			int(numDelivered),
			failReason,
		)
		if dlqErr != nil {
			slog.Error("webhook worker: failed to write to DLQ", "error", dlqErr, "trace_id", evt.TraceID)
			// If we fail to write to the DLQ DB, Nak with short delay so we don't lose the event!
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

	slog.Info("webhook worker: scheduling retry", "trace_id", evt.TraceID, "attempt", numDelivered, "backoff", delay)
	_ = msg.NakWithDelay(delay)
}
