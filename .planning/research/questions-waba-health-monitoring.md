# Research Question: WABA Health Monitoring Endpoint Details

**Date**: 2026-07-25
**Context**: gsd-explore session — WABA features gap analysis
**Priority**: MEDIUM — informs REQ-WABA-HEALTH implementation

## Question

What are the exact Meta Graph API endpoints, fields, permissions, and rate limits for querying WABA phone number health and account status?

## What We Need

1. **Exact endpoint and fields**: Which Graph API call returns `quality_rating`, `messaging_limit`, `code_verification_status`, `throughput`, `platform_type`, and `status`? Is it `GET /{phone_number_id}?fields=...` or a separate endpoint?
2. **Permission requirements**: Does the access token need `whatsapp_business_management` scope, or does `whatsapp_business_messaging` suffice?
3. **Rate limits**: How often can we query the health endpoint without hitting Meta's rate limits? Can we poll every 5 minutes safely?
4. **Quality rating values**: What are all possible values for `quality_rating`? (GREEN, YELLOW, RED? Or numeric?)
5. **Messaging limit tiers**: What are all tier names? (TIER_1K, TIER_10K, TIER_100K, TIER_UNLIMITED?)
6. **Webhooks for status changes**: Does Meta send a webhook when quality rating or tier changes, or must we poll?
7. **Account-level vs phone-level**: Are health metrics per-phone-number or per-WABA-account?

## Sources to Investigate

- [Meta Graph API — Phone Numbers](https://developers.facebook.com/docs/whatsapp/business-management-api/phone-numbers)
- [Meta — Quality Rating & Messaging Limits](https://developers.facebook.com/docs/whatsapp/messaging-limits)
- Chatwoot source: `Whatsapp::HealthService` implementation
