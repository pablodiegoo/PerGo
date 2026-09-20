# Interactive Scalar Developer Portal

PerGo embeds a modern, interactive API reference portal powered by [Scalar](https://scalar.com/). It allows engineers to browse OpenAPI 3.1 specifications, inspect schemas, generate client code in multiple programming languages, and test live HTTP requests directly from their web browser.

---

## 1. Accessing the Portal

The portal is hosted directly by the PerGo binary and is accessible without any external internet connection:

- **Browser URL:** `http://localhost:8080/docs` (or `https://<YOUR_PERGO_DOMAIN>/docs`)
- **OpenAPI 3.1 JSON:** `GET /api/openapi.json`
- **OpenAPI 3.1 YAML:** `GET /docs/openapi.yaml`

---

## 2. Architecture & Offline Capability

Unlike traditional API portals that load scripts and styling from external CDNs, PerGo's Scalar implementation is self-contained:

1. **Zero External CDN Dependencies:** The Scalar JavaScript runtime (`scalar.js`) is vendored locally and served by the Go server.
2. **Embedded OpenAPI Specifications:** The OpenAPI definitions are embedded directly into the Go binary at compile time via `//go:embed`.
3. **Air-Gapped & Offline Ready:** The portal renders and operates seamlessly in air-gapped corporate networks, isolated VPCs, and offline developer laptops.

---

## 3. Testing API Calls in the Portal

1. Open `https://<YOUR_PERGO_DOMAIN>/docs` in your browser.
2. In the top right corner, click **Authentication** (or **Authorize**).
3. Enter your workspace API key in the `BearerAuth` input.
4. Select any endpoint (e.g. `POST /api/v1/messages`), customize the request body, and click **Test Request** to see real server responses.
