# Messaging Channels & Providers

PerGo abstracts provider fragmentation behind a unified messaging interface. Rather than writing divergent integration code for every external network, developers send a single JSON payload to `POST /api/v1/messages`. PerGo resolves the correct channel adapter, performs rate-limiting, and routes to fallback channels if primary channels fail.

---

## Supported Providers

| Channel Identifier | Provider | Protocol / Integration | Best Used For |
| :--- | :--- | :--- | :--- |
| `"whatsapp"` | **WhatsApp Web** | Multi-device WebSocket (`whatsmeow`) | Direct conversational messaging without Meta per-message fees or template approval friction. |
| `"whatsapp_cloud"` | **WhatsApp Cloud (WABA)** | Meta Graph REST API + Webhooks | High-volume official business messaging, automated interactive flows, pre-approved templates outside 24h windows. |
| `"telegram"` | **Telegram** | Telegram Bot API (HTTPS + Webhooks) | Fast notifications, support channels, broadcast alerts with zero per-message cost. |

---

## Channel Setup Guides

- **[WhatsApp Web (whatsmeow)](whatsapp-web.md)**: Pairing devices via QR code (Web Console or REST API), session persistence, and anti-ban delay safeguards.
- **[WhatsApp Cloud API (Meta WABA)](whatsapp-cloud.md)**: Meta developer app setup, system user tokens, webhook subscription, and template messaging.
- **[Telegram Bot Integration](telegram.md)**: Creating bots via `@BotFather`, token configuration, and automated webhook lifecycle management.

---

## Connection Identifiers & Slugs

Every configured channel instance inside a workspace is assigned a unique `ConnectionID` (UUID) and an optional human-readable `slug` (e.g. `support-line-1` or `billing-bot`). When sending messages, clients can target a specific connection directly:

```json
{
  "to": "5511999999999",
  "connection_slug": "support-line-1",
  "body": "Hello from support!"
}
```

If `connection_slug` is omitted, PerGo automatically resolves the active default connection for the requested `channel`.
