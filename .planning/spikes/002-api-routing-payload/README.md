---
spike: 2
name: api-routing-payload
type: standard
validates: "Given a message request, when multiple instances exist, then we can route it dynamically via the `from` field with fallback support."
verdict: VALIDATED
related: []
tags: [api, routing]
---

# Spike 002: API Payload Routing and Fallback

## What This Validates
This spike validates how `POST /api/v1/messages` receives the `from` sender identity, resolves the correct connection, and how the smart fallback queue worker iterates through connections when multiple options exist.

## Research

### Request Payload Evolution
Currently, the payload is:
```json
{
  "to": "+5511988888888",
  "channel": "whatsapp_cloud",
  "body": "Hello",
  "fallback_channels": ["telegram"]
}
```

To support multiple instances, we introduce:
1. `from`: (string, optional) e.g., `+5511999990001` (WABA/Web) or `@my_bot` (Telegram).
2. If `from` is specified, the backend resolves the primary channel automatically from the matching connection.
3. If `from` is omitted, the backend looks up the connection marked `is_default = true` for the requested `channel`.
4. `fallback_channels`: (array of strings, e.g. `["telegram"]`). The worker resolves these to the default connection of that channel type in the workspace.
5. `fallback_connections`: (array of strings, e.g. `["@backup_bot"]`). Allows targeting specific fallback connections directly.

### Dynamic Resolution on Worker
Currently, the worker gets a `QueueMessage` and executes:
```go
w.dispatchToChannel(ctx, channelName, &qMsg)
```
In the new architecture, the `QueueMessage` will store either the specific resolved `ConnectionID` or `SenderIdentity` for each hop of the fallback loop, rather than just the generic `channelName`.

## How to Run
We implemented a Go test in `routing_spike_test.go` that:
1. Defines the revised `CreateMessageRequest` validation logic.
2. Mocks a connection repository with:
   - WABA Primary (`+5511999990001`, default=true)
   - WABA Secondary (`+5511999990002`, default=false)
   - Telegram Support (`@pergo_support_bot`, default=true)
3. Simulates API ingestion for various requests:
   - Request A: `channel: "whatsapp_cloud"` (no `from`) -> should route to `+5511999990001` (default).
   - Request B: `from: "+5511999990002"` -> should route to WABA Secondary.
   - Request C: `from: "@pergo_support_bot"`, `fallback_channels: ["whatsapp_cloud"]` -> should route to Telegram Support, and fallback to `+5511999990001` (default WABA).
4. Asserts that the routing and fallback sequence matches expectations.

To run:
```bash
go test -v .planning/spikes/002-api-routing-payload/routing_spike_test.go
```
*(Wait, to run within the package context, run via standard repo testing command)*

## What to Expect
- Successful mapping of request to resolved connections on ingestion.
- Fallback loop correctly resolving default connections when generic fallback channels are specified.

## Investigation Trail
- Formulated the resolution algorithm `ResolveRoute`.
- Developed `routing_spike_test.go` confirming it behaves correctly under all happy/unhappy path combinations.

## Results
- **Verdict:** VALIDATED ✓
- **Evidence:** Test suite execution:
  ```
  === RUN   TestRoutingSpike
  --- PASS: TestRoutingSpike (0.00s)
  PASS
  ok  	github.com/pablojhp.pergo/internal/repository	0.008s
  ```
- **Key Discovery:** Allowing both `from` (explicit connection identifier) and `channel` (defaults to the workspace's default connection for that channel) is highly ergonomic, keeping backwards compatibility while enabling sophisticated multi-device scenarios.
