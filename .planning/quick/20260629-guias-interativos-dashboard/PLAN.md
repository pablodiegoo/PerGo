---
status: in-progress
date: 2026-06-29
description: Make provider setup guides interactive and copyable inside the admin panel workspaces credentials cards.
---

# Plan - Guias Interativos Dashboard

Add interactive onboarding and helper guides with copyable webhook URLs inside the WABA and Telegram credentials cards in the admin dashboard.

## Tasks
1. **Fix CHANNELS.md webhook URL**:
   - Update `docs/CHANNELS.md` to reference the correct WABA webhook endpoint `/webhooks/waba/[workspace_id]` instead of `/webhooks/waba`.
2. **Update templates/pages/workspaces.templ**:
   - Add `externalURL string` to the signatures of `WorkspaceDetailPage`, `WorkspaceDetailContent`, `WABACredentialsCard`, and `TelegramCredentialsCard`.
   - In `WABACredentialsCard`, add a collapsible "Help / Setup Guide" section containing the copyable Callback URL (`[externalURL]/webhooks/waba/[workspaceID]`), verify token instructions, and Meta Webhook setup instructions.
   - In `TelegramCredentialsCard`, add a similar collapsible guide explaining `@BotFather` setup, and showing the Telegram webhook URL (`[externalURL]/webhooks/telegram/[workspaceID]`) with a note explaining automatic registration.
   - Add appropriate CSS styling for copyable inputs, collapsible detail tags, and instruction lists.
3. **Update internal/api/handler/admin/workspace.go**:
   - Pass `h.ExternalURL` to `pages.WorkspaceDetailPage` in `Detail`.
   - Pass `h.ExternalURL` to `pages.WABACredentialsCard` and `pages.TelegramCredentialsCard` in `SaveCredentials` and `DeleteCredentials`.
4. **Verification**:
   - Run `make generate` to compile the templates.
   - Run `make test` to ensure tests compile and pass.
