---
title: WABA Features Gap Analysis — Inspiration vs PerGo State
date: 2026-07-25
context: gsd-explore session — surveyed Evolution API, Evolution Go, Chatwoot against PerGo's current WABA implementation
---

# WABA Features Gap Analysis — Inspiration vs PerGo State

## Methodology

Researched WABA features across 3 open-source inspiration projects (Evolution API, Evolution Go, Chatwoot) and cross-referenced against PerGo's current implementation, requirements, notes, seeds, and spikes.

## Sources Analyzed

| Project | Focus | Key WABA Features Found |
|---------|-------|------------------------|
| **Evolution API** (TypeScript) | Meta Cloud API direct | Full template CRUD + TTL, business profile, S3 storage, Whisper STT, reactions, multi-transport webhooks |
| **Evolution Go** (Go/whatsmeow) | WhatsApp Web protocol emulation | Carousel, CTA buttons, PIX payments, polls, newsletters, labels, communities, call management |
| **Chatwoot** (Ruby) | Customer support platform | Embedded Signup, health monitoring, SMB echoes, campaigns, CSAT templates, voice calling, HMAC verification, deduplication, phone normalization |

## Gap Classification

### Already Covered in PerGo (.planning/)
- 24h window & status tracking (REQ + note + implemented)
- Meta Flows send + decode (REQ + note)
- Catalog/Commerce (REQ + note)
- Template CRUD + validation + webhooks + sync (REQ + research question)
- Template Builder UI (seed)
- Reactions send + webhook (REQ)
- Message Edit/Revoke webhooks (REQ)
- Business Profile CRUD (REQ)
- Auto Webhook Registration (REQ)
- Analytics sync + consolidated (REQ + seed)
- Embedded Signup (seed)
- Voice Calling (seed)
- Connection Test verify/send (REQ)
- Outbound HMAC signing (spike 014 — validated)
- Campaign Engine UX (spike 020 — validated)

### New Gaps Identified → Crystallized as Artifacts

| Feature | Artifact Type | ID/File |
|---------|--------------|--------|
| Inbound Meta HMAC verification | Requirement | REQ-WABA-HMAC-INBOUND |
| Webhook message deduplication | Requirement | REQ-WABA-WEBHOOK-DEDUP |
| Quoted/reply-to messages | Requirement | REQ-WABA-QUOTED-MSG |
| CTA buttons (URL/Copy/Call) | Requirement | REQ-WABA-CTA-BUTTONS |
| Carousel messages | Requirement | REQ-WABA-CAROUSEL |
| Template TTL | Requirement | REQ-WABA-TEMPLATE-TTL |
| Health monitoring | Requirement | REQ-WABA-HEALTH |
| SMB message echoes | Requirement | REQ-WABA-SMB-ECHOES |
| Per-phone callback override | Requirement | REQ-WABA-PHONE-OVERRIDE |
| S3/MinIO media storage | Seed | s3-media-storage.md |
| Audio speech-to-text | Seed | audio-speech-to-text.md |
| Outbound campaigns | Seed | outbound-campaigns-broadcast.md |
| Carousel Cloud API support | Research | questions-waba-carousel-cloud-api.md |
| Health endpoint details | Research | questions-waba-health-monitoring.md |
| SMB Echoes webhook format | Research | questions-waba-smb-echoes.md |

## Prioritization (CPaaS Lens)

- **Tier 1 (Table Stakes)**: HMAC inbound, dedup, S3 storage — ship before production
- **Tier 2 (API Completeness)**: Quoted msgs, CTA buttons, carousel, template TTL — competitive API
- **Tier 3 (Operational Maturity)**: Health monitoring, SMB echoes, per-phone override
- **Tier 4 (Growth)**: Outbound campaigns — revenue driver
- **Tier 5 (Differentiator)**: Audio STT — unique value proposition
