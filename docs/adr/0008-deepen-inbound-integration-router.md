# ADR-0008: Deepen Inbound Integration Router

**Status:** Proposed  
**Date:** 2026-07-27

## Context

The inbound integration router (`internal/inbound/router.go`) was a shallow module. Its implementation held concrete references to `ChatwootSyncer` and `TypebotForwarder` as struct fields and manually spawned unmanaged goroutines (`go func()`) using `context.Background()` with hardcoded 10-second timeouts.

This structure caused architectural friction:
1. **Low Leverage & High Modification Friction:** Adding any new integration (e.g., N8N, Webhook, Zapier) required modifying the struct definition, constructor, and adding a new hardcoded `if` dispatch block.
2. **Loss of Trace Context:** Spawning goroutines with `context.Background()` dropped the incoming HTTP/NATS trace context.
3. **No Locality for Execution Policies:** Concurrency limits, timeout policies, panic recovery, and telemetry metrics could not be applied uniformly.

## Decision

Deepen `InboundRouter` into an event-driven integration registry in `internal/inbound/router.go`.

### 1. IntegrationHandler Seam

Define a unified interface seam for all integration adapters:

```go
type IntegrationHandler interface {
	Name() string
	HandleInbound(ctx context.Context, contact *domain.Contact, ev *InboundEvent) error
}
```

### 2. Deep Router Module

`DefaultInboundRouter` maintains a contiguous slice of registered handlers (`[]IntegrationHandler`) and a configurable timeout duration:

```go
type DefaultInboundRouter struct {
	handlers []IntegrationHandler
	timeout  time.Duration
}
```

### 3. Concurrency & Context Correlation

Inbound event routing iterates through registered handlers asynchronously. Each handler execution is detached from cancellation but retains context trace values via `context.WithoutCancel(ctx)`:

```go
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
```

### 4. Adapter Bridges for Legacy Interfaces

Provide concrete adapter structs `ChatwootAdapter` and `TypebotAdapter` implementing `IntegrationHandler` to wrap existing `ChatwootSyncer` and `TypebotForwarder` implementations without breaking existing callers.

## Consequences

- **Locality:** Integration fan-out execution, trace context propagation, and error logging concentrate in the `InboundRouter` module.
- **Leverage:** Adding new integrations requires zero changes to the router module; caller code simply calls `r.Register(newHandler)`.
- **Testability:** `InboundRouter` can be unit-tested using lightweight in-memory fake handlers without initializing Chatwoot or Typebot fakes.
- **Performance:** Non-blocking fan-out maintains PerGo's < 50ms ingestion latency and 500+ msg/sec throughput.
