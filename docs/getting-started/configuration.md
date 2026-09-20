# Configuration & Environment Variables

PerGo follows the [12-Factor App](https://12factor.net/config) methodology, loading all runtime configuration from environment variables.

---

## 1. Core Service Variables

| Variable | Default Value | Description |
| :--- | :--- | :--- |
| `PERGO_SERVER_PORT` | `8080` | Main HTTP port serving Admin Console, REST APIs, and inbound webhooks. |
| `PERGO_DEBUG_PORT` | `6060` | Internal debug port exposing pprof profiling (`/debug/pprof`) and Go runtime expvar metrics. Should **never** be exposed to the public internet. |
| `PERGO_EXTERNAL_URL` | `http://localhost:8080` | Publicly reachable HTTPS base URL (e.g. `https://api.pergo.example.com`). Required for Telegram and Meta WABA webhook registrations. |
| `PERGO_DATABASE_URL` | `postgres://postgres:postgres@localhost:5433/pergo?sslmode=disable` | PostgreSQL DSN. In production, use `sslmode=require` or `sslmode=verify-full`. |
| `PERGO_NATS_URL` | `nats://localhost:4222` | Connection string to NATS broker with JetStream enabled. |

---

## 2. Security & Encryption Keys

| Variable | Default Value | Description |
| :--- | :--- | :--- |
| `PERGO_KEK_BASE64` | *(auto-fallback in dev)* | **Key Encryption Key (KEK)** in Base64 format. Must decode to **exactly 32 bytes (256 bits)** for AES-256-GCM envelope encryption. Encrypts channel credentials and session secrets at rest. |
| `PERGO_ADMIN_PASSWORD` | `pergo-dev-2026` | Master password for operator authentication on `/admin/login`. Change immediately for production. |
| `PERGO_MASTER_KEY` | *(defaults to admin password)* | Secret bearer key for headless provisioning APIs (`POST /api/v1/workspaces`). |
| `PERGO_SESSION_SECRET` | *(derived if empty)* | Secret key for signing and verifying HTTP session cookies and SSO hand-off tokens. |

### Generating a Cryptographic KEK

To generate a secure 32-byte Base64 key for `PERGO_KEK_BASE64`:

```bash
openssl rand -base64 32
```

> [!CAUTION]
> **Never change or rotate `PERGO_KEK_BASE64` arbitrarily after inserting channel credentials or session tokens.** Doing so will make previously encrypted database credentials unreadable.

---

## 3. Storage & Media Attachments (S3 / MinIO)

PerGo supports S3-compatible object stores (AWS S3, Cloudflare R2, MinIO, Wasabi) for storing message media attachments:

| Variable | Default Value | Description |
| :--- | :--- | :--- |
| `PERGO_S3_ENDPOINT` | `""` | Custom S3 endpoint URL (e.g. `http://localhost:9000` or `https://<account>.r2.cloudflarestorage.com`). Leave empty for AWS S3. |
| `PERGO_S3_BUCKET` | `""` | Target bucket name. |
| `PERGO_S3_ACCESS_KEY` | `""` | S3 Access Key ID. |
| `PERGO_S3_SECRET_KEY` | `""` | S3 Secret Access Key. |
| `PERGO_S3_REGION` | `us-east-1` | S3 bucket region. |
| `PERGO_S3_USE_SSL` | `true` | Set to `false` when connecting to local unencrypted MinIO (`http`). |

---

## 4. Concurrency & Rate Limiting

| Variable | Default Value | Description |
| :--- | :--- | :--- |
| `PERGO_MAX_WHATSAPP_CONNECTIONS` | `50` | Maximum simultaneous active WhatsApp Web WebSocket sessions allowed per server process. |
| `PERGO_WORKER_CONCURRENCY` | `10` | Number of concurrent NATS JetStream consumer worker goroutines pulling from outbound queue. |
| `PERGO_MAX_QUEUE_DEPTH` | `1000` | Maximum pending messages allowed per workspace queue before triggering HTTP 429 Too Many Requests backpressure rejection. |

---

## 5. Sample Development `.env` File

```env
PERGO_SERVER_PORT=8080
PERGO_DEBUG_PORT=6060
PERGO_EXTERNAL_URL=http://localhost:8080

PERGO_DATABASE_URL=postgres://postgres:postgres@localhost:5433/pergo?sslmode=disable
PERGO_NATS_URL=nats://localhost:4222

PERGO_ADMIN_PASSWORD=pergo-dev-2026
PERGO_MASTER_KEY=pgo_master_secret_key_2026
PERGO_SESSION_SECRET=dev-session-signing-secret-32-chars-long!
PERGO_KEK_BASE64=MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=

PERGO_MAX_QUEUE_DEPTH=1000
PERGO_WORKER_CONCURRENCY=10
```
