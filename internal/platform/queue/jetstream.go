// Package queue provides JetStream-backed durable message queue primitives.
package queue

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// StreamName is the JetStream stream that holds outbound messages.
	StreamName = "MESSAGES"

	// StreamSubject is the wildcard subject the stream listens on.
	StreamSubject = "messages.>"

	// MaxQueueDepth is the per-stream message limit that triggers backpressure.
	MaxQueueDepth = 1000
)

// EnsureStream creates or updates a WorkQueuePolicy stream named "MESSAGES".
// The stream persists to file storage and discards new messages when full.
// Safe to call multiple times — CreateOrUpdateStream is idempotent.
func EnsureStream(ctx context.Context, nc *nats.Conn) (jetstream.Stream, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("jetstream.New: %w", err)
	}

	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      StreamName,
		Subjects:  []string{StreamSubject},
		Retention: jetstream.WorkQueuePolicy,
		MaxMsgs:   MaxQueueDepth,
		Discard:   jetstream.DiscardNew,
		Storage:   jetstream.FileStorage,
		MaxAge:    24 * time.Hour,
	})
	if err != nil {
		return nil, fmt.Errorf("create stream %s: %w", StreamName, err)
	}

	slog.Info("jetstream stream ready", "stream", StreamName)
	return stream, nil
}

// EnsureWebhookStream creates or updates a LimitsPolicy stream named "WEBHOOKS".
// Safe to call multiple times.
func EnsureWebhookStream(ctx context.Context, nc *nats.Conn) (jetstream.Stream, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("jetstream.New: %w", err)
	}

	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      "WEBHOOKS",
		Subjects:  []string{"webhooks.events"},
		Retention: jetstream.LimitsPolicy,
		MaxMsgs:   10000,
		Storage:   jetstream.FileStorage,
		MaxAge:    7 * 24 * time.Hour,
	})
	if err != nil {
		return nil, fmt.Errorf("create stream WEBHOOKS: %w", err)
	}

	slog.Info("jetstream webhook stream ready", "stream", "WEBHOOKS")
	return stream, nil
}

// EnsureWebhookDeliveryStream creates or updates the WEBHOOK_DELIVERIES stream.
func EnsureWebhookDeliveryStream(ctx context.Context, nc *nats.Conn) (jetstream.Stream, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("jetstream.New: %w", err)
	}

	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      "WEBHOOK_DELIVERIES",
		Subjects:  []string{"webhooks.deliveries.>"},
		Retention: jetstream.WorkQueuePolicy,
		MaxMsgs:   10000,
		Discard:   jetstream.DiscardNew,
		Storage:   jetstream.FileStorage,
		MaxAge:    24 * time.Hour,
	})
	if err != nil {
		return nil, fmt.Errorf("create stream WEBHOOK_DELIVERIES: %w", err)
	}

	slog.Info("jetstream webhook deliveries stream ready", "stream", "WEBHOOK_DELIVERIES")
	return stream, nil
}

// EnsureInboundStream creates or updates a LimitsPolicy stream named "INBOUND".
func EnsureInboundStream(ctx context.Context, nc *nats.Conn) (jetstream.Stream, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("jetstream.New: %w", err)
	}

	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      "INBOUND",
		Subjects:  []string{"inbound.events.>"},
		Retention: jetstream.LimitsPolicy,
		MaxMsgs:   10000,
		Storage:   jetstream.FileStorage,
		MaxAge:    7 * 24 * time.Hour,
	})
	if err != nil {
		return nil, fmt.Errorf("create stream INBOUND: %w", err)
	}

	slog.Info("jetstream inbound stream ready", "stream", "INBOUND")
	return stream, nil
}


// EnsureConsumer creates or gets a durable pull consumer on the given stream.
// Safe to call multiple times — CreateConsumer is idempotent when the config matches.
func EnsureConsumer(ctx context.Context, stream jetstream.Stream, consumerName string) (jetstream.Consumer, error) {
	// For WorkQueuePolicy stream, delete any other consumer to prevent "filtered consumer not unique" errors.
	// Since WorkQueue only allows one consumer (or non-overlapping), having any old/stale/test consumers
	// will block the creation of the target consumer.
	if stream.CachedInfo().Config.Retention == jetstream.WorkQueuePolicy {
		lister := stream.ConsumerNames(ctx)
		for name := range lister.Name() {
			if name != consumerName {
				slog.Info("deleting stale consumer from WorkQueue stream", "stream", stream.CachedInfo().Config.Name, "consumer", name)
				_ = stream.DeleteConsumer(ctx, name)
			}
		}
	}

	// Try to get existing consumer first
	cons, err := stream.Consumer(ctx, consumerName)
	if err == nil {
		slog.Info("jetstream consumer found", "consumer", consumerName)
		return cons, nil
	}

	cfg := jetstream.ConsumerConfig{
		Durable:       consumerName,
		FilterSubject: StreamSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    5,
		MaxAckPending: 100,
	}

	cons, err = createConsumerWithRetry(ctx, stream, cfg)
	if err != nil {
		return nil, fmt.Errorf("create consumer %s: %w", consumerName, err)
	}
	slog.Info("jetstream consumer ready", "consumer", consumerName)
	return cons, nil
}

