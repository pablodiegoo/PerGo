---
spike: 018
name: multi-webhook-subscriptions
type: standard
validates: "Given a workspace with multiple webhook subscriptions, when an event occurs, then only the webhooks subscribed to that specific event type are triggered."
verdict: VALIDATED
related: [013, 014]
tags: [api, webhooks, routing]
---

# Spike 018: Multi-Webhook Subscriptions

## What This Validates
This spike validates the design of a multi-webhook routing registry allowing workspaces to subscribe separate URL endpoints to specific event types (like message receipts vs connection state updates), including wildcard match routing.

## Research
### Competing Approaches

| Approach | Tool/Library | Pros | Cons | Status |
|----------|-------------|------|------|--------|
| Single Webhook Endpoint | Current PerGo layout | Extremely simple, single DB lookup. | Force client systems to build an internal demux router to separate activities. | Rejected |
| Event-Filtered Subscriptions | Multi-webhook routing registry | Cleaner client-side integration (separate microservices/functions for separate tasks), industry standard (e.g. Stripe, Novu). | Requires mapping multiple configurations per workspace in the database. | Chosen |

## How to Run
Run the unit tests verifying the routing engine:
```bash
go test .planning/spikes/018-multi-webhook-subscriptions/subscriptions_test.go -v
```

## What to Expect
- Event dispatch checks matching keys or wildcards (`*`).
- Correct demuxing of message events and connection updates to distinct HTTP URLs.

## Investigation Trail
- **Iteration 1**: Designed the `WebhookSubscription` structural model in Go.
- **Iteration 2**: Implemented wildcard and string-prefix matching in `RouteEvent`.
- **Iteration 3**: Verified routing maps in tests.

## Results
- **Verdict**: **VALIDATED**
- **Evidence**: The prototype successfully filtered URLs for specific events and dispatched wildcard events to generic handlers cleanly.
