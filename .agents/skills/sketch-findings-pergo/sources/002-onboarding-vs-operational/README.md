---
sketch: 2
name: onboarding-vs-operational
question: "How do we transition dynamically from onboarding checklist to returning operational dashboard?"
winner: "both"
tags: [dashboard, onboarding, ux]
---

# Sketch 002: Onboarding vs. Operational Dashboard

## Design Question
How do we dynamically guide first-time users through the onboarding checklist (generating API keys, pairing devices, testing messages, setting webhooks) and transition them smoothly to the operational metrics console?

## How to View
open `.planning/sketches/002-onboarding-vs-operational/index.html`

## Features & Interactivity
- **Variant A (Day 1 Onboarding):** Fully interactive guide.
  - Click **"Gerar Chave de API"** to simulate API key generation (unlocks Step 2).
  - Click **"WhatsApp Web"** and then **"Simular Escaneamento"** to pair the device (unlocks Step 3, updates the sidebar connection counter).
  - Complete the message inputs and click **"Enviar Mensagem"** to test (unlocks Step 4).
  - Enter a webhook URL and click **"Salvar"** to complete onboarding.
  - Once all 4 steps are complete, a success banner appears letting you transition to the operational view!
- **Variant B (Day 2+ Operational Dashboard):**
  - Shows connected channels (multi-instance support).
  - Displays developer-focused system telemetry (message count, average latency, RAM consumption).
  - Shows recent audit logs. Click **"Simular Mensagem Recebida"** to dynamically push a new simulated message to the table with a flash animation.

## What to Look For
- Test the full onboarding flow in Variant A. Notice the progress locking/unlocking and visual feedback.
- Test the Webhook Simulator in Variant B to see how the audit log table responds dynamically.
