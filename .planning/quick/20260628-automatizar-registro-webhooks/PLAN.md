---
status: complete
date: 2026-06-28
description: Automate webhook verification and registration for WABA and Telegram channels
---

# Plan - Automatizar Registro de Webhooks

Automate Meta Webhook verification tokens and Telegram webhook registration to eliminate manual setup steps.

## Tasks
1. **Meta Webhook Fallback Verification**: Add support in `waba_webhook.go` for verifying WABA webhook callbacks automatically using a predictable pattern: `omnigo_verify_token_<workspace_id>`.
2. **Telegram Webhook Auto-Registration**:
   - Inject `ExternalURL` configuration into `WorkspaceHandler`.
   - On saving Telegram credentials, if `ExternalURL` is HTTPS, automatically register the webhook URL with Telegram via the `/setWebhook` endpoint and generate a secure random `secret_token`.
   - If not HTTPS (local development), generate a predictable fallback secret token (`omnigo_secret_token_<workspace_id>`) for local development manual testing.
3. **Verification**: Verify that everything builds and tests pass.
