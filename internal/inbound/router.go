package inbound

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/pablojhp.pergo/internal/domain"
)

// IntegrationHandler defines the seam for external integration adapters (e.g. Chatwoot, Typebot, N8N).
type IntegrationHandler interface {
	Name() string
	HandleInbound(ctx context.Context, contact *domain.Contact, ev *InboundEvent) error
}

// chatwootAdapter wraps ChatwootSyncer to satisfy IntegrationHandler.
type chatwootAdapter struct {
	syncer ChatwootSyncer
}

func (a *chatwootAdapter) Name() string { return "chatwoot" }
func (a *chatwootAdapter) HandleInbound(ctx context.Context, contact *domain.Contact, ev *InboundEvent) error {
	if a.syncer == nil {
		return nil
	}
	return a.syncer.SyncInboundMessage(ctx, contact, ev)
}

// NewChatwootAdapter creates an IntegrationHandler from a ChatwootSyncer.
func NewChatwootAdapter(cs ChatwootSyncer) IntegrationHandler {
	if cs == nil {
		return nil
	}
	return &chatwootAdapter{syncer: cs}
}

// typebotAdapter wraps TypebotForwarder to satisfy IntegrationHandler.
type typebotAdapter struct {
	forwarder TypebotForwarder
}

func (a *typebotAdapter) Name() string { return "typebot" }
func (a *typebotAdapter) HandleInbound(ctx context.Context, contact *domain.Contact, ev *InboundEvent) error {
	if a.forwarder == nil {
		return nil
	}
	return a.forwarder.SyncInboundMessage(ctx, contact, ev)
}

// NewTypebotAdapter creates an IntegrationHandler from a TypebotForwarder.
func NewTypebotAdapter(tf TypebotForwarder) IntegrationHandler {
	if tf == nil {
		return nil
	}
	return &typebotAdapter{forwarder: tf}
}

// DefaultInboundRouter implements the InboundRouter interface as a deep integration fanout registry.
type DefaultInboundRouter struct {
	handlers []IntegrationHandler
	timeout  time.Duration
}

// NewDefaultRouter creates a new DefaultInboundRouter instance.
// Accepts any combination of IntegrationHandler, ChatwootSyncer, or TypebotForwarder instances.
func NewDefaultRouter(items ...any) *DefaultInboundRouter {
	r := &DefaultInboundRouter{
		handlers: make([]IntegrationHandler, 0, len(items)),
		timeout:  10 * time.Second,
	}
	for _, item := range items {
		r.RegisterAny(item)
	}
	return r
}

// Register attaches an IntegrationHandler to the router.
func (r *DefaultInboundRouter) Register(h IntegrationHandler) {
	if h != nil {
		r.handlers = append(r.handlers, h)
	}
}

// RegisterAny registers an IntegrationHandler, ChatwootSyncer, or TypebotForwarder.
func (r *DefaultInboundRouter) RegisterAny(item any) {
	if item == nil {
		return
	}
	if handler, ok := item.(IntegrationHandler); ok {
		r.Register(handler)
		return
	}
	if syncer, ok := item.(ChatwootSyncer); ok {
		typeName := fmt.Sprintf("%T", item)
		if strings.Contains(strings.ToLower(typeName), "typebot") {
			r.Register(NewTypebotAdapter(syncer))
		} else {
			r.Register(NewChatwootAdapter(syncer))
		}
		return
	}
	slog.Error("inbound router: unknown handler type", "type", fmt.Sprintf("%T", item))
}

// Route routes the inbound event to registered external integration handlers concurrently in background goroutines.
func (r *DefaultInboundRouter) Route(ctx context.Context, contact *domain.Contact, ev *InboundEvent) error {
	if contact == nil || len(r.handlers) == 0 {
		return nil
	}

	for _, h := range r.handlers {
		go func(handler IntegrationHandler) {
			ctxBg, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.timeout)
			defer cancel()
			if err := handler.HandleInbound(ctxBg, contact, ev); err != nil {
				slog.Error("inbound router: handler error",
					"handler", handler.Name(),
					"error", err,
					"contact_id", contact.ID,
					"workspace_id", ev.WorkspaceID,
				)
			}
		}(h)
	}

	return nil
}
