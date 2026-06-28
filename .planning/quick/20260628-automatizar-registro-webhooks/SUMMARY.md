---
status: complete
date: 2026-06-28
description: Automate webhook verification and registration for WABA and Telegram channels
---

# Quick Task: automatizar-registro-webhooks - Summary

## Work Done
1. **WABA Webhook Verification**:
   - Updated `HandleGet` in `waba_webhook.go` to accept the verification token `omnigo_verify_token_<workspace_id>` as a fallback.
   - This allows operators to configure WABA webhooks in the Meta Developer Console instantly without adding any additional config fields in OmniGo.
2. **Telegram Webhook Auto-Registration**:
   - Injected the `ExternalURL` configuration into `WorkspaceHandler`.
   - On saving Telegram credentials:
     - If `ExternalURL` starts with `https://`, OmniGo generates a random UUID secret token and registers the webhook automatically with Telegram's API (`/setWebhook`).
     - If it's a local non-HTTPS address (like `http://localhost`), it generates a predictable fallback token `omnigo_secret_token_<workspace_id>` so developers can easily test webhook ingestion locally.
3. **Build and Test Verification**:
   - Successfully compiled the project and passed 100% of the test suite.
