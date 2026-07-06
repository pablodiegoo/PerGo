---
spike: 009
name: waba-template-inbox-delivery
type: standard
validates: "Given WABA integration, when the 24-hour customer window is closed, then the Inbox UI disables free text input and enforces sending approved templates to reopen the window."
verdict: VALIDATED
related: [005, 006, 007]
tags: [ui, inbox, waba, templates]
---

# Spike 009: WABA Template Inbox Delivery

## What This Validates
This spike validates how the conversational inbox handles official Meta API (WABA) sessions, specifically the 24-hour customer service window constraint, template composition, and initiating new threads.

## Research
Meta restricts official WhatsApp Cloud (WABA) messages:
- Free text messages can only be sent within 24 hours of the customer's last inbound message (the customer service window).
- Outside this window, only pre-approved Meta Templates can be sent.
- Sending a template successfully re-opens the 24-hour window when the customer replies.

### UI Redesign for WABA:
1. **Dynamic Textarea Disabling**: When a WABA conversation is active, check the `last_inbound_time` in `recipient_sessions`. If older than 24 hours (or if starting a new conversation), disable the standard message input textarea and show an explanatory banner: *"Janela de 24h fechada. Envie um template para reabrir a conversa."*
2. **Template Trigger Button**: In the chat compose bar, display a "Templates" button for WABA. Clicking it opens a modal listing approved templates (e.g., `welcome_optin`, `delivery_update`).
3. **Template Parameter Fields**: The template modal renders input fields for each template variable (`{{1}}`, `{{2}}`). Clicking send posts the template data to `/admin/inbox/send` as structured components.
4. **Initiating New Conversations ("Novo Chat")**: A "Novo Chat" button above the conversation list lets operators start a thread by entering a phone number and choosing a connection. If WABA is selected, the message body area is replaced with the template selector since no 24-hour window is active yet.

## How to Run
1. Run the mock server:
   ```bash
   go run .planning/spikes/007-inbox-polling-stability/main.go
   ```
2. Open `http://localhost:8089/inbox` in your browser.
3. Verify that:
   - Clicking **Carlos Mendes** (WhatsApp Web) allows normal free text replies.
   - Clicking **+5511977776666** (WABA conversation with >24h inactivity) displays the warning banner, disables the textarea, and shows the "Templates" button.
   - Clicking "Templates" allows selecting and filling parameters for `welcome_optin` or `delivery_update`.
   - Clicking "Novo Chat" and choosing WABA requires selecting a template, while WhatsApp Web/Telegram allows raw text bodies.
   - Sending a template re-opens the conversation window.
