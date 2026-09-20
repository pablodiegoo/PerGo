# Production Deployment Overview

This section covers deploying PerGo into production environments using Docker Compose, Traefik v3 reverse proxy, and Let's Encrypt automated SSL/TLS certificates.

---

## 1. Production Architecture Topology

```
             Internet (Clients, Meta, Telegram)
                             │
                             ▼ (Port 80 / 443)
┌─────────────────────────────────────────────────────────────┐
│                 Traefik v3 Reverse Proxy                    │
│   - Automated ACME Let's Encrypt TLS Issuance               │
│   - Enforces HTTP (80) -> HTTPS (443) Redirect              │
│   - Dynamic Docker Service Discovery                        │
└────────────────────────────┬────────────────────────────────┘
                             │
                             ▼ (Internal Docker Network)
┌─────────────────────────────────────────────────────────────┐
│                  PerGo Core Data Plane                      │
│   - Distroless minimal image (< 50MB)                       │
│   - REST API, Admin Console, and Inbound Webhooks           │
└──────────────┬──────────────────────────────┬───────────────┘
               │                              │
               ▼                              ▼
┌─────────────────────────────┐┌──────────────────────────────┐
│        PostgreSQL 16        ││        NATS JetStream        │
│   - Persistent data volume  ││   - Durable message streams  │
│   - Goose auto-migrations   ││   - Outbound work queues     │
└─────────────────────────────┘└──────────────────────────────┘
```

---

## 2. Pre-Flight Production Checklist

Before exposing PerGo to live traffic, ensure the following checklist items are satisfied:

1. **DNS A-Record Configuration:**
   - Configure a public DNS A-record pointing your domain (e.g. `api.pergo.example.com`) to your server's public IPv4 address.
2. **Ports Open in Firewall:**
   - Ensure inbound traffic on ports `80` (HTTP challenge) and `443` (HTTPS) is permitted.
   - Ensure port `6060` (debug/pprof) is **blocked** from public access and restricted to internal VPN/monitoring only.
3. **Environment Variables Configured:**
   - `PERGO_EXTERNAL_URL`: Set to the public HTTPS URL (e.g. `https://api.pergo.example.com`). Without HTTPS, Telegram and Meta webhook verification will fail.
   - `PERGO_KEK_BASE64`: Set to a randomly generated 32-byte Base64 key (`openssl rand -base64 32`).
   - `PERGO_ADMIN_PASSWORD`: Strong password for the admin console.
   - `ACME_EMAIL`: Valid administrator email address for Let's Encrypt expiration notifications.

---

## 3. Deployment Guides

- **[Production Docker Compose](docker-compose.md)**: Operating the standalone production stack via `docker-compose.prod.yml`.
- **[Traefik v3 Reverse Proxy](traefik-proxy.md)**: Edge routing, dynamic service discovery, and SSL termination.
- **[SSL/TLS with Let's Encrypt](ssl-tls.md)**: Automated certificate generation and renewal.