func createConsumerWithRetry(ctx context.Context, stream jetstream.Stream, cfg jetstream.ConsumerConfig) (jetstream.Consumer, error) {
	var cons jetstream.Consumer
	var err error
	for i := 0; i < 3; i++ {
		cons, err = stream.CreateOrUpdateConsumer(ctx, cfg)
		if err == nil {
			return cons, nil
		}
		slog.Warn("failed to create/update consumer, retrying after delete", "consumer", cfg.Durable, "attempt", i+1, "error", err)
		_ = stream.DeleteConsumer(ctx, cfg.Durable)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return nil, err
}

// JetStreamPublisher publishes messages to the JetStream "messages.outbound"
// subject. Each publish carries a Nats-Msg-Id header set to the caller's
// trace_id for publish-side idempotency (dedup).
type JetStreamPublisher struct {
	js jetstream.JetStream
}

// NewJetStreamPublisher wraps a JetStream instance for publishing.
func NewJetStreamPublisher(nc *nats.Conn) *JetStreamPublisher {
	js, err := jetstream.New(nc)
	if err != nil {
		// This should never fail if nc is connected and JetStream-enabled.
		slog.Error("failed to create jetstream instance for publisher", "error", err)
	}
	return &JetStreamPublisher{js: js}
}

// Publish sends data to the given subject with a Nats-Msg-Id header set to
// traceID for dedup. Returns an error if the stream is full (DiscardNew) or
// the connection is broken.
func (p *JetStreamPublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	msg := nats.NewMsg(subject)
	msg.Data = data
	if traceID != "" {
		msg.Header.Set("Nats-Msg-Id", traceID)
	}

	_, err := p.js.PublishMsg(ctx, msg)
	if err != nil {
		return fmt.Errorf("publish to %s: %w", subject, err)
	}
	return nil
}

// EnsureCampaignStream creates or updates a WorkQueuePolicy stream named "CAMPAIGNS".
func EnsureCampaignStream(ctx context.Context, nc *nats.Conn) (jetstream.Stream, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("jetstream.New: %w", err)
	}

	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      "CAMPAIGNS",
		Subjects:  []string{"campaigns.>"},
		Retention: jetstream.WorkQueuePolicy,
		MaxMsgs:   MaxQueueDepth,
		Discard:   jetstream.DiscardNew,
		Storage:   jetstream.FileStorage,
		MaxAge:    24 * time.Hour,
	})
	if err != nil {
		return nil, fmt.Errorf("create stream CAMPAIGNS: %w", err)
	}

	slog.Info("jetstream campaign stream ready", "stream", "CAMPAIGNS")
	return stream, nil
}

// EnsureCampaignConsumer creates or gets a durable pull consumer with MaxAckPending = 1 on the CAMPAIGNS stream.
func EnsureCampaignConsumer(ctx context.Context, stream jetstream.Stream, consumerName string) (jetstream.Consumer, error) {
	if stream.CachedInfo().Config.Retention == jetstream.WorkQueuePolicy {
		lister := stream.ConsumerNames(ctx)
		for name := range lister.Name() {
			if name != consumerName {
				slog.Info("deleting stale consumer from campaigns WorkQueue stream", "stream", stream.CachedInfo().Config.Name, "consumer", name)
				_ = stream.DeleteConsumer(ctx, name)
			}
		}
	}

	cons, err := stream.Consumer(ctx, consumerName)
	if err == nil {
		slog.Info("jetstream campaigns consumer found", "consumer", consumerName)
		return cons, nil
	}

	cfg := jetstream.ConsumerConfig{
		Durable:       consumerName,
		FilterSubject: "campaigns.>",
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    5,
		MaxAckPending: 1, // guarantee sequential batch delivery across replicas
	}

	cons, err = createConsumerWithRetry(ctx, stream, cfg)
	if err != nil {
		return nil, fmt.Errorf("create campaigns consumer %s: %w", consumerName, err)
	}
	slog.Info("jetstream campaigns consumer ready", "consumer", consumerName)
	return cons, nil
}

