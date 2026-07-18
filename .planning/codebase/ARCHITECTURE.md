---
last_mapped_commit: 99448836f14aa64e923a366b95721858185d878b
last_mapped_date: 2026-07-18
---

# Architecture Map

This document describes the architectural design, architectural patterns, abstractions, entry points, and data flow of PerGo.

## 1. Architectural Pattern

PerGo is designed as a layered system with clear dependency boundaries:

```
┌────────────────────────────────────────────────────────┐
│                        Entry Points                    │
│      (Public API, Webhook Handlers, Admin console UI)   │
└───────────────────────────┬────────────────────────────┘
                            ▼
┌────────────────────────────────────────────────────────┐
│                        Bus Layer                       │
│              (NATS JetStream event broker)             │
└───────────────────────────┬────────────────────────────┘
                            ▼
┌────────────────────────────────────────────────────────┐
│                       Domain Logic                     │
│    (InboundProcessor, Dispatcher, Session Manager)     │
└───────────────────────────┬────────────────────────────┘
                            ▼
┌────────────────────────────────────────────────────────┐
│                Data Storage & Provider Adapters        │
│    (Repositories, S3 storage, Channel Adapters)        │
└────────────────────────────────────────────────────────┘
```

The design guarantees data sovereignty and self-hosting efficiency:
- Business entities live in `internal/domain/`.
- Entry points interact with business domains and queues without direct knowledge of physical providers.
- Infrastructure and storage details live under `internal/platform/` and `internal/repository/`.

## 2. Key Architectural Components

### A. Message Ingestion & Routing (Outbound)

1. **Public API Entry Point**: Public HTTP handlers bind, validate, and check per-session queue backpressure (`internal/api/handler/message.go`).
2. **Broker Enqueue**: Messages are serialized to `domain.QueueMessage` and published to NATS JetStream `messages.outbound` subject.
3. **Queue Consumers**: Workers (`internal/platform/queue/worker.go`) consume messages sequentially.
4. **Channel Dispatcher**: The Dispatcher resolves the appropriate channel adapter (Whatsmeow, Telegram, WABA) from the `Registry` and invokes the send function.

### B. Event Intake & Processing (Inbound)

1. **Intake Event Listeners**:
   - Webhook endpoints for WABA and Telegram (`internal/api/handler/`).
   - whatsmeow client event dispatch loops (`internal/session/inbound_processor.go`).
2. **Inbound Enrichment & Deduplication**:
   - `InboundProcessor` handles deduplication (`inbound_dedup` table check), extracts message body, downloads and uploads media attachment payloads to S3, and captures recipient identities.
   - Updates target session's unread counter and active state in `recipient_sessions`.
3. **Inbound Router (`internal/inbound/router.go`)**:
   - Acts as a clean interface seam, decoupling the core ingestion flow from external sync implementations (Chatwoot, Typebot).
   - Offloads external synchronization tasks asynchronously in separate background goroutines with isolated context timeouts (10s).
4. **Auditing & Webhook Publishing**:
   - Writes an event to `audit_logs`.
   - Publishes an event to NATS JetStream `webhooks.inbound` subject for forwarding.

### C. Conversational Dashboard (Inbox UI)

1. **Admin Portal Handler**: `internal/api/handler/admin/inbox.go` manages console interactions.
2. **Query Optimization**: Computes active conversation lists using optimized grouping queries on `audit_logs` and matches read indicators against the `last_read_at` value in `recipient_sessions`.
3. **Chronological Thread Stitching**: Performs a `UNION` query in `AuditRepository.ListThread` to combine inbound and outbound messages matching the same conversation context.
4. **Server-Side Templates**: Uses `templ` components for UI markup, dynamically swapped inline using HTMX polling endpoints.

## 3. Data Flow Diagrams

### Outbound Path (Sending a reply)
```
[Client / Operator] 
      │ 
      ▼ (POST /api/v1/messages OR POST /admin/inbox/send)
[Api Handler] ── (Verify Queue Depth < 1000)
      │
      ▼ (Publish)
[NATS JetStream (MESSAGES Stream)]
      │
      ▼ (Consume)
[Queue Worker]
      │
      ▼ (Registry Resolve)
[Channel Adapter (whatsmeow / Telegram / WABA)] ──► [External Provider API]
```

### Inbound Path (Receiving a message)
```
[External Provider Webhook / WebSocket Event]
      │
      ▼
[Intake Webhook Handler / whatsmeow Listener]
      │
      ▼ (Handle)
[InboundProcessor]
      ├──► Deduplicate (inbound_dedup)
      ├──► Store Media (S3 Storage)
      ├──► Update Session (recipient_sessions)
      ├──► Route via Seam ──► [InboundRouter (Async goroutines)] ──► [Chatwoot / Typebot]
      └──► Log Audit Event & Publish (audit_logs & NATS WEBHOOKS Stream)
```
