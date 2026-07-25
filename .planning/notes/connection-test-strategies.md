---
title: Connection Test Strategies per Channel
date: 2026-07-25
context: gsd-explore session — WABA features, expanded to generic CPaaS pattern
---

# Connection Test Strategies per Channel

Two-level test system: **Verify** (credentials valid?) and **Send** (end-to-end works?).

## Level 1 — Verify (Read-Only, No Message Sent)

| Channel | API Call | What It Returns | Failure Modes |
|---------|---------|-----------------|---------------|
| **WABA** | `GET /v25.0/{phone_number_id}` | Business name, quality rating, status | Invalid token (401), wrong phone_number_id (400) |
| **Telegram** | `getMe` | Bot username, name, can_join_groups | Invalid token (401) |
| **WhatsApp Web** | `whatsmeow.Client.IsConnected()` + `Store.ID` check | Device JID, push name | Session disconnected, not paired |
| **Email (SMTP)** | `EHLO` + `AUTH` handshake (no `MAIL FROM`) | Server capabilities, auth success | Wrong host/port, bad credentials, TLS mismatch |
| **Email (SES)** | `GetSendQuota` | Max24HourSend, SentLast24Hours | Invalid credentials, wrong region |
| **Mautic** | `GET /api/contacts?limit=1` | Contact count (proves API key works) | Invalid API key, wrong base URL |

## Level 2 — Send (End-to-End Test Message)

| Channel | Test Message | Why This Message |
|---------|-------------|-----------------|
| **WABA** | `hello_world` template | Pre-approved by Meta on every WABA account — guaranteed to exist and pass review |
| **Telegram** | Plain text: "✅ PerGo connection test successful" | No setup needed, works with any chat_id |
| **WhatsApp Web** | Plain text: "✅ PerGo connection test successful" | Simplest message type, no template needed |
| **Email (SMTP/SES)** | Branded HTML email: "Your PerGo email connection is working" | Shows rendering works, not just delivery |
| **Mautic** | Plain text email via Mautic API | Validates the full Mautic pipeline |

## Design Notes

- **Verify should be called on connection save** — instant feedback before the operator leaves the form
- **Send requires explicit operator action** — it costs money (WABA conversation) or sends a real message
- **Rate limit Send** — 1 per connection per minute prevents accidental spam
- **UI integration** — admin console should show a "Test" button next to each connection with status indicator
- **Both levels return structured JSON** — same response schema regardless of channel, with channel-specific `provider_info` nested inside
