---
last_mapped_commit: 99448836f14aa64e923a366b95721858185d878b
last_mapped_date: 2026-07-18
---

# Tech Stack Map

This document describes the technologies, libraries, versions, configuration approach, build system, and deployment mechanisms for PerGo.

## 1. Languages & Runtime

- **Go**: `1.26.4` (enforced in `go.mod`).
  - Native features used: `log/slog` for structured logging, `net/http` underlying routing, `math/rand/v2` for random dispatch delays, and standard library concurrency primitives (goroutines, channels, mutexes).

## 2. Core Frameworks

- **HTTP Server**: `github.com/labstack/echo/v5` (v5.2.1).
  - Web routing, context binder, error handling, and middleware integration. Uses Echo v5's native `*slog.Logger` alignment.
- **Templating Engine**: `github.com/a-h/templ` (v0.3.1020).
  - Compile-time type-safe HTML template rendering directly to Go. Generates code before builds.
- **Frontend Interactivity**: **HTMX 2.x** (`htmx.org@2.0.10`).
  - Used for AJAX requests, server-sent fragments, out-of-band updates (`hx-swap-oob="true"`), and real-time polling (every 3s for chats, every 5s for lists).

## 3. Database & Broker Integrations

- **Database**: PostgreSQL (v16).
- **PostgreSQL Driver**: `github.com/jackc/pgx/v5` (v5.10.0).
  - Binary protocol support, prepared-statement caching, and batch query pipelines. Includes `github.com/jackc/pgx/v5/stdlib` to bridge goose and whatsmeow to the same database driver.
- **Broker**: NATS Server (v2.10+) with JetStream enabled.
- **NATS Client**: `github.com/nats-io/nats.go` (v1.52.0).
  - JetStream work-queue durability for outbound message dispatch (`Retention: WorkQueuePolicy`, `Discard: DiscardNew`, `MaxMsgs: 1000`).

## 4. Third-Party Channels

- **WhatsApp Web (Unofficial)**: `go.mau.fi/whatsmeow` (pseudo-version `v0.0.0-20260622185415-5f04eac6dbbb`).
  - Unofficial WhatsApp Web multi-device protocol wrapper. Interacts with SQLite/Postgres store for credentials.
- **WhatsApp Cloud (WABA)**: WhatsApp Business Platform API.
- **Telegram Bot API**: Telegram webhook configuration.

## 5. Build & Dev Tooling

- **Makefile**: Defines build workflows:
  - `make check`: Generates templates, formats, and executes linter.
  - `make test`: Runs the test suite with `-race -count=1`.
  - `make dev`: Launches local hot-reload using `air`.
  - `make prod`: Builds and deploys production containers using docker-compose.
- **Air**: Hot reloading daemon for development (`.air.toml`).
- **Goose**: `github.com/pressly/goose/v3` (v3.27.1) for database schema migrations. Migrations are automatically applied on application start.

## 6. Infrastructure & Deployment

- **Containerization**: `Dockerfile` uses a multi-stage build:
  - Builder stage compiles static binary (using `CGO_ENABLED=0` for cross-compilation).
  - Production stage runs inside a minimal `alpine` container (previously scratch, changed for ca-certificates compatibility).
- **Orchestration**: `docker-compose.yml` boots three services:
  - `postgres` (port `5432`): System of record.
  - `nats` (port `4222`): Durable event broker.
  - `pergo` (port `8080`): Main API and UI dashboard.
