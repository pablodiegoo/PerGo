# Traefik v3 Reverse Proxy Configuration

PerGo uses [Traefik v3](https://doc.traefik.io/traefik/) as its edge ingress controller in production.

---

## 1. Key Features

- **Automated HTTPS:** Traefik interacts with Let's Encrypt using ACME to obtain and renew SSL/TLS certificates automatically.
- **HTTP to HTTPS Redirection:** Port 80 traffic is redirected to port 443 with a permanent 301 redirect.
- **Docker Dynamic Discovery:** Traefik connects to `/var/run/docker.sock` in read-only mode, discovering backend services and routing rules via Docker container labels.

---

## 2. Docker Labels on PerGo Service

In `docker-compose.prod.yml`, the `pergo` service is annotated with Traefik routing labels:

```yaml
labels:
  - "traefik.enable=true"
  - "traefik.http.routers.pergo.rule=Host(`${DOMAIN}`)"
  - "traefik.http.routers.pergo.entrypoints=websecure"
  - "traefik.http.routers.pergo.tls.certresolver=letsencrypt"
  - "traefik.http.services.pergo.loadbalancer.server.port=8080"
```

Where:
- `Host(${DOMAIN})`: Directs incoming requests matching your configured domain to PerGo.
- `entrypoints=websecure`: Only exposes the service over HTTPS.
- `certresolver=letsencrypt`: Triggers the Let's Encrypt ACME resolver.
- `server.port=8080`: Forward target internal container port.

---

## 3. Security Considerations

- **Insecure API Disabled:** In `docker-compose.prod.yml`, the Traefik API and dashboard are disabled (`--api.insecure=false`, `--api.dashboard=false`) to eliminate unauthorized public exposure.
- **ExposedByDefault=False:** Only containers explicitly labeled with `traefik.enable=true` are exposed publicly; internal databases (PostgreSQL, NATS) have no public ingress.
