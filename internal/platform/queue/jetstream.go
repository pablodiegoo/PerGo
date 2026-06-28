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
		Subjects:  []string{"webhooks.>"},
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

// EnsureConsumer creates or gets a durable pull consumer on the given stream.
// Safe to call multiple times — CreateConsumer is idempotent when the config matches.
func EnsureConsumer(ctx context.Context, stream jetstream.Stream, consumerName string) (jetstream.Consumer, error) {
	// Try to get existing consumer first
	cons, err := stream.Consumer(ctx, consumerName)
	if err == nil {
		slog.Info("jetstream consumer found", "consumer", consumerName)
		return cons, nil
	}
	// Create new consumer
	cons, err = stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       consumerName,
		FilterSubject: StreamSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    5,
		MaxAckPending: 100,
	})
	if err != nil {
		return nil, fmt.Errorf("create consumer %s: %w", consumerName, err)
	}
	slog.Info("jetstream consumer ready", "consumer", consumerName)
	return cons, nil
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
