# ADR-0001: Deepen Worker into DispatchOrchestrator

**Status:** Proposed  
**Date:** 2026-07-03

## Context

The outbound message worker (`internal/platform/queue/worker.go`) is a shallow module: its constructor takes 8 dependencies and its `processMessage` function spans 280 lines covering idempotency, TTL enforcement, fallback routing, channel dispatch, audit logging, and webhook events — all in a single flat function body. Testing the full dispatch pipeline requires wiring 8 real dependencies.

## Decision

Extract a **DispatchOrchestrator** module with interface `Process(ctx, DispatchMessage, *QueueMessage) error`. The orchestrator owns all three layers:

1. **Gatekeeping** — idempotency check, TTL enforcement, in-memory dedup (internal seams)
2. **Routing** — fallback loop across channels, terminal vs transient error classification
3. **Side effects** — audit writes, webhook events, queue depth decrement

### Constructor (6 params, down from 8)

```
DispatchRepo, Registry, audit.Writer, Publisher, QueueDepthTracker, config
```

### DispatchMessage port

Abstract `jetstream.Msg` behind a port with 4 methods: `Data()`, `Headers()`, `Ack()`, `NakWithDelay(time.Duration)`. Two adapters: JetStream in production, fake in tests. Two adapters justify the seam.

### Deserialization

The caller (worker loop) parses JSON; the orchestrator receives the already-parsed `QueueMessage`. The orchestrator never touches raw bytes.

### Fallback behavior

- **Success** → ack, status→sent, webhook, audit, return
- **Terminal error** → audit, advance to next channel, continue loop
- **Transient error** → update DB status, NAK with delay, return (JetStream retries)

## Test strategy

Table-driven tests with mock adapters injected at the constructor:

| Scenario | Expected |
|----------|----------|
| Already sent | Ack, no dispatch |
| TTL expired | Ack, status→failed, webhook |
| First channel succeeds | Ack, status→sent, webhook, audit |
| Terminal → fallback succeeds | Ack, status→sent on ch2 |
| All channels terminal | Ack, status→failed |
| Transient error | NakWithDelay, status→failed_transient |

## Consequences

- **Locality:** dispatch logic concentrates in one module
- **Leverage:** one interface, tests exercise idempotency + fallback + audit together
- **Testability:** table-driven tests through the orchestrator's interface — no real Postgres or NATS needed
- **Deletion test:** deleting the orchestrator spreads dispatch logic across callers
