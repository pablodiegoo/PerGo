---
status: complete
date: 2026-06-28
description: Implement synchronous credentials validation for Telegram bot and WABA channels from the admin UI
---

# Plan - Validar Credenciais de Canais na Tela

Implement synchronous API verification of connection tokens for both Telegram and WhatsApp Cloud (WABA) before credentials are saved to the workspace.

## Tasks
1. **Telegram Token API Verification**: Implement a `validateTelegramToken` helper in `WorkspaceHandler` calling Telegram's GET `/getMe` endpoint.
2. **Synchronous Validation Flow**: Update `SaveCredentials` in `workspace.go` to block saving credentials if Telegram token validation fails.
3. **UI Integration**: Render verification errors to the user in a red alert box within the `TelegramCredentialsCard` component when validation fails.
4. **Verification**: Run `templ generate`, `go build`, and `make test-race`.
