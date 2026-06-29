# PerGo

PerGo is a self-hosted, open-source Omnichannel Communications Platform as a Service (CPaaS) engineered in Go. It exposes a single, unified REST API (`POST /messages`) that abstracts away the fragmentation of managing multiple messaging providers—WhatsApp Web (unofficial via `whatsmeow`), WhatsApp Cloud (WABA), and Telegram—under a single standardized JSON payload.

It is built for backend developers integrating omnichannel messaging into CRMs/ERPs and for system operators managing channel connections, compliance, and logs under full data custody.

## Core Value

* **Unified API:** A single API request delivers a message through any configured channel with automatic fallback.
* **Self-Hosted & Cost-Effective:** No per-message vendor markup. High-performance, self-hosted platform under your full custody (fully GDPR/LGPD compliant).
* **Backpressure & Reliability:** Enforces a 1,000-message per-session queue limit before enqueue (yielding HTTP 429/422 on overload). Powered by NATS JetStream for durable queuing.
* **Safety Primitives:** Staggered dispatch (1–3s random delay) for WhatsApp Web to mimic human behavior and minimize account suspension risk.

---

## Technical Stack

* **Language:** Go 1.25+ (Toolchain 1.26+)
* **HTTP Router:** Echo v5 (`github.com/labstack/echo/v5`)
* **Admin Dashboard:** `a-h/templ` (type-safe compile-time HTML template engine) + HTMX (for server-driven interactive components)
* **Broker / Work-Queue:** NATS JetStream (durable delivery boundary)
* **Persistence:** PostgreSQL via `pgx/v5` (driver) with `goose` for embedded schema migrations
* **WhatsApp Web Integration:** `whatsmeow` (unofficial multi-device WhatsApp adapter)
* **Rate Limiting:** `golang.org/x/time/rate` (token-bucket rate limiting)

---

## Repository Directory Layout

* `cmd/pergo/`: Server entry point and composition root.
* `internal/`: Core application modules.
  * `api/`: API handlers (health, messages, admin dashboard) and middlewares (auth, HTMX, rate limiter, trace).
  * `channel/`: Message dispatch registry and channel adapters (WhatsApp Web, WABA, Telegram).
  * `config/`: Configurations loaded from 12-factor environment variables.
  * `domain/`: Core messaging domain structures.
  * `platform/`: Shared infrastructural components (audit writer, crypto, database connections, NATS queue worker, shutdown orchestrator).
  * `repository/`: Database repositories (workspaces, API keys, audit).
  * `session/`: WhatsApp device sessions and registry.
* `static/`: Static assets (CSS, images) for the operator console.
* `templates/`: `a-h/templ` components and views.

For deeper architectural context, see the [Documentation Directory](file:///home/pablo/Coding/PerGo/docs/architecture/README.md).
For a step-by-step guide on configuring messaging channels (Telegram, WABA, WhatsApp Web), see the [Channels Setup Guide](file:///home/pablo/Coding/PerGo/docs/CHANNELS.md).

---

## Getting Started

### Prerequisites

* Go 1.26+ installed locally (if running outside Docker)
* Docker and Docker Compose (recommended for dependencies)
* PostgreSQL 16+ (if not running via Docker)
* NATS Server (if not running via Docker)

### 1. Run Dependencies (Postgres & NATS)

Use Docker Compose to spin up the local environment dependencies:
```bash
docker compose up -d
```

This starts:
* **PostgreSQL** on port `5432` (Username/Password/Database: `postgres`/`postgres`/`pergo`)
* **NATS** on port `4222` (and management console on `8222`)

### 2. Environment Variables

Configure your local environment. Default variables are structured in the app configuration:
* `DATABASE_URL`: Connection string for PostgreSQL (e.g. `postgres://postgres:postgres@localhost:5432/pergo?sslmode=disable`)
* `NATS_URL`: Connection string for NATS (e.g. `nats://localhost:4222`)
* `PERGO_ADMIN_PASSWORD`: Plain password to access the `/admin` operator console.
* `PERGO_SESSION_SECRET`: Key used for signing administration cookies.

### 3. Running Locally

To start the application, run:
```bash
make run
```
or
```bash
go run ./cmd/pergo
```

On start, goose database migrations will automatically be executed to set up the necessary PostgreSQL schemas.

### 4. Running via Docker

To build and run the entire stack (including the `pergo` binary itself) inside Docker:
```bash
docker compose up --build
```

---

## Verification & Testing

To run the unit tests:
```bash
make test
```

To run tests with race condition detection:
```bash
make test-race
```

To lint the codebase:
```bash
make lint
```
