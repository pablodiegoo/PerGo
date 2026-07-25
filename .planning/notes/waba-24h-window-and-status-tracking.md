---
title: WABA 24h Session Window Enforcement and Status Tracking
date: 2026-07-25
context: Exploration of Meta Cloud API upgrades for PerGo WABA engine
---

# WABA 24h Session Window Enforcement and Status Tracking

## Overview
Meta WhatsApp Cloud API enforces a strict 24-hour session window policy: freeform text, media, and interactive messages can only be sent within 24 hours of the last incoming user message. Attempts to send outside this window fail on Meta's end with error `131047`.

To maintain PerGo's philosophy ("our backend does the dirty job, keeping API requests clean and minimalist"), PerGo will track session windows locally and intercept expired requests early.

## Architectural Decisions

1. **Pre-flight 24h Window Validation**:
   - PerGo tracks the timestamp of the last incoming message from each contact in a `contact_sessions` table (`workspace_id`, `phone_number`, `last_incoming_at`).
   - Before publishing a non-template message to the dispatch queue, PerGo checks if `time.Since(last_incoming_at) <= 24*time.Hour`.
   - If the window is expired (or no prior incoming message exists), PerGo immediately rejects the request with **HTTP 422 Unprocessable Entity**:
     ```json
     {
       "error": "session_window_expired",
       "message": "Cannot send non-template message outside the 24-hour session window. Please send a WABA template message instead.",
       "last_incoming_at": "2026-07-24T12:00:00Z"
     }
     ```

2. **Delivery Status Correlation & Error Translation**:
   - Meta webhooks emit numerical status updates (`statuses`: `sent`, `delivered`, `read`, `failed`).
   - PerGo correlates incoming status webhooks to internal `message_dispatches` by `provider_message_id`.
   - Numerical error codes from Meta are translated into human-readable error reasons (`session_window_expired`, `phone_not_on_whatsapp`, `payment_required`, etc.) before forwarding outward to the workspace webhook endpoint (`message.status_updated`).
