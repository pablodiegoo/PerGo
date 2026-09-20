# Telegram Bot Channel Setup

PerGo integrates with the official [Telegram Bot API](https://core.telegram.org/bots/api) to dispatch outbound notifications, process conversational inbound replies, and support customer communication threads.

---

## 1. Creating a Telegram Bot

1. Open the Telegram app on desktop or mobile.
2. Search for the official verified bot [@BotFather](https://t.me/BotFather) and start a chat.
3. Send the command:
   ```text
   /newbot
   ```
4. Follow the prompts:
   - **Display Name:** Enter a human-readable title (e.g. `Acme Notifications Gateway`).
   - **Username:** Must end in `bot` (e.g. `acme_alerts_bot`).
5. BotFather will provide an **HTTP API Token** (e.g. `123456789:ABCdefGhIJKlmNoPQRsTUVwxyZ`). Copy this token.

---

## 2. Registering in PerGo

### Option A: Admin Console
1. In the PerGo Admin Console (`http://localhost:8080/admin`), open your Workspace.
2. Go to **Channels** > **Telegram**.
3. Paste the **Bot Token** and assign an optional slug (e.g. `telegram-alerts`).
4. Click **Save Connection**.

### Option B: Programmatic REST API
```bash
curl -X POST https://api.pergo.example.com/api/v1/workspaces/<WORKSPACE_ID>/connections \
  -H "Authorization: Bearer <WORKSPACE_API_KEY>" \
  -H "Content-Type: application/json" \
  -d '{
    "channel": "telegram",
    "slug": "telegram-support",
    "credentials": {
      "bot_token": "123456789:ABCdefGhIJKlmNoPQRsTUVwxyZ"
    }
  }'
```

---

## 3. Webhook Lifecycle Management

When your PerGo instance is configured with a valid HTTPS public URL (`PERGO_EXTERNAL_URL`), PerGo handles the Telegram webhook lifecycle automatically:

- Upon saving the connection, PerGo invokes Telegram's `setWebhook` endpoint pointing to:
  `https://<PERGO_EXTERNAL_URL>/webhooks/telegram/<WORKSPACE_ID>`
- PerGo provisions a cryptographic secret token passed in the `X-Telegram-Bot-Api-Secret-Token` header. Inbound HTTP requests from Telegram lacking this signature are rejected immediately.

### Local Development with Ngrok / Tunnels
Telegram rejects plain `http://` callback endpoints. To test Telegram inbound webhooks locally:
1. Start an HTTPS tunnel (e.g. `ngrok http 8080`).
2. Set `PERGO_EXTERNAL_URL=https://<your-ngrok-subdomain>.ngrok-free.app` in your `.env`.
3. Restart PerGo (`make dev`).
