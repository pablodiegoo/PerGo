# Local Installation & Development

This guide walks you through setting up and running **PerGo** locally for development and testing.

---

## 1. Prerequisites

Ensure the following dependencies are installed on your host machine:

- **Go 1.25+** (Toolchain 1.26+): [Install Go](https://go.dev/dl/)
- **Docker & Docker Compose**: [Install Docker](https://docs.docker.com/get-docker/)
- **Air** (live hot-reloading daemon for Go):
  ```bash
  go install github.com/air-verse/air@latest
  ```
- **Templ** (compile-time type-safe HTML template engine for Go):
  ```bash
  go install github.com/a-h/templ/cmd/templ@latest
  ```

Ensure your Go binary path is included in your system `PATH`:
```bash
export PATH=$PATH:$(go env GOPATH)/bin
```

---

## 2. Clone the Repository

Clone the PerGo repository and enter the project directory:

```bash
git clone https://github.com/pablodiegoo/PerGo.git
cd PerGo
```

Download Go module dependencies:
```bash
go mod download
```

---

## 3. Configure Environment Variables

Create your local `.env` configuration file from the provided example:

```bash
cp .env.example .env
```

Review `.env` in your editor. For local development, default values are usually sufficient. 

> [!IMPORTANT]
> The `PERGO_KEK_BASE64` variable must contain a valid 32-byte Base64-encoded Key Encryption Key (KEK) used for AES-256-GCM envelope encryption. You can generate one with:
> ```bash
> openssl rand -base64 32
> ```
> For full variable documentation, see the [Configuration Guide](configuration.md).

---

## 4. Spin Up Infrastructure Services

PerGo requires PostgreSQL 16 and NATS JetStream. Spin up these local services via Docker Compose:

```bash
make infra
```

This starts:
- **PostgreSQL** on `localhost:5433` (database: `pergo`, user: `postgres`, password: `postgres`)
- **NATS JetStream** on `localhost:4222` (monitoring UI on `8222`)

For more details on local containers, see [Docker Compose](docker-compose.md).

---

## 5. Compile Templates & Run Development Server

Generate the Templ UI components and start the server with live hot-reloading:

```bash
make dev
```

On first startup, PerGo automatically executes embedded database migrations (via `goose`), creating all necessary tables (`workspaces`, `api_keys`, `connections`, `audit_logs`, `message_dispatches`, `campaigns`, etc.).

---

## 6. Access the Operator Console

1. Open your browser at: **`http://localhost:8080/admin`**
2. Log in using the administrator password configured in `PERGO_ADMIN_PASSWORD` (default development value: `pergo-dev-2026`).
3. Create your first **Workspace** (e.g., "Main Organization" or "Dev Testing").
4. Under your Workspace details:
   - **Generate API Key:** Click **Generate Key** to produce a workspace bearer token (`pgo_live_...`) for authenticating REST requests.
   - **Configure Channels:** Navigate to **Channels** to connect a Telegram Bot token, WABA Cloud credentials, or pair a WhatsApp Web device by scanning the generated QR Code.

---

## Common Setup Troubleshooting

### Database Connection Refused (`connect: connection refused` on port 5432 or 5433)
- **Cause:** PostgreSQL container is not running or the port in `PERGO_DATABASE_URL` does not match the host mapping.
- **Fix:** PerGo's local compose maps internal port `5432` to host port `5433` to avoid clashing with any existing system PostgreSQL instances. Ensure `PERGO_DATABASE_URL` in `.env` points to port `5433`:
  ```env
  PERGO_DATABASE_URL=postgres://postgres:postgres@localhost:5433/pergo?sslmode=disable
  ```
  Run `make infra` to ensure the container is up.

### NATS Connection Refused (`nats: connect: connection refused`)
- **Cause:** NATS broker is not running or `PERGO_NATS_URL` is wrong.
- **Fix:** Ensure `make infra` is active and `PERGO_NATS_URL` is set to `nats://localhost:4222`.

### Missing `air` or `templ` Command
- **Cause:** Go binary directory is not in your terminal `PATH`.
- **Fix:** Add `$(go env GOPATH)/bin` to your `PATH` or add `export PATH=$PATH:$HOME/go/bin` to your `~/.bashrc` or `~/.zshrc`.

### Templ Compilation Error
- **Cause:** Out-of-sync or invalid `.templ` templates.
- **Fix:** Run `make generate` to inspect Templ compiler error messages.

---

## Next Steps

- Review the [Configuration Guide](configuration.md) to customize ports, S3 storage, and security secrets.
- Check [Channel Setup](../channels/index.md) to configure your WhatsApp and Telegram integrations.
- Review [Testing Guide](../development/testing.md) for running unit and concurrency tests.
