# PerGo Visual Assets: Screenshots

This directory stores official platform imagery used in `README.md` and repository documentation, compliant with [ADR 0013: Visual Asset Standards and Screenshot Lifecycle](../../adr/0013-visual-asset-standards-and-screenshot-lifecycle.md).

## Official Visual Assets

| Asset Filename | Format | Dimensions | View / Purpose |
| :--- | :--- | :--- | :--- |
| [`inbox-hero.png`](inbox-hero.png) | PNG (159 KB) | `1440 × 900` | Live Omnichannel Chat Inbox displaying multi-turn conversation timeline, chat bubbles, and status indicators |
| [`inbox.webp`](inbox.webp) | WebP (75 KB) | `1440 × 900` | WebP optimized version of `inbox-hero.png` |
| [`dashboard.png`](dashboard.png) | PNG (119 KB) | `1440 × 900` | Operator Dashboard showcasing real-time delivery telemetry, workspace metrics, and system health |
| [`dashboard.webp`](dashboard.webp) | WebP (58 KB) | `1440 × 900` | WebP optimized version of `dashboard.png` |
| [`devices-qr.png`](devices-qr.png) | PNG (145 KB) | `1440 × 900` | Device Connections page illustrating WhatsApp Web QR Code pairing dialog and active channel statuses |
| [`connections.webp`](connections.webp) | WebP (38 KB) | `1440 × 900` | WebP optimized version of `devices-qr.png` |
| [`campaigns.png`](campaigns.png) | PNG (124 KB) | `1440 × 900` | Broadcast Campaign Manager with token-bucket dispatch controls, contact tag resolution, and campaign progress |
| [`campaigns.webp`](campaigns.webp) | WebP (61 KB) | `1440 × 900` | WebP optimized version of `campaigns.png` |
| [`api-docs.png`](api-docs.png) | PNG (247 KB) | `1440 × 900` | Embedded interactive Scalar OpenAPI 3.1 portal (`/docs`) |
| [`scalar-docs.webp`](scalar-docs.webp) | WebP (117 KB) | `1440 × 900` | WebP optimized version of `api-docs.png` |
| [`../pergo-banner.svg`](../pergo-banner.svg) | SVG (14 KB) | `1200 × 360` | Modern high-contrast repository header banner |

## Asset Standards & Governance

- **Viewport**: Strictly `1440 × 900` px (16:10 desktop aspect ratio).
- **PII Isolation**: 100% synthetic deterministic mock fixtures generated via `cmd/pergo-seed`. Zero real phone numbers, real customer conversations, or production secrets.
- **File Optimization**: All images remain under 250 KB (PNG) and under 120 KB (WebP).
- **Automation Pipeline**: Re-capture all assets at any time by running:
  ```bash
  make screenshots
  # or directly:
  node scripts/capture_screenshots.js
  ```
- **Lifecycle Policy**: Pull requests that modify UI templates (`templates/**/*.templ`) or CSS must run `make screenshots` to keep documentation imagery in sync with code.
