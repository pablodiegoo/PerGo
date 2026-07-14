---
spike: 015
name: messaging-verbs-engine
type: standard
validates: "Given an inbound message, when PerGo triggers a webhook and receives a list of JSON-serialized messaging verbs, then PerGo executes the sequence dynamically."
verdict: VALIDATED
related: [013]
tags: [api, verbs, routing]
---

# Spike 015: Messaging Verbs Engine

## What This Validates
This spike validates the design of a declarative JSON-based messaging verbs engine (inspired by Fonoster/Jambonz SIP verbs). This allows client applications to return a list of sequential actions (like replying, waiting, or forwarding) in response to inbound webhook events, which PerGo executes dynamically on their behalf.

## Research
### Competing Approaches

| Approach | Tool/Library | Pros | Cons | Status |
|----------|-------------|------|------|--------|
| Client-side Bot Daemons | WebSockets / Long-polling | Client holds the entire conversation state and triggers APIs dynamically. | Needs persistent socket connections; high client infrastructure overhead and complexity. | Rejected |
| Declarative Webhook Verbs | JSON Array response (e.g. `[reply, wait, forward]`) | Stateless client architecture. PerGo manages execution delays and dynamic forwarding. | Slightly larger payload. Requires schema validation on execution engine. | Chosen |

## How to Run
Run the unit tests verifying the verbs engine:
```bash
go test .planning/spikes/015-messaging-verbs-engine/verbs_test.go -v
```

## What to Expect
- Webhook JSON response maps to a slice of structured `Verb` commands.
- Engine executes commands sequentially, validating nested parameters (`ReplyParams`, `WaitParams`, `ForwardParams`).
- Thread-safe wait intervals block goroutine execution without freezing the scheduler.
- Context cancellation correctly aborts the execution sequence.

## Investigation Trail
- **Iteration 1**: Mocked the `Verb` interface and unmarshalling logic.
- **Iteration 2**: Added the `MockSender` to verify outbound side-effects (`reply` and `forward`).
- **Iteration 3**: Implemented context deadline checks to ensure long `wait` verbs abort gracefully.

## Results
- **Verdict**: **VALIDATED**
- **Evidence**: The engine parsed and executed a sequence of `reply` -> `wait` -> `forward` verbs, respecting time delays and context timeouts cleanly.
