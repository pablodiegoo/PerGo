# Architectural Summary & Core Principles

## System Objectives & Non-Functional Envelope

PerGo is designed against a strict operational load envelope:

- **Throughput:** Sustained peak loads exceeding **500 API requests per second**.
- **Ingestion Latency:** Network-to-broker transition within **<= 50 milliseconds p99**.
- **Memory Ceiling:** Sustained memory usage under **< 512MB RAM** on standard 2 vCPU runtime environments.
- **Delivery Success:** Target **>= 99.5% delivery success** across active providers with automatic fallback.
- **Audit Traceability:** **100% trace-correlated logging** across HTTP ingress, broker queuing, channel workers, and outbound subscriber webhooks.

---

## Distributed Systems Challenges

| Challenge | Impact on Architecture | Solution in PerGo |
| :--- | :--- | :--- |
| **At-Least-Once vs. Exactly-Once** | JetStream `WorkQueuePolicy` provides at-least-once delivery. Redeliveries after worker crashes could cause duplicate messages to end users. | Outbound workers check an idempotency gate (`message_idempotency` table) by `trace_id` before invoking channel adapters. |
| **Backpressure & Queue Depths** | High burst ingestion could overwhelm down-stream providers or deplete server RAM. | The ingestion gateway tracks per-workspace pending messages in NATS. If depth reaches 1,000, it rejects new ingress with `HTTP 429 Too Many Requests` synchronously before enqueue. |
| **Ingestion Latency Budget** | 50ms p99 budget leaves ~45ms after TLS and JSON decode. Synchronous database writes would violate this SLO. | The ingress path executes only cached auth verification and NATS JetStream publish. DB persistence, media upload, and audit logging happen asynchronously. |
| **Trace-Context Propagation** | Loss of trace ID at any layer breaks auditing and end-to-end debugging. | `Trace-Id` UUID is generated at HTTP ingress, preserved in NATS message headers, propagated into worker `context.Context`, and recorded into PostgreSQL dispatches and audit rows. |
| **Connection Lifecycle & State** | WhatsApp Web uses stateful WebSockets. Container restarts or worker crashes must not lose paired devices. | Device keys and pairing state are encrypted and persisted in PostgreSQL. The in-memory connection registry can rebuild active sessions upon restart. |
| **Secret Custody** | Storing plain provider tokens and session credentials poses severe security risks. | All credentials at rest are sealed with AES-256-GCM using an envelope Key Encryption Key (`PERGO_KEK_BASE64`). |
| **Audit Contention** | Synchronous SQL inserts into an audit table under 500 req/s will degrade PostgreSQL I/O performance. | A dedicated `audit.BatchWriter` buffers audit events in memory and performs bulk inserts using `pgx.CopyFrom` over time or size thresholds. |

---

## Design Posture

- Treat **NATS JetStream** as the **single durability boundary** for outbound work. Anything acknowledged by the broker is safe across application crashes.
- Treat **PostgreSQL** as the **system of record** for identity (workspaces, API keys, channel connections, contacts, campaigns) and compliance audit logs—never as a hot-path queue.
- Keep the channel layer a **pluggable boundary** (consumer-side `Dispatcher` interface), ensuring that protocol updates in unofficial libraries (like `whatsmeow`) do not leak into core routing or domain logic.
