---
title: Meta Embedded Signup (Tech Provider OAuth Flow)
trigger_condition: When REQ-WABA-WEBHOOK-AUTO is implemented and there's demand for zero-friction WABA onboarding
planted_date: 2026-07-25
context: gsd-explore session — WABA features gap analysis (inspiration from Chatwoot & Novu)
---

# Meta Embedded Signup (Tech Provider OAuth Flow)

## Idea

Implement Meta's Embedded Signup flow so WABA account onboarding is a single "Connect with Meta" button click — no copy-pasting of tokens, phone_number_id, or WABA account IDs. The user authenticates via OAuth popup, grants permissions, and PerGo receives all credentials + auto-registers webhooks.

## How It Works (Chatwoot/Novu Pattern)

1. Operator clicks "Connect WhatsApp Business" in PerGo admin
2. Meta OAuth popup opens (Embedded Signup JS SDK)
3. User logs into their Meta Business account, selects/creates WABA
4. Meta returns `access_token`, `phone_number_id`, `waba_id` via callback
5. PerGo stores credentials, auto-registers webhook (REQ-WABA-WEBHOOK-AUTO), syncs templates
6. Connection is ready — zero manual configuration

## Prerequisites

- PerGo instance must have a registered Meta App (App ID + App Secret)
- Meta App must be approved for `whatsapp_business_management` and `whatsapp_business_messaging` scopes
- REQ-WABA-WEBHOOK-AUTO must be working (Level 1 auto-webhook)

## Self-Hosted Complexity

- Each self-hosted PerGo instance needs its own Meta App — can't share one App across instances
- Documentation must guide operators through Meta App creation (one-time setup)
- Consider: could PerGo offer a "PerGo Cloud" Meta App for quick-start, with migration to self-hosted App later?

## Dependencies

- REQ-WABA-WEBHOOK-AUTO (auto-webhook registration)
- Meta Developer documentation for Embedded Signup: https://developers.facebook.com/docs/whatsapp/embedded-signup

## Inspiration

- **Chatwoot**: `WhatsappCloudEmbeddedSignupService` — full OAuth flow with webhook auto-setup
- **Novu**: Tech Provider flow with `NOVU_WHATSAPP_APP_ID`, `NOVU_WHATSAPP_APP_SECRET`, `NOVU_WHATSAPP_CONFIG_ID`
