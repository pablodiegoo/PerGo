# Research Question: WABA Carousel Messages via Cloud API

**Date**: 2026-07-25
**Context**: gsd-explore session — WABA features gap analysis
**Priority**: MEDIUM — blocks REQ-WABA-CAROUSEL implementation

## Question

Does the Meta WhatsApp Cloud API support native carousel messages, or is carousel only available via the WhatsApp Web protocol (whatsmeow protobuf)? If supported, what is the exact payload structure?

## Why This Matters

Evolution Go implements carousel via whatsmeow's `InteractiveMessage_CarouselMessage` protobuf (WhatsApp Web protocol). But PerGo's WABA channel uses the Meta Cloud API (HTTP REST), not the Web protocol. We need to confirm:

1. Does `POST /{phone_number_id}/messages` accept `type: "interactive"` with a carousel subtype?
2. If yes, what's the exact JSON payload structure (cards, headers, buttons per card)?
3. If no, what are the workarounds? (sequential messages? product_list as pseudo-carousel?)
4. Is carousel a beta/limited-access feature on Cloud API?

## Known Information

- Evolution Go uses whatsmeow protobuf `InteractiveMessage_CarouselMessage` — this is Web protocol, not Cloud API
- Meta Cloud API docs list interactive types: `button`, `list`, `product`, `product_list`, `flow`, `cta_url` — carousel is NOT explicitly listed as of v25.0
- Some community reports suggest carousel might be available via `type: "template"` with carousel template category

## Sources to Investigate

- [Meta Cloud API — Interactive Messages](https://developers.facebook.com/docs/whatsapp/cloud-api/messages/interactive-messages)
- [Meta Cloud API — Template Messages](https://developers.facebook.com/docs/whatsapp/cloud-api/messages/template-messages)
- Meta developer community forums for carousel announcements
- WhatsApp Business API changelog for recent additions
