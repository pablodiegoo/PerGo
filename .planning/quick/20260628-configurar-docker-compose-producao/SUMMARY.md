---
status: complete
date: 2026-06-28
description: Configured production-ready Docker Compose structure with NATS JetStream data persistence and parameterized PostgreSQL credentials.
---

# Quick Task: configurar-docker-compose-producao - Summary

## Work Done
1. **NATS JetStream Data Persistence**:
   - Added a named volume `natsdata` to the compose file.
   - Configured the NATS container to run with command `["-js", "-sd", "/data"]` and mapped the `/data` directory inside the container to the `natsdata` volume. This ensures all message stream state is persisted across container recreations.
2. **Parameterized PostgreSQL Credentials**:
   - Replaced hardcoded PostgreSQL username, password, and database name with environment variables `${POSTGRES_USER}`, `${POSTGRES_PASSWORD}`, and `${POSTGRES_DB}` with default values fallback (`postgres`/`postgres`/`pergo`).
   - Cleaned up healthcheck and `PERGO_DATABASE_URL` strings to dynamically use these variables.
3. **Verification**:
   - Verified that the `docker-compose.yml` config evaluates correctly without syntax errors.
