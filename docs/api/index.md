# PerGo REST API Reference

PerGo provides a standardized, unified REST API for programmatic omnichannel messaging, tenant workspace provisioning, device lifecycle management, and outbound webhook delivery.

---

## 1. Base URL & Versioning

All API requests target the `/api/v1` namespace:

- **Production:** `https://<YOUR_PERGO_DOMAIN>/api/v1`
- **Local Development:** `http://localhost:8080/api/v1`

---

## 2. Authentication Schemes

PerGo employs two distinct authentication layers depending on the operation scope:

### Workspace Bearer Authentication (Standard)
Most operations (sending messages, managing contacts, scheduling campaigns, viewing delivery statuses) are scoped to a tenant workspace. Authenticate using a workspace API key:

```http
Authorization: Bearer pgo_live_8f3d1b9a7c2e4f0a1b2c3d4e5f6a7b8c
```

API keys can be generated in the Admin Console (`/admin`) or via the programmatic workspace provisioning endpoint.

### Master Key Authentication (Administrative / Multi-Tenant)
Administrative operations such as provisioning new workspace tenants (`POST /api/v1/workspaces`) or listing all workspaces require the master secret configured in `PERGO_MASTER_KEY`:

```http
Authorization: Bearer <PERGO_MASTER_KEY>
```
or via the custom header:
```http
X-Master-Key: <PERGO_MASTER_KEY>
```

---

## 3. Standard HTTP Response Codes

| Status Code | Meaning | Description |
| :--- | :--- | :--- |
| `200 OK` | Success | Synchronous query succeeded (e.g. device status, workspace list). |
| `201 Created` | Created | Resource provisioned successfully (e.g. new workspace, campaign created). |
| `202 Accepted` | Accepted | Message accepted, validated, and placed onto the durable NATS work queue. |
| `400 Bad Request` | Invalid Body | Malformed JSON or missing required fields. |
| `401 Unauthorized` | Auth Failed | Invalid, missing, or revoked API key. |
| `403 Forbidden` | Access Denied | Master key required or workspace mismatch. |
| `404 Not Found` | Not Found | Requested workspace, connection, or device does not exist. |
| `422 Unprocessable` | Validation Error | Semantic validation failure (e.g. campaign with no recipients). |
| `429 Too Many Requests` | Rate Limited / Backpressure | Workspace queue depth exceeded 1,000 pending messages or rate limiter tripped. |
| `500 / 503` | Server Error | Internal server or broker communication error. |

---

## 4. API Guides

- **[Endpoints Reference](endpoints.md)**: Exhaustive documentation of all REST endpoints and JSON payloads.
- **[Webhook Security Signatures](webhook-signatures.md)**: Cryptographic verification of outbound webhooks (`X-PerGo-Signature`).
- **[Interactive Scalar Portal](scalar-portal.md)**: In-browser interactive OpenAPI 3.1 documentation portal available at `/docs`.
