---
status: complete
date: 2026-06-28
description: Implement synchronous credentials validation for Telegram bot and WABA channels from the admin UI
---

# Quick Task: validar-credenciais-canais-tela - Summary

## Work Done
1. **Telegram Token Validation**:
   - Implemented `validateTelegramToken` in `WorkspaceHandler` which makes a synchronous HTTP request to `https://api.telegram.org/bot{token}/getMe`.
   - If Telegram returns HTTP `401 Unauthorized` or any failure status, validation fails.
2. **Form Integration**:
   - Integrated validation into the `SaveCredentials` POST handler for `telegram` channel.
   - If token is invalid, credentials are not saved, and the form is returned with a warning alert containing the failure details.
3. **WABA Validation Integration**:
   - Combined with previous sync work, both WABA and Telegram now feature synchronous token checking.
4. **Build and Test Verification**:
   - Successfully compiled the project and passed 100% of the test suite.
