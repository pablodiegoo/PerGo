<p align="center">
  <a href="https://github.com/pablodiegoo/PerGo">
    <img src="docs/assets/pergo-banner.svg" alt="PerGo CPaaS Banner" width="100%">
  </a>
</p>

<h3 align="center">High-Performance, Self-Hosted Omnichannel CPaaS Engineered in Go</h3>

<p align="center">
  <a href="https://golang.org/"><img src="https://img.shields.io/badge/Go-1.25%2B-00ADD8?style=flat-square&logo=go" alt="Go 1.25+"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg?style=flat-square" alt="MIT License"></a>
  <a href="https://www.docker.com/"><img src="https://img.shields.io/badge/Docker-Ready-2496ED?style=flat-square&logo=docker" alt="Docker"></a>
  <a href="https://www.postgresql.org/"><img src="https://img.shields.io/badge/PostgreSQL-16-4169E1?style=flat-square&logo=postgresql" alt="PostgreSQL 16"></a>
  <a href="https://nats.io/"><img src="https://img.shields.io/badge/NATS-JetStream-27AAE1?style=flat-square&logo=natsdotio" alt="NATS JetStream"></a>
  <a href="https://github.com/pablodiegoo/PerGo/actions"><img src="https://img.shields.io/github/actions/workflow/status/pablodiegoo/PerGo/ci.yml?style=flat-square&logo=github&label=CI%20Build" alt="CI Build"></a>
  <a href="https://spec.openapis.org/oas/v3.1.0"><img src="https://img.shields.io/badge/OpenAPI-3.1-85EA2D?style=flat-square&logo=openapiinitiative" alt="OpenAPI 3.1"></a>
</p>

<p align="center">
  <a href="docs/assets/screenshots/inbox-hero.png">
    <img src="docs/assets/screenshots/inbox-hero.png" alt="PerGo Live Omnichannel Inbox" width="100%">
  </a>
</p>
<p align="center">
  <em>Live Omnichannel Chat Inbox: Unified multi-turn conversation timeline across WhatsApp Web, WABA, and Telegram with real-time delivery telemetry, contact badges, and channel indicators.</em>
</p>

---

## Overview

**PerGo** is a self-hosted, open-source Omnichannel Communication Platform as a Service (CPaaS) engineered from the ground up in Go. It abstracts away provider fragmentation by exposing a single, unified REST API (`POST /api/v1/messages`) that routes messages across **WhatsApp Web** (unofficial multi-device via `whatsmeow`), **WhatsApp Cloud API** (official Meta WABA), and **Telegram Bot API** under a standardized JSON contract with automatic fallback pipelines.

Built for backend developers integrating mission-critical messaging into CRMs, ERPs, and autonomous AI agent workflows, PerGo delivers extreme resource efficiency (<50MB RAM), durable queuing via NATS JetStream, human-like anti-ban dispatch jitter, and 100% on-premise data sovereignty with zero per-message vendor markup.

---

## Why PerGo? Competitive Differentiators

Most open-source messaging solutions rely on heavy Node.js runtimes that consume hundreds of megabytes per session and suffer from event-loop starvation under high broadcast load. Commercial CPaaS vendors charge substantial per-message markups and force sensitive customer communication through third-party cloud infrastructure.

PerGo bridges this gap by combining the speed, safety, and memory footprint of compiled Go with enterprise-grade durability primitives:

| Dimension | PerGo | Node.js Bot Frameworks<br/>*(Evolution API, WPPConnect)* | Legacy SaaS CPaaS<br/>*(Twilio, Zenvia, Take Blip)* |
| :--- | :--- | :--- | :--- |
| **Memory Footprint** | **<50MB RAM** (Single compiled Go binary) | 300MB–800MB+ RAM per instance | N/A (Closed-source cloud API) |
| **Pricing & Markup** | **100% Free & Open Source** (Zero markup per message) | Free / Open Source (self-hosted) | $0.005–$0.05+ markup per message + recurring SaaS tiers |
| **Runtime & Concurrency** | **Compiled Go binary** with lightweight goroutines | Single-threaded JavaScript event loop | Multi-tenant cloud black box |
| **Message Backpressure** | **NATS JetStream** durable work queues with strict session limits | In-memory queues or Redis without native backpressure | Proprietary cloud queuing |
| **Anti-Ban Safety** | **Human-like anti-ban jitter** (1–3s randomized delay) + token bucket | Rudimentary fixed delay | Not applicable (official APIs only) |
| **Data Custody & Privacy** | **100% On-Premise / Self-Hosted** (Strict GDPR & LGPD compliance) | Self-hosted | Multi-tenant cloud: customer data leaves your perimeter |
| **Developer Experience** | **Embedded Scalar OpenAPI 3.1 & `/llms.txt` Agent Discovery** | Static markdown or basic Swagger | Proprietary SDKs & documentation |
| **Channel Redundancy** | **Built-in Automatic Fallback Pipelines** (e.g. Telegram → WABA) | Manual implementation per script | Vendor lock-in within proprietary ecosystem |

