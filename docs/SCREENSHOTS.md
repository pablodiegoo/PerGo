# Screenshot Capture & Visual Asset Guidelines

This document establishes the practical step-by-step procedure for generating, capturing, and updating official UI visual assets for the PerGo repository, in accordance with [ADR 0013: Visual Asset Standards and Screenshot Lifecycle](adr/0013-visual-asset-standards-and-screenshot-lifecycle.md).

---

## 1. Quick Start: Seeding the Mock Database

All official screenshots must be taken from deterministic mock fixtures seeded via `cmd/pergo-seed`. This guarantees realistic data density with **100% PII isolation** (no real phone numbers, real customer conversations, or sensitive tokens).

### Step 1: Ensure Local PostgreSQL & NATS Are Running

If using the shared development infrastructure (`devInfra`):
```bash
# From Startup repository root or PerGo directory:
make infra
```
Ensure PostgreSQL is healthy at `localhost:5432` with database `pergo_db` (or as configured in `.env`).

### Step 2: Execute `pergo-seed`

The seed tool bootstraps a dedicated demonstration workspace (`PerGo Demo`), mock channel connections (WhatsApp Cloud, WhatsApp Web, Telegram), tags, rich business contacts, message templates, campaigns with recipient metrics, and multi-turn conversational threads:

```bash
# Inside the PerGo repository directory:
go run ./cmd/pergo-seed
```

*Note: `cmd/pergo-seed` automatically falls back to deterministic mock WABA credentials when live Meta credentials (`ACCESS_TOKEN`, `PHONE_NUMBER_ID`, `WHATSAPP_BUSINESS_ACCOUNT_ID`) are not provided in the environment. No third-party API accounts or webhooks are needed.*

### Step 3: Launch PerGo in Development Mode

```bash
# Run PerGo with live hot-reloading:
make dev
# Alternatively:
go run ./cmd/pergo
```

