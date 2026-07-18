package inbound_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/inbound"
)

type mockChatwootSyncer struct {
	called  chan struct{}
	contact *domain.Contact
	event   *inbound.InboundEvent
}

func (m *mockChatwootSyncer) SyncInboundMessage(ctx context.Context, contact *domain.Contact, ev *inbound.InboundEvent) error {
	m.contact = contact
	m.event = ev
	close(m.called)
	return nil
}

type mockTypebotForwarder struct {
	called  chan struct{}
	contact *domain.Contact
	event   *inbound.InboundEvent
}

func (m *mockTypebotForwarder) SyncInboundMessage(ctx context.Context, contact *domain.Contact, ev *inbound.InboundEvent) error {
	m.contact = contact
	m.event = ev
	close(m.called)
	return nil
}

func TestDefaultInboundRouter_Route(t *testing.T) {
	ctx := context.Background()
	wsID := uuid.New()
	contact := &domain.Contact{
		ID:          uuid.New(),
		WorkspaceID: wsID,
		Name:        "Test Recipient",
	}
	event := &inbound.InboundEvent{
		WorkspaceID: wsID,
		MessageID:   "msg-123",
		Body:        "Hello Router",
	}

	t.Run("Routes to all non-nil syncers asynchronously", func(t *testing.T) {
		cs := &mockChatwootSyncer{called: make(chan struct{})}
		tf := &mockTypebotForwarder{called: make(chan struct{})}
		router := inbound.NewDefaultRouter(cs, tf)

		err := router.Route(ctx, contact, event)
		if err != nil {
			t.Fatalf("Route failed: %v", err)
		}

		// Wait for both to be called asynchronously
		select {
		case <-cs.called:
			if cs.event.Body != "Hello Router" {
				t.Errorf("expected body 'Hello Router', got %q", cs.event.Body)
			}
		case <-time.After(1 * time.Second):
			t.Fatal("ChatwootSyncer was not called")
		}

		select {
		case <-tf.called:
			if tf.event.Body != "Hello Router" {
				t.Errorf("expected body 'Hello Router', got %q", tf.event.Body)
			}
		case <-time.After(1 * time.Second):
			t.Fatal("TypebotForwarder was not called")
		}
	})

	t.Run("Handles nil syncers gracefully", func(t *testing.T) {
		router := inbound.NewDefaultRouter(nil, nil)
		err := router.Route(ctx, contact, event)
		if err != nil {
			t.Fatalf("Route failed on nil syncers: %v", err)
		}
	})

	t.Run("Handles nil contact by bypassing routing", func(t *testing.T) {
		cs := &mockChatwootSyncer{called: make(chan struct{})}
		router := inbound.NewDefaultRouter(cs, nil)
		err := router.Route(ctx, nil, event)
		if err != nil {
			t.Fatalf("Route failed: %v", err)
		}

		select {
		case <-cs.called:
			t.Fatal("ChatwootSyncer should not be called with nil contact")
		case <-time.After(100 * time.Millisecond):
			// Success, didn't call
		}
	})
}