---

## Interactive Architecture

PerGo enforces clean separation of concerns: inbound HTTP requests and external provider events are ingested and immediately enqueued into a durable **NATS JetStream** work-queue boundary, insulating external networks and API consumers from transient surges.

```mermaid
flowchart TD
    subgraph Clients["Clients & Upstream Services"]
        API["Backend Apps / CRMs / Autonomous Agents"]
        WebhookSub["Customer Webhook Endpoints"]
    end

    subgraph PerGo["PerGo CPaaS Engine"]
        HTTP["Echo v5 HTTP Server & Ingestion Engine"]
        Scalar["Embedded Scalar OpenAPI 3.1 (/docs)"]
        AgentIdx["Curated Agent Index (/llms.txt)"]

        subgraph Queue["Durable Message Broker"]
            JetStream["NATS JetStream Work-Queue Boundary<br/>(Durable Storage, At-Least-Once Delivery, Backpressure)"]
        end

        subgraph Processors["Pipeline Processors"]
            OutboundProc["Outbound Dispatch Processor<br/>(Token-Bucket Limiter + Human Anti-Ban Jitter)"]
            InboundProc["Inbound Event Processor<br/>(Normalization, Deduplication & Delivery Timing)"]
        end

        subgraph Adapters["Channel Adapters"]
            WABA["WhatsApp Cloud API (Meta Graph API)"]
            WhatsMeow["WhatsApp Web (whatsmeow Multi-Device)"]
            Telegram["Telegram Bot API"]
        end

        subgraph Storage["Persistence & Crypto Layer"]
            PG[(PostgreSQL 16 with pgx/v5)]
            Crypto["AES-256-GCM Envelope Encryption (KEK/DEK)"]
        end
    end

    subgraph Networks["External Messaging Networks"]
        MetaCloud["Meta WhatsApp Cloud Infrastructure"]
        WAWeb["WhatsApp Multi-Device WebSocket Network"]
        TGNet["Telegram MTProto / Bot Network"]
    end

    API -->|POST /api/v1/messages| HTTP
    HTTP -->|Enqueue Outbound Job| JetStream
    JetStream -->|Consume Work Item| OutboundProc
    OutboundProc -->|Dispatch Message| Adapters
    Adapters -->|HTTPS REST| MetaCloud
    Adapters -->|TLS WebSocket| WAWeb
    Adapters -->|HTTPS Bot API| TGNet

    MetaCloud -->|Inbound Webhook| HTTP
    WAWeb -->|Inbound Socket Frame| WhatsMeow
    TGNet -->|Inbound Webhook| HTTP

    WhatsMeow -->|Raw Socket Event| InboundProc
    HTTP -->|Ingest Provider Webhook| InboundProc
    InboundProc -->|Safe Webhook Delivery (HMAC)| WebhookSub
    InboundProc -->|Persist Inbound Message| PG
    OutboundProc -->|Persist Dispatch Status| PG
    Adapters <-->|Secure Device Session Storage| Crypto
    Crypto <--> PG
```

---

## Feature Showcase

<table>
  <tr>
    <td width="50%">
      <h4 align="center">Real-Time Operator Dashboard</h4>
      <a href="docs/assets/screenshots/dashboard.png">
        <img src="docs/assets/screenshots/dashboard.png" alt="Operator Dashboard" width="100%">
      </a>
      <p align="center"><em>Live delivery throughput, active connection health cards, and workspace metrics.</em></p>
    </td>
    <td width="50%">
      <h4 align="center">Device Connections & QR Pairing</h4>
      <a href="docs/assets/screenshots/devices-qr.png">
        <img src="docs/assets/screenshots/devices-qr.png" alt="WhatsApp QR Pairing" width="100%">
      </a>
      <p align="center"><em>Instant WhatsApp Web pairing via browser QR scan and live connection state management.</em></p>
    </td>
  </tr>
  <tr>
    <td width="50%">
      <h4 align="center">Broadcast Campaign Manager</h4>
      <a href="docs/assets/screenshots/campaigns.png">
        <img src="docs/assets/screenshots/campaigns.png" alt="Broadcast Campaigns" width="100%">
      </a>
      <p align="center"><em>Segmented message campaigns with tag filtering, token-bucket pacing, and delivery progress.</em></p>
    </td>
    <td width="50%">
      <h4 align="center">Embedded Scalar API Explorer</h4>
      <a href="docs/assets/screenshots/api-docs.png">
        <img src="docs/assets/screenshots/api-docs.png" alt="Scalar OpenAPI 3.1 Portal" width="100%">
      </a>
      <p align="center"><em>Zero-dependency OpenAPI 3.1 interactive testing console served at <code>/docs</code>.</em></p>
    </td>
  </tr>
