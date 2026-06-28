package session_test

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/pablojhp.pergo/internal/platform/queue"
	"github.com/pablojhp.pergo/internal/session"
)

func connectNATS(t *testing.T) *nats.Conn {
	t.Helper()
	nc, err := nats.Connect(nats.DefaultURL, nats.Timeout(2*time.Second))
	if err != nil {
		t.Skipf("NATS not available: %v", err)
	}
	t.Cleanup(func() { nc.Close() })
	return nc
}

func TestWhatsAppInbound(t *testing.T) {
	nc := connectNATS(t)
	publisher := queue.NewJetStreamPublisher(nc)

	// Setup NATS Stream
	ctx := context.Background()
	js, err := jetstream.New(nc)
	if err == nil {
		_ = js.DeleteStream(ctx, "INBOUND")
	}
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     "INBOUND",
		Subjects: []string{"inbound.events.>"},
	})
	if err != nil {
		t.Fatalf("failed to create NATS stream: %v", err)
	}

	m := session.NewManager(
		nil,
		nil,
		nil,
		nil,
		"2.3000.1025000000",
		nil,
		nil,
		nil,
		publisher,
		nil,
		nil,
	)

	if m == nil {
		t.Fatal("expected manager")
	}
}
