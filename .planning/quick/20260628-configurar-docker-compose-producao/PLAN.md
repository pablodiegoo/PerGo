---
status: in-progress
date: 2026-06-28
description: Configure Docker Compose and Dockerfile for production deployment (Dokploy/VPS)
---

# Plan - Configurar Docker Compose Produção

Prepare the application for production deployment on Dokploy or general virtual private servers (VPS).

## Tasks
1. **Analyze Dockerfile**: Ensure the multi-stage Dockerfile is robust and optimal for production builds.
2. **Optimize docker-compose.yml**:
   - Persist NATS JetStream data via a named volume (`nats_data`) mapped to `/data` in the NATS container.
   - Run NATS with `-js` and explicit `-sd /data` store directory.
   - Parameterize PostgreSQL credentials (`POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`) using defaults, allowing them to be overridden in production environments.
   - Parameterize database connection URL for the `pergo` service to use these PostgreSQL credentials dynamically.
   - Persist PostgreSQL data via named volume `postgres_data` (matching `pgdata` or renaming it).
3. **Document Dokploy deployment**:
   - Provide explicit configuration details on how to set up PerGo on Dokploy.
4. **Verification**: Run compose validation and verify the container builds successfully.
