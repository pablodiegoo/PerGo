package typebot

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/inbound"
)

type mockPublisher struct {
	called bool
}

func (m *mockPublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	m.called = true
	return nil
}

func TestTypebotForwarder(t *testing.T) {
	t.Run("SyncInboundMessage_BotInactive", func(t *testing.T) {
		// Create Forwarder with nil repositories and mock publisher
		pub := &mockPublisher{}
		f := &Forwarder{
			publisher: pub,
		}

		contact := &domain.Contact{
			ID:        uuid.New(),
			BotActive: false,
		}

		event := &inbound.InboundEvent{
			WorkspaceID:  uuid.New(),
			ConnectionID: uuid.New(),
			Body:         "Hello",
		}

		err := f.SyncInboundMessage(context.Background(), contact, event)
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}

		if pub.called {
			t.Error("expected publisher to not be called when bot is inactive")
		}
	})
}
