# WhatsApp Web Channel Setup (whatsmeow)

The WhatsApp Web channel enables unofficial direct messaging by emulating a linked multi-device companion client using the open-source [`whatsmeow`](https://github.com/tulir/whatsmeow) library.

---

## 1. Overview & Advantages

- **Zero Per-Message Markups:** Operates without paying Meta template conversation fees.
- **Immediate Deployment:** No Facebook Business verification or Meta app review required.
- **Full Media Support:** Supports sending and receiving text messages, images, audio notes, and documents.
- **Real-Time Pairing:** Links directly by scanning a QR Code generated inside the PerGo Admin UI or retrieved via REST API.

---

## 2. Pairing a Device

### Option A: Using the Native Admin Console
1. Open the PerGo Operator Console at `http://localhost:8080/admin` (or your production URL).
2. Select your **Workspace** and navigate to the **Channels / Devices** tab.
3. Click **Connect New Device** under WhatsApp Web.
4. Open WhatsApp on your primary mobile phone:
   - Go to **Settings** > **Linked Devices** > **Link a Device**.
5. Scan the QR code displayed on your screen. Once paired, the connection status updates to `connected`.

### Option B: Programmatic Headless Pairing via REST API
For external systems (CRMs, onboarding portals) that embed QR pairing directly in their own custom UI:

1. **Initiate Pairing:**
   ```bash
   curl -X POST https://api.pergo.example.com/api/v1/devices/pair \
     -H "Authorization: Bearer <WORKSPACE_API_KEY>" \
     -H "Content-Type: application/json" \
     -d '{"name": "Support WhatsApp"}'
   ```
   **Response (`201 Created`):**
   ```json
   {
     "device_id": "c7a8b9d0-1234-5678-9abc-def012345678",
     "qr_code": "2@b...Base64EncodedQR...",
     "status": "pairing"
   }
   ```
2. **Poll or Stream QR Code Updates:**
   - **Polling:** `GET /api/v1/devices/:id/qr` returns JSON `{ "qr_code": "..." }`.
   - **Server-Sent Events (SSE):** Connect to `GET /api/v1/devices/:id/qr/stream` with `Accept: text/event-stream` for push updates as pairing tokens rotate.

---

## 3. Session Persistence & Reconnection

WhatsApp Web multi-device companion credentials are encrypted with AES-256-GCM using `PERGO_KEK_BASE64` and persisted in PostgreSQL.

- **Reboot Resilience:** When the PerGo process restarts, active sessions automatically reconnect without requiring a new QR code scan.
- **Connection Throttling:** PerGo uses a staged reconnection queue with exponential backoff to avoid hammering WhatsApp servers during mass process restarts.

---

## 4. Safety Primitives & Anti-Ban Protections

Because WhatsApp Web utilizes unofficial protocols, accounts sending bulk messages are subject to spam detection algorithms. PerGo implements strict safety controls:

- **Randomized Dispatch Jitter:** By default, PerGo pauses for 1 to 3 seconds randomly between sequential outbound messages on the same phone number.
- **Queue Limits:** High-burst campaigns are throttled by the campaign broadcaster rate limiter (`rate_limit_per_min`).
- **Media Optimization:** Inbound and outbound media files are verified and transcoded to standard WhatsApp audio/video codecs to prevent client corruption.
