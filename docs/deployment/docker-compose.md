# Production Docker Compose

PerGo provides an all-in-one production Docker Compose manifest (`docker-compose.prod.yml`) that runs Traefik v3, PostgreSQL 16, NATS JetStream, and the PerGo service.

---

## 1. Quick Launch with Make

Create your production `.env` file (see [Configuration Guide](../getting-started/configuration.md)) and start the production stack:

```bash
make prod-stack
```

### Management Commands

```bash
# View live application logs
make prod-stack-logs

# Stop the production stack
make prod-stack-down
```

---

## 2. Docker Compose File Details (`docker-compose.prod.yml`)

The file specifies four services:

1. **`traefik` (`traefik:v3.1`)**: Listens on ports `80` and `443`, handles automated ACME Let's Encrypt certificates, and proxies traffic to PerGo.
2. **`postgres` (`postgres:16-alpine`)**: Stores application data in named volume `postgres-data`.
3. **`nats` (`nats:2.10-alpine`)**: Runs with JetStream enabled (`-js`) and stores persistent streams in `nats-data`.
4. **`pergo`**: Multi-stage build resulting in a distroless runtime container.

---

## 3. Persistent Volumes

To ensure zero data loss during container upgrades, data is stored in named Docker volumes:

- `traefik-certificates`: Stores `/letsencrypt/acme.json` containing TLS private keys and certificates.
- `postgres-data`: Stores PostgreSQL database tables and transaction logs.
- `nats-data`: Stores durable JetStream message streams.

To backup database state:
```bash
docker compose -f docker-compose.prod.yml exec -T postgres pg_dump -U pergo pergo > pergo_backup_$(date +%Y%m%d).sql
```
