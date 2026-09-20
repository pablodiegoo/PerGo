# 1-Click VPS Quickstart

PerGo includes an automated, production-ready installer (`install.sh`) designed for quick provisioning on clean Linux Virtual Private Servers (VPS) or cloud VMs (Ubuntu 22.04+, Debian 12+, Rocky Linux, etc.).

---

## 1. Quick One-Liner Install

Execute the following command on your server with `root` or `sudo` privileges:

```bash
curl -fsSL https://raw.githubusercontent.com/pablodiegoo/PerGo/main/install.sh | bash
```

The script interactively prompts for your:
1. **Public Domain Name** (e.g. `api.pergo.example.com` or `cpaas.mycompany.com`)
2. **Email Address** (for Let's Encrypt TLS certificate expiration notices)
3. **Admin Password** (generated automatically if left blank)

---

## 2. Non-Interactive / Scripted Installation

For automated orchestration (e.g., cloud-init, Ansible, Terraform), you can pass command-line arguments:

```bash
curl -fsSL https://raw.githubusercontent.com/pablodiegoo/PerGo/main/install.sh | bash -s -- \
  --domain api.pergo.example.com \
  --email ops@example.com \
  --password "MyStrongSecretPass2026!" \
  --dir /opt/pergo
```

### Supported CLI Flags

| Flag | Long Option | Description | Default |
| :--- | :--- | :--- | :--- |
| `-d` | `--domain` | Target fully qualified domain name (FQDN) | Interactive prompt |
| `-e` | `--email` | Contact email for Let's Encrypt TLS notices | Interactive prompt |
| `-p` | `--password` | Custom administrator password | Auto-generated (32 chars) |
| | `--dir` | Installation target directory | Current directory or `/opt/pergo` |
| `-y` | `--non-interactive` | Run without confirmation prompts | Disabled |
| `-h` | `--help` | Print usage help | N/A |

---

## 3. What the Installer Automates

The 1-Click installer executes the following provisioning stages:

1. **System Dependency Validation:** Confirms Docker engine (>= 24.0) and Docker Compose plugin are installed; installs them automatically via official packages if missing.
2. **Firewall & Port Check:** Verifies ports `80` (HTTP ACME challenge) and `443` (HTTPS) are available.
3. **Secret Generation:**
   - Generates cryptographically secure Base64 32-byte `PERGO_KEK_BASE64` for AES-256-GCM envelope encryption.
   - Generates random PostgreSQL and session secret keys.
4. **Configuration Assembly:** Creates `.env` and `docker-compose.prod.yml` configured for your domain.
5. **Stack Initialization:** Brings up the autonomous production stack:
   - **Traefik v3**: Reverse proxy with automatic Let's Encrypt SSL/TLS issuance and HTTP-to-HTTPS redirect.
   - **PostgreSQL 16**: Embedded migrations applied on boot.
   - **NATS JetStream**: Durable messaging work queue.
   - **PerGo**: Distroless production container.
6. **Health Verification:** Polls `https://<domain>/health` until the service reports healthy status.

---

## 4. Post-Installation Verification

Once installation succeeds, your credentials and endpoints will be displayed in the terminal:

```text
=============================================================================
  PerGo Production Installation Complete!
=============================================================================
  Operator Console: https://api.pergo.example.com/admin
  REST API Base:    https://api.pergo.example.com/api/v1
  OpenAPI & Scalar: https://api.pergo.example.com/docs
  Admin Password:   <your_generated_password>
=============================================================================
```

To manage the installation on your VPS:
```bash
cd /opt/pergo

# View live service logs
docker compose -f docker-compose.prod.yml logs -f pergo

# Restart the stack
docker compose -f docker-compose.prod.yml restart

# Stop the stack
docker compose -f docker-compose.prod.yml down
```

For advanced production tuning, see the [Production Deployment](../deployment/index.md) section.
