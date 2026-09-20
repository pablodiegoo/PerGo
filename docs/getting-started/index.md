# Getting Started with PerGo

Welcome to **PerGo**, a high-performance, self-hosted Omnichannel Communications Platform as a Service (CPaaS) engineered in Go.

PerGo exposes a single, unified REST API (`POST /api/v1/messages`) that abstracts away the fragmentation of managing multiple messaging providers—**WhatsApp Web** (unofficial via `whatsmeow`), **WhatsApp Cloud** (official Meta WABA), and **Telegram**—under a single standardized JSON payload.

---

## Core Capabilities

- **Unified Dispatch API:** Deliver messages through any configured channel with automatic fallback (e.g. try WhatsApp Web first, fallback to Telegram or WABA if undelivered).
- **Self-Hosted & Cost-Effective:** Zero per-message vendor markups. Full data custody and sovereignty compliant with GDPR, LGPD, and enterprise compliance regulations.
- **Durable Broker & Backpressure:** Powered by NATS JetStream for resilient queueing. Enforces a 1,000-message per-workspace queue depth limit before enqueue to prevent memory exhaustion under bursts.
- **Human Emulation & Safety:** Configurable anti-ban random delays (1–3s) for WhatsApp Web dispatches to emulate natural human behavior and minimize ban risk.
- **Headless CPaaS & Embedded Console:** Fully programmatic REST API for provisioning tenant workspaces, API keys, webhooks, and QR pairing, alongside a server-rendered operator console (`/admin`) with encrypted session SSO hand-off.

---

## Prerequisites

Before setting up PerGo, ensure you have the following prerequisites installed:

| Dependency | Minimum Version | Required For |
| :--- | :--- | :--- |
| **Go** | 1.25+ (Toolchain 1.26+) | Local compilation, running tests, or building native binaries |
| **Docker & Docker Compose** | v2.20+ | Running database, message broker, or containerized production deployment |
| **PostgreSQL** | 15+ (16 recommended) | Relational persistence, connection credentials, and audit logs |
| **NATS Server** | 2.10+ (with JetStream enabled) | Asynchronous work queue, worker decoupling, and backpressure isolation |

---

## Guides in this Section

1. **[Installation Guide](installation.md)**: Step-by-step local development setup, toolchain installation (Air, Templ), and running the development server.
2. **[Docker Compose Infrastructure](docker-compose.md)**: Spin up local dependencies (PostgreSQL 16 and NATS JetStream) with a single command.
3. **[1-Click VPS Quickstart](vps-quickstart.md)**: Automated production installer for Ubuntu/Debian Linux VPS instances with Traefik v3 and Let's Encrypt SSL.
4. **[Configuration Reference](configuration.md)**: Comprehensive guide to environment variables, AES-256-GCM encryption key generation, and ports.

---

## Next Steps

- Explore the **[Architecture Overview](../architecture/index.md)** to understand how PerGo handles concurrency, resilience, and data isolation.
- Learn how to connect providers in **[Channel Setup](../channels/index.md)**.
- Review the **[API Reference](../api/index.md)** to start sending messages and webhooks.
