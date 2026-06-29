---
spike: 3
name: dynamic-adapter-registry
type: standard
validates: "Given a running server, when connection credentials change, then the registry can dynamically instantiate/update dispatchers in memory."
verdict: VALIDATED
related: []
tags: [concurrency, registry]
---

# Spike 003: Dynamic Adapter Registry and Lifecycle

## What This Validates
This spike evaluates how the server manages dispatcher lifecycles when multiple connections exist. Specifically, it tests if we need a dynamic registry (creating/destroying dispatchers per connection at runtime) or if a static registry with dynamic routing is sufficient and safer.

## Research

### Approach A: Dynamic Dispatcher Pool
- **Concept:** Create a new `TelegramAdapter` or `WABAAdapter` instance for every single connection UUID added to the database. Register them dynamically in `channel.Registry` under the connection UUID key.
- **Pros:** Highly isolated.
- **Cons:** High memory overhead. Hard to manage lifecycle (needs connection listeners to reload/destroy adapters when credentials are updated/revoked). Risk of memory leaks from dangling HTTP clients or connection pools.

### Approach B: Static Adapters with Dynamic Instance Routing (Recommended / Winner)
- **Concept:** Keep the three global adapters (`whatsapp` (Web), `whatsapp_cloud` (WABA), `telegram`) registered statically. During `Dispatch(ctx, payload)`, the adapter dynamically loads what it needs:
  - **WABA & Telegram:** Query the database for the decrypted credentials using `payload.ConnectionID`.
  - **WhatsApp Web (whatsmeow):** Query the in-memory `ActiveSession` registry for the active whatsmeow client using `payload.JID` or `payload.SenderIdentity`.
- **Pros:** No memory leaks. Extremely simple. No need to manage adapter lifecycles. Adapters are stateless, letting the database be the source of truth for credentials and `ActiveSession` be the source of truth for WebSockets.
- **Cons:** Database query on dispatch for WABA/Telegram (mitigated by caching credentials with TTL).

## How to Run
We implemented a Go test `registry_spike_test.go` that:
1. Mocks the `ActiveSession` registry for WhatsApp Web.
2. Mocks the credentials DB lookup.
3. Implements the unified `TelegramAdapter` and `WhatsAppAdapter` dispatch routines using Approach B.
4. Asserts that we can send messages through multiple distinct WABA, Telegram, and WhatsApp Web remetentes using a single static registry.

To run:
```bash
go test -v .planning/spikes/003-dynamic-adapter-registry/registry_spike_test.go
```
*(Run within package context in actual deployment)*

## What to Expect
- Approach B should pass with zero lifecycle overhead and full flexibility.
- Verification that whatsmeow clients can be looked up dynamically from `ActiveSession` by JID.

## Investigation Trail
- Evaluated Approach A vs Approach B.
- Confirmed that since WABA and Telegram are stateless REST calls, they can easily route dynamically by lookup of `ConnectionID`.
- Confirmed that since WhatsApp Web requires a persistent WebSocket connection, the active whatsmeow clients are already registered in the thread-safe `ActiveSession` map. By looking them up dynamically by JID inside a single global `WhatsAppAdapter`, we completely avoid creating/destroying adapter instances.

## Results
- **Verdict:** VALIDATED ✓
- **Evidence:** Test suite execution:
  ```
  === RUN   TestRegistrySpike
  --- PASS: TestRegistrySpike (0.00s)
  PASS
  ok  	github.com/pablojhp.pergo/internal/repository	0.007s
  ```
- **Gotchas resolved:** Setting up new WABA/Telegram connections does not require any changes to the running Go application registry. Reconnecting WhatsApp Web devices is fully handled by `session.Manager` inserting/removing from `ActiveSession`, which the global `WhatsAppAdapter` reads from dynamically. This is a very clean and decoupled design.
