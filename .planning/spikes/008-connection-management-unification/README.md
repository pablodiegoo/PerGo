---
spike: 008
name: connection-management-unification
type: standard
validates: "Given a workspace with multiple connections, when replacing separate connection pages and playground forms, then we can configure and test connections in a single dashboard."
verdict: VALIDATED
related: [001, 003]
tags: [ui, connections, workspace]
---

# Spike 008: Connection Management Unification

## What This Validates
This spike validates how to design a single, consolidated "Connections" screen that lets operators configure, pair, and test all connection channels (WhatsApp Web, WABA Cloud, Telegram) inside a workspace, replacing the standalone Developer Playground.

## Research
Currently, PerGo has a split UI:
- `/admin/devices` handles WhatsApp Web pairing and device listings.
- `/admin/workspaces/:id` handles Telegram/WABA credentials.
- `/admin/playground` provides a developer form to test sending messages.

Since Phase 8 consolidated all channels into a unified `connections` table, we can unify the interface under a single page:
1. **Unified List**: A table showing all connections (names, channel type, current status, sender identity, and connected timestamps).
2. **Channel Selection Modal**: A "Nova Conexão" button that opens a popup. The user selects the channel type (WhatsApp Web, WABA, Telegram) and enters credentials.
   - For WhatsApp Web: Clicking "Pair" starts Whatsmeow pairing and renders the QR Code inside the modal.
   - For WABA & Telegram Bot: Inputs credentials directly and verifies them instantly.
3. **Integrated Connection Testing (Playground)**: Instead of a standalone page, each connection in the table has a "Testar" button that opens a popup to send a quick text message to a specific receiver, outputting a real-time log of the NATS event dispatch inside the modal.

## How to Run
1. Run the unified mock server:
   ```bash
   go run .planning/spikes/007-inbox-polling-stability/main.go
   ```
2. Open `http://localhost:8089/connections` in your browser.
3. Verify that:
   - All connection types are listed together.
   - Clicking "Nova Conexão" allows configuring WABA, Telegram, or starting a WhatsApp Web QR pairing.
   - Clicking "Testar" opens a playground modal to send a test message and view debug logs.
