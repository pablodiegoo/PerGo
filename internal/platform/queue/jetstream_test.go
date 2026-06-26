package queue

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// connectNATS dials a local NATS server. Returns nil if unavailable.
func connectNATS(t *testing.T) *nats.Conn {
	t.Helper()
	nc, err := nats.Connect(nats.DefaultURL,
		nats.Timeout(2*time.Second),
	)
	if err != nil {
		t.Skipf("NATS not available at %s: %v", nats.DefaultURL, err)
	}
	t.Cleanup(func() { nc.Close() })
	return nc
}

func TestEnsureStream(t *testing.T) {
	nc := connectNATS(t)
	ctx := context.Background()

	stream, err := EnsureStream(ctx, nc)
	if err != nil {
		t.Fatalf("EnsureStream failed: %v", err)
	}

	info, err := stream.Info(ctx)
	if err != nil {
		t.Fatalf("stream.Info failed: %v", err)
	}

	if info.Config.Name != StreamName {
		t.Errorf("stream name = %q, want %q", info.Config.Name, StreamName)
	}
	if info.Config.Retention != jetstream.WorkQueuePolicy {
		t.Errorf("retention = %v, want WorkQueuePolicy", info.Config.Retention)
	}
	if info.Config.MaxMsgs != MaxQueueDepth {
		t.Errorf("MaxMsgs = %d, want %d", info.Config.MaxMsgs, MaxQueueDepth)
	}
	if info.Config.Discard != jetstream.DiscardNew {
		t.Errorf("Discard = %v, want DiscardNew", info.Config.Discard)
	}
}

func TestEnsureStreamIdempotent(t *testing.T) {
	nc := connectNATS(t)
	ctx := context.Background()

	_, err := EnsureStream(ctx, nc)
	if err != nil {
		t.Fatalf("first EnsureStream failed: %v", err)
	}

	// Second call should succeed without error
	_, err = EnsureStream(ctx, nc)
	if err != nil {
		t.Fatalf("second EnsureStream failed (should be idempotent): %v", err)
	}
}

func TestPublishAndConsume(t *testing.T) {
	nc := connectNATS(t)
	ctx := context.Background()

	// Clean slate: delete and recreate the stream
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream.New failed: %v", err)
	}
	_ = js.DeleteStream(ctx, StreamName)

	stream, err := EnsureStream(ctx, nc)
	if err != nil {
		t.Fatalf("EnsureStream failed: %v", err)
	}

	// Create a consumer to read messages
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:   "test-consumer",
		AckPolicy: jetstream.AckExplicitPolicy,
	})
	if err != nil {
		t.Fatalf("CreateOrUpdateConsumer failed: %v", err)
	}

	// Publish a message
	publisher := NewJetStreamPublisher(nc)
	payload := []byte(`{"to":"5511999999999","channel":"whatsapp","body":"hello"}`)
	traceID := "test-trace-001"

	err = publisher.Publish(ctx, "messages.outbound", payload, traceID)
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	// Consume the message
	msgCtx, err := consumer.Messages()
	if err != nil {
		t.Fatalf("consumer.Messages failed: %v", err)
	}
	defer msgCtx.Stop()

	msg, err := msgCtx.Next()
	if err != nil {
		t.Fatalf("msgCtx.Next failed: %v", err)
	}

	if string(msg.Data()) != string(payload) {
		t.Errorf("message data = %q, want %q", string(msg.Data()), string(payload))
	}

	// Verify trace ID in headers
	headers := msg.Headers()
	if headers != nil {
		gotTrace := headers.Get("Nats-Msg-Id")
		if gotTrace != traceID {
			t.Errorf("Nats-Msg-Id header = %q, want %q", gotTrace, traceID)
		}
	}

	if err := msg.Ack(); err != nil {
		t.Fatalf("msg.Ack failed: %v", err)
	}
}

func TestPublishDedup(t *testing.T) {
	nc := connectNATS(t)
	ctx := context.Background()

	// Delete and recreate the stream to get a clean state (WorkQueuePolicy
	// only allows one non-filtered consumer).
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream.New failed: %v", err)
	}
	_ = js.DeleteStream(ctx, StreamName) // ignore error if not exists

	stream, err := EnsureStream(ctx, nc)
	if err != nil {
		t.Fatalf("EnsureStream failed: %v", err)
	}

	publisher := NewJetStreamPublisher(nc)
	payload := []byte(`{"to":"5511999999999","channel":"whatsapp","body":"dedup test"}`)
	traceID := "dedup-trace-001"

	// Publish twice with same trace ID — Dedup via Nats-Msg-Id header
	err = publisher.Publish(ctx, "messages.outbound", payload, traceID)
	if err != nil {
		t.Fatalf("first Publish failed: %v", err)
	}
	err = publisher.Publish(ctx, "messages.outbound", payload, traceID)
	if err != nil {
		t.Fatalf("second Publish failed: %v", err)
	}

	// Create a consumer and check only one message arrives
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:   "test-dedup-consumer",
		AckPolicy: jetstream.AckExplicitPolicy,
	})
	if err != nil {
		t.Fatalf("CreateOrUpdateConsumer failed: %v", err)
	}

	msgCtx, err := consumer.Messages()
	if err != nil {
		t.Fatalf("consumer.Messages failed: %v", err)
	}
	defer msgCtx.Stop()

	// Read first message
	msg, err := msgCtx.Next()
	if err != nil {
		t.Fatalf("msgCtx.Next failed: %v", err)
	}
	if err := msg.Ack(); err != nil {
		t.Fatalf("msg.Ack failed: %v", err)
	}

	// Second message should not arrive (dedup)
	// Use a short timeout to confirm no second message
	_, err = msgCtx.Next(jetstream.NextMaxWait(500 * time.Millisecond))
	if err == nil {
		t.Error("expected no second message (dedup), but got one")
	}
}
