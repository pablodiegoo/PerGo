# Phase 28: Email Channels & Tracking Engine

## User Request
"precisamos implementar novos canais pra comunicar... como amazon SES e demais disparadores de email... tipo mautic ou mailtrap, com disparo, tracking"

## Scope & Design (per codebase-design skill)

### Core Capabilities:
1. **Email Channel Support**: Add `"email"`, `"email_ses"`, `"email_smtp"`, and `"email_mautic"` to `domain.ValidChannels`.
2. **Deep Email Module (`internal/channel/email`)**:
   - Small interface at the `channel.Dispatcher` seam.
   - Internal `Provider` interface (`SESProvider`, `SMTPProvider`, `MauticProvider`).
   - MIME message builder (HTML/Text bodies, attachments, headers).
3. **Tracking Engine (`internal/channel/email/tracking.go`)**:
   - Transparent open tracking: Injects 1x1 GIF tracking pixel with HMAC signature before `</body>`.
   - Transparent click tracking: Rewrites `<a href="...">` links with signed HMAC tokens pointing to PerGo tracking endpoint `/v1/webhooks/email/click`.
4. **Inbound Webhook Normalizer (`internal/inbound/email`)**:
   - Normalizes AWS SES SNS notifications and Mautic webhooks into standard PerGo message status updates (`StatusDelivered`, `StatusRead`, `StatusFailed`).

## Technical Decisions
- **Standard Library `net/smtp`**: Zero-dep SMTP adapter for Mailtrap/SendGrid/Postmark/Custom SMTP.
- **HMAC Signatures**: Prevent URL tampering on tracking links without needing extra DB lookup state.
- **Provider Seam**: Abstracted behind `email.Provider` interface for clean mockability and expansion.
