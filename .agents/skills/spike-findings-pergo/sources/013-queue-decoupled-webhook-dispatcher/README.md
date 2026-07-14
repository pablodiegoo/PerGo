---
spike: 013
name: queue-decoupled-webhook-dispatcher
type: standard
validates: "Given inbound messages on whatsmeow socket listeners, when enqueued to NATS JetStream and processed asynchronously, then slow/failed CRM webhook targets do not block or lag the whatsmeow socket loop."
verdict: VALIDATED
related: [006, 007]
tags: [queue, nats, webhook]
---

# Spike 013: Queue-Decoupled Webhook Dispatcher

## What This Validates
This spike validates the asynchronous processing of webhook notifications via NATS JetStream, verifying that inbound message ingestion does not depend on target CRM availability or response times.

## Research
### Competing Approaches

| Approach | Tool/Library | Pros | Cons | Status |
|----------|-------------|------|------|--------|
| Synchronous HTTP postbacks | Standard net/http | Simple execution, zero infrastructure overhead. | Blocks the whatsmeow socket reader. Slow/down targets cause connection drops and memory leaks. | Rejected |
| In-memory Go channels | Goroutines | Fast, asynchronous, no external dependencies. | Unbuffered channel drops events on app crash/restart. No durability or automatic retry backoff. | Rejected |
| Durable Broker Delivery | NATS JetStream | At-least-once delivery, durable queueing, backpressure control, and retry DLQ. | Requires running NATS JetStream server. | Chosen |

## How to Run
Run the webhook dispatcher tests to verify dispatch decoupling and retries:
```bash
go test ./internal/webhook -run TestDefaultDispatcher -v
```

## What to Expect
- Inbound events are immediately acknowledged and dispatched to a durable NATS stream.
- Webhook consumer processes events asynchronously with an independent lifecycle.
- Failures do not propagate back to whatsmeow or the HTTP gateway, but route to the DLQ instead.

## Investigation Trail
- **Iteration 1**: Set up NATS JetStream streams for `webhooks.events` in `03-02-PLAN.md`.
- **Iteration 2**: Created `DefaultDispatcher` in `internal/webhook/dispatcher.go` to handle PII compliance redaction and signature signing.
- **Iteration 3**: Wired DLQ insertions on permanent HTTP dispatch failures in `008_create_webhooks_and_dlq.sql`.

## Results
- **Verdict**: **VALIDATED**
- **Evidence**: Asynchronous workers successfully dequeue NATS messages and execute HTTP postbacks independently, isolating the socket loop from CRM downtime.