The application will be accessible at:
- **Admin Console**: [http://localhost:8080/admin](http://localhost:8080/admin) (Default password: `troque-esta-senha` or value of `PERGO_ADMIN_PASSWORD`)
- **API Documentation**: [http://localhost:8080/docs](http://localhost:8080/docs)
- **Health Check**: [http://localhost:8080/healthz](http://localhost:8080/healthz)

---

## 2. Standard Viewport & Browser Configuration

To maintain visual uniformity across documentation, all screenshots must adhere to the following specifications:

| Parameter | Standard Value | Rationale |
| :--- | :--- | :--- |
| **Viewport Resolution** | `1440 × 900` px | Standard desktop ratio (16:10), optimal for sidebar + content density |
| **Device Scale Factor** | `1.0` (100% zoom) | Avoids blurry raster scaling or disproportionate font rendering |
| **Browser Environment** | Chromium / Google Chrome | Consistent Blink typography and SVG antialiasing |
| **Theme** | Light Mode (default) | High contrast for documentation readability across light/dark GitHub themes |
| **Browser Extensions** | Disabled / Incognito | Eliminates external extensions, translation banners, or custom scrollbars |

### Setting Up Chrome DevTools Device Mode

1. Open Google Chrome and navigate to [http://localhost:8080/admin](http://localhost:8080/admin).
2. Press `F12` or `Ctrl + Shift + I` (`Cmd + Option + I` on macOS) to open Chrome DevTools.
3. Click the **Toggle device toolbar** icon (`Ctrl + Shift + M` / `Cmd + Shift + M`).
4. In the top dimension inputs, set:
   - Width: `1440`
   - Height: `900`
   - Zoom: `100%`
5. Press `F5` to reload the page at the target viewport.

---

## 3. Key Views & Capture Instructions

Navigate to each route, verify that the seeded mock data is displayed cleanly, and capture the screenshot:

### 1. Live Omnichannel Inbox (`inbox.webp`)
- **Route**: `http://localhost:8080/admin/inbox`
- **Focus**: Click on the conversation with **Ana Clara Silva** so the middle list displays all active chats with unread badges, and the right pane shows the multi-turn thread with message timestamps.
- **Capture**: In DevTools, press `Ctrl + Shift + P` (`Cmd + Shift + P`) and type `Capture screenshot`.

### 2. Campaign Broadcaster (`campaigns.webp`)
- **Route**: `http://localhost:8080/admin/campaigns`
- **Focus**: Displays the list of campaigns ("Campanha Boas-Vindas Q3", "Pesquisa Satisfação 2026") showing completion status, recipient progress bars, batch size, and channel badges.
- **Capture**: In DevTools, run `Capture screenshot`.

### 3. Channel Connections (`connections.webp`)
- **Route**: `http://localhost:8080/admin/connections`
- **Focus**: Displays connected WABA Cloud, WhatsApp Web (with QR status), and Telegram channel connections.
- **Capture**: In DevTools, run `Capture screenshot`.

### 4. Contact Management & Segmentation (`contacts.webp`)
- **Route**: `http://localhost:8080/admin/contacts`
- **Focus**: Displays the contact table showing names, multi-channel identities, assigned colored tags (VIP, Cliente Ativo, Lead Qualificado), and custom JSON attributes.
- **Capture**: In DevTools, run `Capture screenshot`.

### 5. WABA Template Manager (`templates.webp`)
- **Route**: `http://localhost:8080/admin/templates`
- **Focus**: Displays pre-approved WhatsApp message templates (`boas_vindas_onboarding`, `confirmacao_agendamento`, `promocao_upgrade`) with category badges (UTILITY, MARKETING) and green quality indicators.
- **Capture**: In DevTools, run `Capture screenshot`.

### 6. Interactive Scalar API Explorer (`scalar-docs.webp`)
- **Route**: `http://localhost:8080/docs`
- **Focus**: Displays the OpenAPI 3.1 Scalar interactive documentation with endpoint categories (`/api/v1/messages`, `/api/v1/campaigns`, `/api/v1/connections`).
- **Capture**: In DevTools, run `Capture screenshot`.

### 7. Operator Dashboard & Telemetry (`dashboard.webp`)
- **Route**: `http://localhost:8080/admin`
- **Focus**: Shows overview metrics, active connection health cards, and recent delivery throughput.
- **Capture**: In DevTools, run `Capture screenshot`.

---

## 4. Asset Optimization & File Formats

Save raw captures and convert them to WebP using Google's `cwebp` CLI (or an equivalent image processing tool):

```bash
# Install webp tools if needed:
# Ubuntu/Debian: sudo apt install webp
# macOS: brew install webp

# Convert PNG to optimized WebP (quality 85):
cwebp -q 85 raw_inbox.png -o docs/assets/screenshots/inbox.webp
cwebp -q 85 raw_campaigns.png -o docs/assets/screenshots/campaigns.webp
cwebp -q 85 raw_connections.png -o docs/assets/screenshots/connections.webp
cwebp -q 85 raw_contacts.png -o docs/assets/screenshots/contacts.webp
cwebp -q 85 raw_templates.png -o docs/assets/screenshots/templates.webp
cwebp -q 85 raw_scalar_docs.png -o docs/assets/screenshots/scalar-docs.webp
cwebp -q 85 raw_dashboard.png -o docs/assets/screenshots/dashboard.webp
```

### Verification Checklist:
- [ ] Viewport resolution is exactly `1440x900`.
- [ ] File format is `.webp` (or crushed `.png`).
- [ ] File size is under 300 KB per asset.
- [ ] Zero real personal identifying information (PII) is visible.
- [ ] Image filename matches the canonical table in `docs/assets/screenshots/README.md`.

---

## 5. UI Contributor Lifecycle Gate

Whenever a pull request introduces changes to:
- Templ components in `templates/**/*.templ`
- Layout styles, navigation sidebars, or CSS in `static/`
- Major admin workflows

The author must:
1. Re-run `go run ./cmd/pergo-seed`.
2. Inspect if any affected view's visual appearance changed significantly.
3. Update the corresponding `.webp` file in `docs/assets/screenshots/`.
4. Check the `[x] UI changed: screenshots updated according to docs/SCREENSHOTS.md` item in the pull request description.
