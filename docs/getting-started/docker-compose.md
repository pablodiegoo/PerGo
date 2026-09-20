# Local Docker Compose Infrastructure

PerGo provides Docker Compose configurations for running essential dependencies locally during development.

---

## 1. Overview of Services

When developing PerGo locally, you can spin up background backing services without installing them on your host OS:

```
┌────────────────────────────────────────────────────────┐
│                   Docker Compose                        │
│                                                        │
│   ┌────────────────────┐      ┌────────────────────┐   │
│   │   PostgreSQL 16    │      │   NATS JetStream   │   │
│   │   Port: 5433       │      │   Port: 4222       │   │
│   └────────────────────┘      └────────────────────┘   │
│                                                        │
│   ┌────────────────────┐      ┌────────────────────┐   │
│   │   MinIO (S3)       │      │   Mailpit          │   │
│   │   Port: 9000/9001  │      │   Port: 1025/8025  │   │
│   └────────────────────┘      └────────────────────┘   │
└────────────────────────────────────────────────────────┘
```

- **PostgreSQL 16 (`postgres`):** Stores tenant workspaces, encrypted credentials, contact directories, campaigns, and partitioned audit records. Mapped to host port `5433` to prevent conflicts with standard host PostgreSQL daemons.
- **NATS JetStream (`nats`):** High-throughput distributed message broker that provides durable at-least-once queueing for outbound dispatches and inbound webhook workers. Port `4222` (client connection) and `8222` (HTTP monitoring).
- **MinIO S3 (Optional):** S3-compatible object storage for inbound/outbound audio, images, and document attachments.
- **Mailpit (Optional):** Local email testing server for email channel adapters.

---

## 2. Managing Local Infrastructure

Use the provided Makefile targets from the project root:

### Start Infrastructure
```bash
make infra
```
Starts PostgreSQL, NATS, and supporting development containers in detached mode.

### Stop Infrastructure
```bash
make infra-down
```
Stops the running containers without removing data volumes.

---

## 3. Database Credentials & Port Mapping

Default local configuration used by PerGo:

| Parameter | Local Docker Value |
| :--- | :--- |
| **Host** | `localhost` |
| **Port** | `5433` (mapped from container `5432`) |
| **Database** | `pergo` |
| **User** | `postgres` |
| **Password** | `postgres` |
| **Connection String** | `postgres://postgres:postgres@localhost:5433/pergo?sslmode=disable` |

---

## 4. NATS JetStream Setup

NATS runs with JetStream enabled (`-js`), storing streams in a persistent Docker volume (`natsdata`). 

- **Client Port:** `localhost:4222`
- **Monitoring Dashboard:** `http://localhost:8222`
- **Stream Name:** `PERGO_MESSAGES` (auto-provisioned by PerGo on startup)

To inspect streams using the NATS CLI:
```bash
nats --server=localhost:4222 stream list
```

---

## 5. Standalone Containerized Application Run

If you want to run the entire stack including the PerGo daemon inside Docker Compose:

```bash
make prod
```
This builds the multi-stage Docker image and brings up PostgreSQL, NATS, and the PerGo service attached to the internal network.

To view application logs:
```bash
make prod-logs
```

To stop all containers:
```bash
make prod-down
```

For production deployments with Traefik v3 and automated TLS, see the [Production Deployment Guide](../deployment/docker-compose.md).