</table>

---

## ☁️ PerGo Cloud (Coming Soon)

Are you looking for an enterprise managed solution without the operational overhead of running Docker, configuring reverse proxies, or rotating IP proxies for WhatsApp Web?

**PerGo Cloud** is our commercial managed SaaS platform engineered for agencies, growth teams, and enterprise systems that require guaranteed delivery SLAs and turnkey Meta onboarding:

| Feature / Capability | Self-Hosted Community Edition | ☁️ PerGo Cloud (Managed SaaS) |
| :--- | :--- | :--- |
| **License & Source Code** | 100% Free & Open-Source (MIT) | Commercial Managed Control Plane |
| **Infrastructure & Hosting** | Self-managed VPS, Docker, or Bare Metal | High-Availability Managed Multi-Region Cloud |
| **Maintenance & Updates** | Manual Docker Compose updates & database migrations | Zero-downtime rolling updates & automated off-site backups |
| **WhatsApp Cloud (WABA)** | Manual Meta App & System User configuration | 1-Click Embedded Meta Tech Provider Onboarding |
| **WhatsApp Web Anti-Ban** | Single static server IP (higher suspension risk) | Managed Residential Proxy Pools with dynamic geo-rotation |
| **Message Queuing & Broker** | Single-node NATS JetStream container | Fault-tolerant Clustered NATS JetStream with DLQ alerting |
| **Multi-Tenancy & Billing** | Single organization per installation | Multi-tenant organization hierarchies, seat RBAC & invoicing |
| **Support & SLAs** | Community GitHub Discussions & Issues | 24/7 Dedicated Support, SLA guarantees & private Slack channel |

<p align="center">
  <a href="mailto:pablodiegoo@gmail.com?subject=[PerGo%20Cloud]%20Early%20Access%20Request">
    <img src="https://img.shields.io/badge/☁️%20Request%20Early%20Access-PerGo%20Cloud-007ACC?style=for-the-badge&logo=googlecloud" alt="Request Early Access">
  </a>
</p>

> **Join the Early Access Program:** Interested in testing PerGo Cloud before public launch?  
> Send an email to [**pablodiegoo@gmail.com**](mailto:pablodiegoo@gmail.com?subject=[PerGo%20Cloud]%20Early%20Access%20Request) with your estimated monthly message volume and channel requirements to receive an invitation to our private beta.

---

## 60-Second Quickstart

### Option A: Automated 1-Click VPS Installer (Recommended for Production)

Deploy PerGo directly to any fresh Ubuntu/Debian VPS with **Traefik v3**, automated Let's Encrypt SSL/TLS certificates, PostgreSQL 16, and NATS JetStream:

```bash
curl -fsSL https://raw.githubusercontent.com/pablodiegoo/PerGo/main/install.sh | bash
```

Or run non-interactively with your parameters:

```bash
./install.sh --domain api.pergo.yourdomain.com --email admin@yourdomain.com --yes
```

---

### Option B: Local Docker Compose (1-Command Full Stack)

Spin up the entire PerGo stack (PerGo server on `:8080`, PostgreSQL 16 on `:5432`, and NATS JetStream on `:4222`) in one command:

```bash
git clone https://github.com/pablodiegoo/PerGo.git
cd PerGo
docker compose up -d
```

