# PerGo

PerGo is a self-hosted, open-source Omnichannel Communications Platform as a Service (CPaaS) engineered in Go. It exposes a single, unified REST API (`POST /api/v1/messages`) that abstracts away the fragmentation of managing multiple messaging providers—WhatsApp Web (unofficial via `whatsmeow`), WhatsApp Cloud (WABA), and Telegram—under a single standardized JSON payload.

It is built for backend developers integrating omnichannel messaging into CRMs/ERPs and for system operators managing channel connections, compliance, and logs under full data custody.

> **TL;DR (Quick Start):**
> 
> ```bash
> make generate && make prod-down && make prod
> ```

---

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
*   `api/`: API handlers (health, messages, admin dashboard) and middlewares (auth, HTMX, rate limiter, trace).
*   `channel/`: Message dispatch registry and channel adapters (WhatsApp Web, WABA, Telegram).
*   `config/`: Configurations loaded from 12-factor environment variables.
*   `domain/`: Core messaging domain structures.
*   `platform/`: Shared infrastructural components (audit writer, crypto, database connections, NATS queue worker, shutdown orchestrator).
*   `repository/`: Database repositories (workspaces, API keys, connections, audit).
*   `session/`: WhatsApp device sessions and registry.
* `static/`: Static assets (CSS, images) for the operator console.
* `templates/`: `a-h/templ` components and views.

## Documentação

Para guias detalhados de configuração, desenvolvimento e implantação do PerGo, consulte a documentação oficial:

* **Arquitetura do PerGo:** [Visão Geral de Arquitetura](file:///home/pablo/Coding/OmniGo/docs/architecture/README.md)
* **Como Começar:** [Guia de Início Rápido (Getting Started)](file:///home/pablo/Coding/OmniGo/docs/GETTING-STARTED.md)
* **Configurações:** [Variáveis de Ambiente (.env)](file:///home/pablo/Coding/OmniGo/docs/CONFIGURATION.md)
* **Referência da API:** [Endpoints, Payloads e Erros](file:///home/pablo/Coding/OmniGo/docs/API.md)
* **Configuração de Provedores:** [Telegram, WABA e WhatsApp Web Setup](file:///home/pablo/Coding/OmniGo/docs/CHANNELS.md)
* **Desenvolvimento:** [Estrutura de Pastas e Diretrizes](file:///home/pablo/Coding/OmniGo/docs/DEVELOPMENT.md)
* **Testes:** [Guia de Escrita e Execução de Testes](file:///home/pablo/Coding/OmniGo/docs/TESTING.md)
* **Implantação (Deploy):** [Dockerfile e Compose para Produção](file:///home/pablo/Coding/OmniGo/docs/DEPLOYMENT.md)

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
* **PostgreSQL** on port `5433` (Username/Password/Database: `postgres`/`postgres`/`pergo`)
* **NATS** on port `4222` (and management console on `8222`)

### 2. Environment Variables

Configure your local environment. Default variables are structured in the app configuration:
* `PERGO_DATABASE_URL`: Connection string for PostgreSQL (e.g. `postgres://postgres:postgres@localhost:5433/pergo?sslmode=disable`)
* `PERGO_NATS_URL`: Connection string for NATS (e.g. `nats://localhost:4222`)
* `PERGO_ADMIN_PASSWORD`: Plain password to access the `/admin` operator console.
* `PERGO_SESSION_SECRET`: Key used for signing administration cookies.

### 3. Running Locally

To start the application, run:
```bash
make dev
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

---

## Installation

To set up PerGo on your local machine or server:

1. **Clone the repository:**
   ```bash
   git clone https://github.com/pablodiegoo/PerGo.git
   cd PerGo
   ```
   <!-- VERIFY: The repository URL is https://github.com/pablodiegoo/PerGo.git -->

2. **Install Go dependencies:**
   Make sure you have Go 1.26+ installed, then download the dependencies:
   ```bash
   go mod download
   ```

3. **Install Code-Generation Tools:**
   PerGo uses `a-h/templ` for the admin UI. Install it to your path:
   ```bash
   go install github.com/a-h/templ/cmd/templ@latest
   ```

4. **Generate Templ Files:**
   Generate the Go templates from the `.templ` source files:
   ```bash
   make generate
   ```

5. **Build the Application:**
   Compile the binary to `./bin/pergo`:
   ```bash
   make build
   ```

---

## Usage Examples

PerGo exposes a single unified REST endpoint at `POST /api/v1/messages` to send messages. All requests must include a workspace API Key passed in the `Authorization` header.

### Authenticating
Generate an API key in the admin dashboard (by default at `http://localhost:8080/admin`).
Include it in your request headers:
```http
Authorization: Bearer <your_api_key>
```

### 1. Send a Text Message (WhatsApp Web)
```bash
curl -X POST http://localhost:8080/api/v1/messages \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "to": "5511999999999",
    "channel": "whatsapp",
    "body": "Hello from PerGo!"
  }'
```

### 2. Send a Text Message with Fallbacks (Telegram with WABA fallback)
```bash
curl -X POST http://localhost:8080/api/v1/messages \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "to": "5511999999999",
    "channel": "telegram",
    "body": "Hello from PerGo with Fallbacks!",
    "fallback_channels": ["whatsapp_cloud"]
  }'
```

### 3. Send a Media Message
```bash
curl -X POST http://localhost:8080/api/v1/messages \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "to": "5511999999999",
    "channel": "whatsapp_cloud",
    "body": "",
    "media": {
      "media_url": "https://example.com/invoice.pdf",
      "media_type": "document",
      "filename": "invoice.pdf",
      "caption": "Your monthly invoice"
    }
  }'
```

### 4. Send a WhatsApp Template (WhatsApp Cloud API)
```bash
curl -X POST http://localhost:8080/api/v1/messages \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "to": "5511999999999",
    "channel": "whatsapp_cloud",
    "body": "",
    "template_name": "welcome_message",
    "language": "en_US",
    "components": [
      {
        "type": "body",
        "parameters": [
          {
            "type": "text",
            "text": "Jane Doe"
          }
        ]
      }
    ]
  }'
```

---

## Contributing

We welcome contributions to PerGo! If you want to contribute, please follow these guidelines:

1. **Open an Issue:** Discuss significant changes by creating an issue first.
2. **Submit a Pull Request:** Fork the repository, create a branch, and submit your PR.
3. **Format & Verify:** Ensure that the linter passes and all unit tests run successfully before committing:
   ```bash
   make lint
   ```
   ```bash
   make test-race
   ```

Refer to the [DEVELOPMENT.md](file:///home/pablo/Coding/OmniGo/docs/DEVELOPMENT.md) guide for folder structure details.
<!-- VERIFY: CONTRIBUTING.md file doesn't exist yet but contributing instructions point to standard fork-and-pull-request flow -->

---

## License

This project is licensed under the MIT License.
<!-- VERIFY: The project is licensed under the MIT License -->

---

## Quick Start

The quickest way to get PerGo up and running is using Docker Compose:

1. **Start Postgres and NATS services:**
   ```bash
   docker compose up -d
   ```
2. **Generate UI template files:**
   ```bash
   make generate
   ```
3. **Start the local development server:**
   ```bash
   make dev
   ```
   The admin console will be available at `http://localhost:8080/admin`.
