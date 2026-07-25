---
title: WABA Meta Flows Integration Architecture
date: 2026-07-25
context: Exploration of Meta Flows (NFM) for PerGo WABA engine
---

# WABA Meta Flows Integration Architecture

## Overview
Meta Flows (Native Form Messages / NFM) allow businesses to present multi-screen interactive forms natively within WhatsApp.

To adhere to PerGo's low-friction developer experience philosophy, PerGo abstracts away the multi-nested Meta Graph API v25.0 payload during dispatch, and automatically parses/decodes the escaped JSON response string from incoming `nfm_reply` webhooks.

## Dispatch Payload Format (`POST /messages`)

Developers submit a clean `flow` payload:
```json
{
  "to": "5511999999999",
  "channel": "vendas-waba",
  "type": "flow",
  "body": "Clique no botão abaixo para agendar a sua consulta:",
  "flow_id": "109823487654",
  "flow_cta": "Iniciar Agendamento",
  "flow_token": "token_optional_uuid",
  "flow_screen": "APPOINTMENT_SCREEN",
  "flow_data": { "user_id": "99" }
}
```

PerGo automatically:
- Generates a UUID `flow_token` if omitted.
- Builds the Meta Graph API v25.0 payload with `flow_message_version: "3"`, `action.name: "flow"`, `action.parameters`.

## Incoming Response Webhook Decoding (`nfm_reply`)

When the user completes the Flow, Meta delivers an incoming webhook with `interactive.type = "nfm_reply"` containing `interactive.nfm_reply.response_json` as an escaped JSON string.

PerGo decodes `response_json` and emits a clean outward event:
```json
{
  "event": "message.received",
  "type": "flow_response",
  "from": "5511999999999",
  "flow_id": "109823487654",
  "flow_token": "token_optional_uuid",
  "data": {
    "data_consulta": "2026-08-01",
    "especialidade": "Cardiologia",
    "observacoes": "Primeira consulta"
  }
}
```
