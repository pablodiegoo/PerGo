# SSL/TLS & Automated Let's Encrypt Certificates

Production deployments of PerGo require valid HTTPS certificates. Messaging providers (Telegram Bot API and Meta WhatsApp Cloud) reject unencrypted HTTP webhook callback URLs.

---

## 1. How Automated TLS Works

PerGo's production deployment automates TLS certificate generation using the **ACME HTTP-01 Challenge**:

```
1. Client/Provider connects -> https://api.pergo.example.com
2. Traefik handles TLS handshake.
   - If certificate does not exist, Traefik requests certificate from Let's Encrypt.
   - Let's Encrypt makes HTTP verification request to port 80:
     http://api.pergo.example.com/.well-known/acme-challenge/<TOKEN>
   - Traefik answers challenge directly.
   - Let's Encrypt issues certificate.
3. Traefik stores certificate in /letsencrypt/acme.json volume and completes handshake.
```

---

## 2. Configuration Parameters

In your production `.env` file, specify:

```env
# Domain name matching DNS A-record
DOMAIN=api.pergo.example.com

# Email address for Let's Encrypt account registration & renewal alerts
ACME_EMAIL=admin@example.com
```

---

## 3. Certificate Storage & Renewal

- Certificates are stored encrypted inside the named volume `traefik-certificates` at path `/letsencrypt/acme.json` with strict `0600` file permissions.
- Traefik automatically renews certificates 30 days before expiration without requiring service restarts or manual intervention.

---

## 4. Troubleshooting Certificate Issuance

If Traefik fails to acquire a certificate:

1. **Verify DNS Resolution:** Ensure your domain resolves to your server's public IP from external networks (`dig +short api.pergo.example.com`).
2. **Check Port 80 Access:** Verify port 80 is not blocked by cloud provider security groups (AWS Security Groups, DigitalOcean Cloud Firewall, etc.).
3. **Inspect Traefik Logs:**
   ```bash
   docker compose -f docker-compose.prod.yml logs traefik
   ```
   Look for ACME challenge errors or rate limiting notices from Let's Encrypt.
