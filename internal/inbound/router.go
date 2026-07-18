package inbound

import (
	"context"
	"log/slog"
	"time"

	"github.com/pablojhp.pergo/internal/domain"
)

// DefaultInboundRouter implements the InboundRouter interface.
type DefaultInboundRouter struct {
	chatwootSyncer   ChatwootSyncer
	typebotForwarder TypebotForwarder
}

// NewDefaultRouter creates a new DefaultInboundRouter instance.
func NewDefaultRouter(cs ChatwootSyncer, tf TypebotForwarder) *DefaultInboundRouter {
	return &DefaultInboundRouter{
		chatwootSyncer:   cs,
		typebotForwarder: tf,
	}
}

// Route routes the inbound event to the configured external syncers in background goroutines.
func (r *DefaultInboundRouter) Route(ctx context.Context, contact *domain.Contact, ev *InboundEvent) error {
	if contact == nil {
		return nil
	}

	if r.chatwootSyncer != nil {
		go func(c *domain.Contact, e *InboundEvent) {
			ctxBg, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := r.chatwootSyncer.SyncInboundMessage(ctxBg, c, e); err != nil {
				slog.Error("inbound router: failed to sync message to chatwoot", "error", err, "contact_id", c.ID, "workspace_id", e.WorkspaceID)
			}
		}(contact, ev)
	}

	if r.typebotForwarder != nil {
		go func(c *domain.Contact, e *InboundEvent) {
			ctxBg, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := r.typebotForwarder.SyncInboundMessage(ctxBg, c, e); err != nil {
				slog.Error("inbound router: failed to sync message to typebot", "error", err, "contact_id", c.ID, "workspace_id", e.WorkspaceID)
			}
		}(contact, ev)
	}

	return nil
}
