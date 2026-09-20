# Report 02: PerGo Capabilities Matrix vs. Market Gaps

## 1. Context & Architecture Baseline

**PerGo** is an open-source, self-hosted Omnichannel CPaaS Gateway engineered in Go (Echo v5, Templ + HTMX, NATS JetStream, PostgreSQL pgx/v5).

The core platform features:
1. **Integrated Multi-Channel Architecture**:
   - WhatsApp Web (unofficial via `whatsmeow`)
   - WhatsApp Cloud API (official Meta WABA)
   - Telegram Bot API
   - Instagram Direct DM
   - Email (SMTP, Amazon SES, Mautic with open/click tracking)
2. **High-Throughput Messaging Infrastructure**:
   - Unified ingress endpoint `POST /api/v1/messages`
   - Resilience & durability via NATS JetStream (at-least-once delivery, retries, Dead Letter Queue)
   - Connection routing via workspace-scoped slugs
   - Staggered dispatch (1–3s randomized jitter) for unofficial channel anti-ban protection
   - Automated intelligent fallback engine across secondary channels
3. **Enterprise Official WABA Capabilities**:
   - 24-hour customer service window management (tracking `last_inbound_at`, pre-flight HTTP 422 enforcement)
   - Full WABA message template lifecycle (CRUD, Graph API sync, visual previewer, strict variable validation)
   - Support for **Meta Flows** (HMAC tokens, `nfm_reply` parsing, RSA/AES Data Exchange encryption)
   - Support for **WhatsApp Commerce Catalogs** (`product` and `product_list` dispatches, converting order webhooks into `order.created` events)
4. **Extensible Integrations**:
   - Native bidirectional sync with **Chatwoot** (multi-agent live chat)
   - Native asynchronous connector with **Typebot** (flow-based chatbot builder)
   - **Stateful Handoff Routing** engine (`bot_active` / `bot_paused_at` state tracking with inactivity cooldowns)
   - Server-rendered Templ/HTMX operator console with integrated conversational inbox

---

## 2. Feature Comparison Matrix (PerGo vs. Competitors)

| Capability / Feature | PerGo | Twilio | 360dialog | Take Blip | Botconversa | Octadesk | Wati |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: | :---: |
| **WhatsApp Web (Unofficial QR Code)** | ✅ Native (`whatsmeow`) | ❌ | ❌ | ❌ | ✅ | ⚠️ Limited | ❌ |
| **WhatsApp Cloud API (Official WABA)** | ✅ Native | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| **Telegram & Instagram DM** | ✅ Native | ✅ (via Channels) | ❌ | ✅ | ❌ | ✅ | ❌ |
| **Email SMTP / SES / Mautic + Pixel** | ✅ Native | ✅ (SendGrid) | ❌ | ⚠️ Integration | ❌ | ✅ | ❌ |
| **Unified API Ingestion (`POST /messages`)** | ✅ Native | ✅ | ✅ | ✅ | ⚠️ Limited | ⚠️ Limited | ⚠️ Limited |
| **Connection Slugs Routing** | ✅ Native | ❌ (uses SIDs) | ❌ | ❌ (uses GUIDs) | ❌ | ❌ | ❌ |
| **Intelligent Automated Fallbacks** | ✅ Native | ⚠️ Custom code | ❌ | ⚠️ Via Flow | ❌ | ❌ | ❌ |
| **Meta Flows & Commerce Catalogs** | ✅ Native | ⚠️ Custom code | ⚠️ Pass-through | ✅ Native | ❌ | ❌ | ⚠️ Basic |
| **Visual Drag-and-Drop Flow Builder** | ❌ (uses Typebot) | ✅ (Studio) | ❌ | ✅ (Blip Builder) | ✅ (Canvas) | ⚠️ Basic bot | ⚠️ Basic bot |
| **Multi-Agent Helpdesk / SLA Tickets** | ⚠️ Basic (uses Chatwoot) | ❌ (uses Flex) | ❌ | ✅ (Blip Desk) | ⚠️ Simplified | ✅ (Octadesk) | ✅ (Team Inbox) |
| **Broadcast / Mass Campaigns** | ✅ Native Engine | ❌ (Twilio Engage) | ❌ | ✅ | ✅ (Broadcaster) | ⚠️ Basic | ✅ |
| **Zero Per-Message Markup (Self-Hosted)** | ✅ 100% Free on Infra | ❌ ($/msg) | ⚠️ Flat fee | ❌ (High Markup) | ✅ Flat plan | ❌ | ❌ |

---

## 3. Gap Analysis & Strategic Opportunities

### Gap 1: Visual Drag-and-Drop Flow Builder
- **Description**: In-dashboard visual bot flow canvas (similar to Botconversa or Blip Builder).
- **Resolution**: PerGo delegates visual bot design to **Typebot** via native webhook and session synchronization.

### Gap 2: Advanced Multi-Agent Live Chat & Helpdesk Ticketing
- **Description**: Departmental queues, strict first-response SLA enforcement, agent transfer, internal private notes.
- **Resolution**: PerGo embeds an operator inbox and integrates bidirectionally with **Chatwoot**.

### Gap 3: Mass Broadcasting & Campaign Scheduling
- **Description**: Admin UI for uploading contact CSVs, scheduling campaigns, rate limiting dispatches, pause/resume controls.
- **Resolution**: Delivered in PerGo v1.8 via the Broadcaster Engine and Campaign Manager.

### Gap 4: Developer Portal & Client SDKs
- **Description**: Interactive documentation, client libraries (Go, TypeScript, Python), API key scoping, HMAC webhook signing.
- **Resolution**: Delivered via the Scalar developer portal at `/docs` and HMAC-SHA256 request verification.

---
*Generated as part of the PerGo Wayfinder research ecosystem.*
