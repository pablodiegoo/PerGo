# Concurrency & Performance Strategy

## Concurrency Lifecycle & Goroutine Topology

PerGo is designed to handle high concurrency while keeping total memory usage below 512MB RAM on a 2 vCPU machine. Concurrency is explicit and bounded:

| Component | Concurrency Primitive | Lifetime | Responsibility |
| :--- | :--- | :--- | :--- |
| **HTTP Ingress** | 1 goroutine per HTTP request (Echo) | Request-scoped | Authenticate, check backpressure, publish to NATS, return `202 Accepted`. |
| **Outbound Workers** | Fixed pool of pull-consumer goroutines (configurable via `PERGO_WORKER_CONCURRENCY`, default 10) | Process lifetime | Fetch from JetStream, execute idempotency checks, route to adapters. |
| **WhatsApp Sessions** | 1 goroutine per active WhatsApp Web device | Session-scoped | Maintains WebSocket connection, handles keep-alives, listens for inbound frames. |
| **Audit Batch Writer** | 1 bounded Go channel (capacity 5,000) + batch worker | Process lifetime | Consumes audit records and bulk flushes to PostgreSQL using `pgx.CopyFrom`. |
| **Webhook Workers** | Dedicated JetStream consumer pool | Process lifetime | Delivers signed outbound webhooks (`X-PerGo-Signature`) to tenant endpoints with exponential backoff. |

---

## Concurrency Patterns Applied

### 1. Consumer-Side Fan-Out
A single `POST /api/v1/messages` results in exactly **one** lightweight JetStream publish. Parallelism occurs on the consumer side: worker goroutines pull messages from the durable queue group. This allows horizontal worker scaling without increasing per-request memory pressure.

### 2. Explicit Pipeline with Bounded Buffers
```text
HTTP Request ──publish──► NATS JetStream (durable boundary)
                                │
                                ▼ (pull)
                         Worker Goroutine
                                │
                                ▼
                       Channel Dispatcher
                                │
               ┌────────────────┴────────────────┐
               ▼                                 ▼
       External Provider               Audit Channel (cap: 5000)
   (WhatsApp, WABA, Telegram)                    │
                                                 ▼
                                        Batch Flush Worker
                                                 │
                                                 ▼
                                            PostgreSQL
```

### 3. Per-Session Rate Limiting & Staggered Dispatch
Sending messages too rapidly through WhatsApp Web can lead to temporary or permanent number bans. To mitigate this:
- Each active WhatsApp connection owns a dedicated `*rate.Limiter` from `golang.org/x/time/rate`.
- Dispatches introduce a random, jittered delay of 1–3 seconds between sequential messages on the same session.
- `limiter.Wait(ctx)` yields execution back to the Go runtime scheduler, allowing other goroutines to progress concurrently without consuming CPU cycles.

### 4. Backpressure Protection (1,000 Queue Depth Ceiling)
To protect system memory under traffic spikes:
- The system inspects current pending stream messages for the requesting workspace.
- If pending messages exceed `PERGO_MAX_QUEUE_DEPTH` (default: 1,000), the request is rejected immediately with:
  ```http
  HTTP/1.1 429 Too Many Requests
  Retry-After: 5
  Content-Type: application/json

  {
    "error": "workspace message queue depth exceeded limit (1000); apply backpressure"
  }
  ```