Open your browser:
- **Operator Console:** [http://localhost:8080/admin](http://localhost:8080/admin) (Default password: `troque-esta-senha`)
- **Interactive API Documentation:** [http://localhost:8080/docs](http://localhost:8080/docs)
- **Health Probe:** [http://localhost:8080/healthz](http://localhost:8080/healthz)

---

### Option C: Native Go Development (with Hot Reload)

To run PerGo directly with the Go toolchain:

```bash
# 1. Start backing services
docker compose up -d postgres nats

# 2. Configure environment
cp .env.example .env

# 3. Launch with hot reload
make dev
```

---

## API Usage Examples

PerGo exposes a unified endpoint at `POST /api/v1/messages`. All requests authenticate using a Workspace API key generated in the operator console.

### 1. Send WhatsApp Web Message
```bash
curl -X POST http://localhost:8080/api/v1/messages \
  -H "Authorization: Bearer YOUR_WORKSPACE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "to": "5511999999999",
    "channel": "whatsapp",
    "body": "Hello from PerGo! Unified CPaaS in action."
  }'
```

### 2. Send Telegram Message with Automatic WABA Fallback
```bash
curl -X POST http://localhost:8080/api/v1/messages \
  -H "Authorization: Bearer YOUR_WORKSPACE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "to": "5511999999999",
    "channel": "telegram",
    "body": "Critical alert: your invoice has been generated.",
    "fallback_channels": ["whatsapp_cloud"]
  }'
```

### 3. Send Media Document (PDF / Image / Voice Note)
```bash
curl -X POST http://localhost:8080/api/v1/messages \
  -H "Authorization: Bearer YOUR_WORKSPACE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "to": "5511999999999",
    "channel": "whatsapp_cloud",
    "body": "",
    "media": {
      "media_url": "https://example.com/monthly-statement.pdf",
      "media_type": "document",
      "filename": "monthly-statement.pdf",
      "caption": "Your monthly statement is ready."
    }
  }'
```

### 4. Send Pre-Approved WhatsApp Cloud Template (WABA)
```bash
curl -X POST http://localhost:8080/api/v1/messages \
  -H "Authorization: Bearer YOUR_WORKSPACE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "to": "5511999999999",
    "channel": "whatsapp_cloud",
    "template_name": "order_confirmation",
    "language": "en_US",
    "components": [
      {
        "type": "body",
        "parameters": [
          {"type": "text", "text": "Jane Doe"},
          {"type": "text", "text": "ORD-2026-9821"}
        ]
      }
    ]
  }'
```

---

## 🤖 Agent-Ready CPaaS & Autonomous AI

PerGo is engineered for autonomous AI agents and modern LLM orchestration frameworks:

* **Curated Agent Index (`/llms.txt` & `/llms-full.txt`)**: Machine-readable specification detailing system architecture, message schemas, and API constraints for rapid agent context ingestion.
* **Content Negotiation**: Requesting any documented route with `Accept: text/markdown` automatically yields clean, token-efficient Markdown instead of HTML.
* **Model Context Protocol (MCP) Server**: PerGo includes a built-in MCP diagnostic suite allowing AI agents (like Claude Desktop, Cursor, or custom sidecars) to audit and control messaging infrastructure programmatically:
  - `diagnose_connection_health`: Test socket connections, decrypt credentials, and verify provider tokens.
  - `simulate_webhook_event`: Synthesize mock inbound messages, button clicks, and delivery receipts for automated pipeline testing.
  - `replay_webhook_dlq`: Inspect and replay failed webhook deliveries from the encrypted Dead-Letter Queue.
  - `inspect_queue_health`: Real-time telemetry on NATS JetStream consumer lag, delivery rates, and pending dispatches.

---

## Official Documentation Hub

For exhaustive technical blueprints, channel credential guides, and deployment playbooks, visit the official documentation:

* **[Getting Started](docs/getting-started/index.md):** Local installation, configuration variables, and dependencies.
* **[Architecture Blueprint](docs/architecture/index.md):** Detailed concurrency model, JetStream resilience, and schema designs.
* **[Channel Integrations](docs/channels/index.md):** Setup guides for WhatsApp Web (`whatsmeow`), WhatsApp Cloud (WABA), and Telegram.
* **[API Reference](docs/api/index.md):** Complete OpenAPI 3.1 specification, HMAC webhooks, and endpoint contracts.
* **[Production Deployment](docs/deployment/index.md):** Production Docker Compose (`docker-compose.prod.yml`), Traefik v3, and TLS.
* **[Visual Asset Guidelines](docs/SCREENSHOTS.md):** Deterministic mock data seeding and UI screenshot capture lifecycle (ADR 0013).
* **[Contributing Guide](CONTRIBUTING.md):** Branching model, code standards, and PR workflows.
* **[Code of Conduct](CODE_OF_CONDUCT.md):** Contributor Covenant v2.1 standards.
* **[Security Policy](SECURITY.md):** Vulnerability reporting and disclosure policies.

---

## Community & Contributing

We welcome community contributions! Please read our **[Contributing Guide](CONTRIBUTING.md)** before submitting a Pull Request.

1. **Bug Reports & Feature Requests:** Use our structured [GitHub Issue Forms](https://github.com/pablodiegoo/PerGo/issues/new/choose).
2. **Pre-flight Checks:** Verify your changes pass formatting, race condition testing, and linting:
   ```bash
   make lint
   make test-race
   ```
3. **UI Contributions:** If your pull request modifies UI templates or styles, update the official screenshots using `make screenshots` according to **[docs/SCREENSHOTS.md](docs/SCREENSHOTS.md)**.

---

## License

This project is licensed under the [MIT License](LICENSE).
