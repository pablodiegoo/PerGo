---
title: Outbound Campaigns & Broadcast Engine
trigger_condition: When Tier 1 hardening (HMAC, dedup, S3) and Tier 2 message types (CTA, carousel, quoted) are stable
planted_date: 2026-07-25
context: gsd-explore session — WABA features gap analysis. Spike 020 (campaign-engine) already validated UI/UX patterns.
scope: Dedicated milestone — too large for a single phase
---

# Outbound Campaigns & Broadcast Engine

## Idea

Build a campaign engine that allows businesses to broadcast approved WABA templates to segmented contact lists — the core revenue driver for any CPaaS.

## What Spike 020 Already Validated

- CSV mailing list upload with cleaning/scrubbing pipeline
- Dynamic variable mapping (column → template placeholder)
- Estimated dispatch duration calculator (batch size × delay)
- Batch dispatch simulation with progress tracking
- Log architecture decision: **Enriched Outbound Logs** (single table with `campaign_id` column + indexes)

## What Still Needs Design

- **Contact list management**: persistent lists vs one-time CSV uploads vs API-driven lists
- **Segmentation**: label-based filtering, custom attributes, opt-in/opt-out tracking
- **Throttling intelligence**: auto-adjust batch rate based on Meta messaging tier (TIER_1K → TIER_10K → TIER_100K → UNLIMITED)
- **Campaign lifecycle**: draft → scheduled → running → paused → completed → cancelled
- **Progress webhooks**: `campaign.progress` events with sent/delivered/failed counts
- **Rate limit awareness**: respect Meta's per-phone rate limits, back off on 429s
- **Campaign analytics**: delivery rate, read rate, failure breakdown per campaign
- **REST API**: `POST /campaigns`, `GET /campaigns/:id`, `PUT /campaigns/:id/pause`, etc.
- **Admin UI**: campaign dashboard with status, progress, and drill-down

## Suggested Milestone Structure

1. **Phase A**: Contact lists API + CSV import + cleaning pipeline
2. **Phase B**: Campaign creation + scheduling + throttled dispatch via NATS
3. **Phase C**: Progress tracking + webhooks + analytics
4. **Phase D**: Admin UI campaign dashboard

## Dependencies

- REQ-WABA-HEALTH (need messaging tier for throttling)
- REQ-WABA-TEMPLATE-* (templates must be fully managed)
- Enriched Outbound Logs pattern (spike 020 decision)

## Anti-patterns

- Don't send all messages at once — Meta will rate-limit or ban the number
- Don't skip opt-out tracking — LGPD/GDPR requires consent management
- Don't build a marketing automation platform — PerGo exposes primitives, not workflows
