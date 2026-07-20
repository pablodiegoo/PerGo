---
phase: 21-chatwoot-integration
plan: "02"
subsystem: integration
tags: [go, chatwoot, nats, integrations]

requires: ["21-01"]
provides:
  - Chatwoot REST v1 client and endpoints integration
  - Chatwoot inbound messages syncer
  - Asynchronous Chatwoot sync in InboundProcessor
  - Fully implemented ChatwootWebhookHandler with outbound dispatch queue integration
affects: []

tech-stack:
  added: []
  patterns:
    - Asynchronous message synchronization to Chatwoot in goroutines to prevent webhook timeouts.
    - Tenant isolation matching Conversation ID to mapped Contact/Workspace inside the webhook handler.

key-files:
  created:
    - internal/integration/chatwoot/client.go
    - internal/integration/chatwoot/syncer.go
    - internal/integration/chatwoot/client_test.go
    - internal/integration/chatwoot/syncer_test.go
  modified:
    - internal/inbound/processor.go
    - internal/inbound/processor_test.go
    - internal/channel/telegram/inbound.go
    - internal/channel/whatsapp/waba_inbound.go
    - internal/session/manager.go
    - internal/api/handler/chatwoot_webhook.go
    - internal/api/handler/chatwoot_webhook_test.go
    - cmd/pergo/main.go

key-decisions:
  - "D-04: Checked the local mapping table before routing inbound messages to Chatwoot, and resolved/created contact on mismatch/404."
  - "D-06: Filtered Chatwoot webhook payloads using message_type == outgoing, private == false, and sender.type == user."

requirements-completed:
  - CHAT-03
  - CHAT-04

coverage:
  - id: D4
    description: "ChatwootClient and ChatwootSyncer sync inbound customer traffic to Chatwoot, handling 404 local deletes"
    requirement: "CHAT-04"
    verification:
      - kind: integration
        ref: "internal/integration/chatwoot/syncer_test.go"
        status: pass
      - kind: unit
        ref: "internal/integration/chatwoot/client_test.go"
        status: pass
    human_judgment: false
  - id: D6
    description: "InboundProcessor processes unique events and calls ChatwootSyncer asynchronously"
    requirement: "CHAT-04"
    verification:
      - kind: unit
        ref: "internal/inbound/processor_test.go"
        status: pass
    human_judgment: false
  - id: D7
    description: "ChatwootWebhookHandler filters agent outgoing replies, resolves customer address, and publishes to outbound queue"
    requirement: "CHAT-03"
    verification:
      - kind: integration
        ref: "internal/api/handler/chatwoot_webhook_test.go"
        status: pass
    human_judgment: false

duration: 25min
completed: 2026-07-17
status: complete
---

# Phase 21 Plan 02: Bidirectional Sync Engine & Message Dispatch Summary

Completed the bidirectional synchronization engine between PerGo and Chatwoot, allowing customer inbound traffic to sync to Chatwoot and human agent outbound replies to dispatch back to channels.

## Performance

- **Duration:** 25 min
- **Started:** 2026-07-17T15:20:00Z
- **Completed:** 2026-07-17T15:45:00Z
- **Tasks:** 3
- **Files modified:** 8

## Accomplishments

- Implemented `ChatwootClient` wrapping Chatwoot REST v1 contacts, conversations, and messages APIs.
- Implemented `ChatwootSyncer` resolving/registering customer contacts and mapping them to conversation IDs.
- Integrated `ChatwootSyncer` asynchronously within the `InboundProcessor` flow.
- Fully implemented `ChatwootWebhookHandler` verifying outgoing public agent replies, mapping Chatwoot conversation IDs to customer addresses, and publishing to NATS `messages.outbound` queue.

## Task Commits

1. **Task 1: Chatwoot REST Client & Inbound Syncer** - `d5eeb2f` (feat)
2. **Task 2: Integrate ChatwootSyncer in InboundProcessor and adapters** - `ffe9744` (feat)
3. **Task 3: Chatwoot Webhook Dispatch & Wiring** - `485b3cf` (feat)
