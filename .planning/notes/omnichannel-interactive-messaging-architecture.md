---
title: Omnichannel Interactive Messaging Architecture
date: 2026-07-19
context: Explored WABA JSON-to-Protobuf mapping inspired by Evolution API and Wuzapi.
---

# Omnichannel Interactive Messaging Architecture

## Context
We need to support rich interactive messages (lists, carousels, templates, buttons) on channels like WhatsApp (via WABA or whatsapp-web).

## Decision: The Hybrid Approach
To avoid vendor lock-in for standard features, while still allowing developers to utilize bleeding-edge channel-specific features, we will adopt a **Hybrid Architecture** for the `POST /messages` payload.

### 1. Unified Base Schema
Standard interactive types (like `type: list` or `type: button`) will be defined generically. PerGo will translate these seamlessly into the specific provider's format (e.g., WhatsApp protobufs, Telegram inline keyboards).

### 2. Channel Overrides (The Escape Hatch)
To support advanced, proprietary WABA features (e.g., WhatsApp Flows, Catalogs), the payload will support an escape hatch:
```json
{
  "type": "custom",
  "channel_overrides": {
    "whatsapp": {
      "raw_protobuf_mapping": { ... }
    }
  }
}
```
When `channel_overrides.whatsapp` is provided, PerGo bypasses the generic abstraction and parses the raw WABA schema directly.
