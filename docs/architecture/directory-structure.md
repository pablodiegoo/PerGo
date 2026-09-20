# Repository Directory Layout & Architectural Seams

PerGo adheres to a domain-oriented package layout rather than traditional multi-layered MVC structures. Each package possesses clear ownership, explicit dependencies, and well-defined public boundaries (seams).

---

## 1. Directory Tree

```
PerGo/
├── cmd/
│   ├── pergo/                     # Main server entrypoint, composition root, CLI commands
│   └── pergo-seed/                # Seed script for initial workspace and fixture data
├── docs/                          # Technical documentation and guides
├── internal/
│   ├── api/                       # HTTP routes, middlewares, REST controllers, and Scalar UI
│   │   ├── handler/               # REST handlers (messages, workspaces, devices, campaigns)
│   │   │   ├── admin/             # HTMX-driven administrative dashboard handlers
│   │   │   └── api/               # External programmatic CPaaS endpoints
│   │   └── middleware/            # Auth, rate limiting, HTMX detection, trace propagation
│   ├── channel/                   # Channel abstraction and adapter implementations
│   │   ├── whatsapp/              # WhatsApp Web (whatsmeow) and WABA Cloud adapters
│   │   ├── telegram/              # Telegram Bot API adapter
│   │   └── dispatcher.go          # Dispatcher public seam interface
│   ├── config/                    # 12-factor environment variable parser
│   ├── domain/                    # Core messaging models, statuses, and sentinel errors
│   ├── inbound/                   # Inbound message processing, media download, and deduplication
│   ├── outbound/                  # Outbound dispatch orchestrator and fallback pipeline
│   ├── platform/                  # Shared infrastructural drivers
│   │   ├── audit/                 # Asynchronous buffered batch audit writer
│   │   ├── breaker/               # Minimal 3-state circuit breaker
│   │   ├── crypto/                # AES-256-GCM envelope encryption and API key hashing
│   │   ├── postgres/              # pgxpool connection management and goose migrations
│   │   └── queue/                 # NATS JetStream producer, consumer workers, and queue metrics
│   ├── repository/                # SQL persistence repositories (workspaces, contacts, dispatches)
│   ├── session/                   # Stateful WhatsApp Web device connection registry
│   └── webhook/                   # Outbound HMAC-SHA256 signed webhook delivery
├── static/                        # Static styling and frontend scripts
└── templates/                     # a-h/templ compile-time HTML components
```

---

## 2. Key Architectural Seams

A **seam** is a clean boundary where behavior can be observed or tested without inspecting private implementation details:

- **`channel.Dispatcher`:** The public contract for sending a message through a specific communication network.
- **`outbound.DispatchOrchestrator`:** Owns idempotency gatekeeping, fallback evaluation, and retry loops independently of the NATS consumer transport.
- **`inbound.InboundProcessor`:** Intercepts raw provider webhook events, maps them into normalized domain events, persists attachments to S3, and routes to registered third-party CRM sinks (e.g. Chatwoot, Typebot).
- **`audit.Writer`:** High-performance async recording port decoupled from database commit latency.
