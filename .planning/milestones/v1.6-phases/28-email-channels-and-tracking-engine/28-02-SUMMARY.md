---
phase: 28-email-channels-and-tracking-engine
plan: 02
subsystem: channel
tags: [email, tracking, hmac, ses, mautic, webhooks, echo]

requires:
  - phase: 28-email-channels-and-tracking-engine
    provides: Email provider abstraction and SMTP provider
provides:
  - Amazon SES & Mautic email channel providers
  - Email open pixel injection & click link HMAC rewriting engine
  - Open tracking, click redirecting, and SES SNS webhook handlers in Echo
affects: [webhooks, inbound, channel]

tech-stack:
  added: []
  patterns: [HMAC signature verification for email tracking, transparent 1x1 GIF serving, webhooks event normalization]

key-files:
  created:
    - internal/channel/email/tracking.go
    - internal/channel/email/tracking_test.go
    - internal/channel/email/ses.go
    - internal/channel/email/mautic.go
    - internal/inbound/email.go
    - internal/inbound/email_test.go
  modified:
    - cmd/pergo/main.go

key-decisions:
  - "Used SHA256-HMAC for messageID + payload signature verification on open/click tracking endpoints to prevent URL tampering."
  - "Integrated SES SNS and Mautic event payloads directly into PerGo status updates (StatusDelivered, StatusRead, StatusFailed)."
  - "Wired email tracking and webhook endpoints into main Echo router under /v1/webhooks/email/*."

patterns-established:
  - "Pattern 1: Open tracking pixel injected before closing body tag with HMAC signature."
  - "Pattern 2: Click tracking link rewriting with base URL and HMAC payload protection."

requirements-completed:
  - EMAIL-02
  - TRACK-01

coverage:
  - id: D1
    description: "Email tracking engine for open pixel injection and link HMAC rewriting"
    requirement: "TRACK-01"
    verification:
      - kind: unit
        ref: "internal/channel/email/tracking_test.go"
        status: pass
    human_judgment: false
  - id: D2
    description: "Amazon SES & Mautic email providers implementation"
    requirement: "EMAIL-02"
    verification:
      - kind: unit
        ref: "internal/channel/email/email_test.go"
        status: pass
    human_judgment: false
  - id: D3
    description: "Email webhooks for open, click, and SES SNS notifications"
    requirement: "TRACK-01"
    verification:
      - kind: unit
        ref: "internal/inbound/email_test.go"
        status: pass
    human_judgment: false

duration: 15 min
completed: 2026-07-25
status: complete
---

# Phase 28 Plan 02: Amazon SES, Mautic, Tracking Engine & Email Webhooks Summary

**Amazon SES and Mautic API providers with HMAC-secured open pixel injection, link rewriting, and HTTP webhook event handlers**

## Performance

- **Duration:** 15 min
- **Started:** 2026-07-25T01:25:00Z
- **Completed:** 2026-07-25T01:27:00Z
- **Tasks:** 3
- **Files modified:** 7

## Accomplishments
- Implemented `InjectOpenPixel`, `RewriteClickLinks`, and `VerifyTrackingHMAC` for email engagement tracking.
- Created `SESProvider` and `MauticProvider` implementations of the `email.Provider` interface.
- Created HTTP handlers in `internal/inbound/email.go` for `/v1/webhooks/email/open`, `/v1/webhooks/email/click`, and `/v1/webhooks/email/ses`.
- Wired tracking and webhook routes into Echo web router in `cmd/pergo/main.go`.

## Task Commits

Each task was committed atomically:

1. **Task 1: Implement Tracking Engine** - `feat(28-02): email tracking open/click engine with HMAC`
2. **Task 2: Implement Amazon SES & Mautic Providers** - `feat(28-02): Amazon SES and Mautic email channel providers`
3. **Task 3: Implement Email Webhooks & Tracking HTTP Handlers** - `feat(28-02): email tracking and webhook Echo HTTP handlers`

## Files Created/Modified
- `internal/channel/email/tracking.go` - Open pixel injection, click link rewriting, and HMAC sign/verify logic
- `internal/channel/email/tracking_test.go` - Unit tests for tracking engine
- `internal/channel/email/ses.go` - Amazon SES v2 API HTTP provider implementation
- `internal/channel/email/mautic.go` - Mautic REST API provider implementation
- `internal/inbound/email.go` - Webhook handlers for open, click redirect, and SES SNS event status updates
- `internal/inbound/email_test.go` - Unit tests for open pixel serving and click redirection
- `cmd/pergo/main.go` - Echo router route registration for email tracking and webhooks

## Decisions Made
- Used SHA256-HMAC for messageID + payload signature verification on open/click tracking endpoints to prevent URL tampering.
- Integrated SES SNS and Mautic event payloads directly into PerGo status updates (`StatusDelivered`, `StatusRead`, `StatusFailed`).

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required for local execution.

## Next Phase Readiness
- Email channel support (SMTP, SES, Mautic) and email tracking engine are fully complete.
- Ready for phase verification and closure.

---
*Phase: 28-email-channels-and-tracking-engine*
*Completed: 2026-07-25*
