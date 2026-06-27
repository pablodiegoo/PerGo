---
phase: 06-webhook-delivery-dlq
plan: 01
subsystem: webhooks
tags: [nats, jetstream, postgres, hmac-sha256, dlq, admin-ui]

requires:
  - phase: none
    provides: Webhook delivery queue and dead-letter queue system
provides:
  - "Database schema and repository operations for webhook configurations and dead-letter logs (DLQ)"
  - "NATS JetStream stream named WEBHOOKS and status webhook event publishing in the message worker"
  - "Durable pull consumer worker delivering webhooks with HMAC-SHA256 signatures, replay protection, and exponential backoff retry"
  - "Echo handler and compile-time type-safe Templ views for webhook configurations and DLQ management"
affects: []

tech-stack:
  added: []
  patterns: [hmac-sha256-signing, pull-consumer, exponential-backoff-retry, dead-letter-queue]

key-files:
  created:
    - internal/platform/postgres/migrations/008_create_webhooks_and_dlq.sql
    - internal/repository/webhook_dlq.go
    - internal/repository/webhook_dlq_test.go
    - internal/platform/queue/webhook_worker.go
    - internal/platform/queue/webhook_worker_test.go
    - templates/pages/webhooks.templ
    - templates/pages/webhooks_templ.go
    - internal/api/handler/admin/webhook_dlq.go
    - cmd/omnigo/admin_webhook_dlq_test.go
  modified:
    - internal/platform/queue/jetstream.go
    - internal/platform/queue/worker.go
    - cmd/omnigo/main.go
    - static/css/admin.css
    - templates/layout/sidebar.templ
    - templates/layout/sidebar_templ.go
    - templates/pages/workspaces.templ
    - templates/pages/workspaces_templ.go
    - .planning/config.json

key-decisions:
  - "Used HMAC-SHA256 signature scheme with prefix t=timestamp,v1=signature for payload verification, preventing spoofing and replay attacks (5-minute window validation)"
  - "Mapped HTTP response status codes: terminal failures (400, 401, 403, 404) write straight to DLQ, while transient failures (429, 5xx, timeouts) trigger NakWithDelay retry"
  - "Leveraged NATS JetStream native NakWithDelay delay computation based on message NumDelivered for crash-safe exponential backoff retry up to 10 attempts"
  - "Integrated a sidebar badge count updated asynchronously using HTMX GET /admin/webhooks/dlq/badge to prevent changing standard layout signatures"

patterns-established:
  - "Durable status webhook pull consumer using JetStream stream LimitsPolicy and explicit ACK"
  - "Workspace tenant-isolated dead-letter log (DLQ) dashboard enabling inspection, manual deletion, and manual retry (re-enqueueing)"

requirements-completed: [WHOOK-01, WHOOK-02, WHOOK-03, WHOOK-04, WHOOK-05]

coverage:
  - id: WHOOK-01-01
    description: "Outbound webhook event published and consumed durably via WEBHOOKS stream"
    requirement: WHOOK-01
    verification:
      - kind: integration
        ref: "internal/platform/queue/webhook_worker_test.go#TestWebhookWorker_Integration"
        status: pass
  - id: WHOOK-02-01
    description: "Webhook signature computed correctly using HMAC-SHA256 and matched prefix format"
    requirement: WHOOK-02
    verification:
      - kind: unit
        ref: "internal/platform/queue/webhook_worker_test.go#TestSignPayload"
        status: pass
  - id: WHOOK-03-01
    description: "Webhook event JSON payload structured with event, trace_id, message_id, and channel"
    requirement: WHOOK-03
    verification:
      - kind: integration
        ref: "internal/platform/queue/webhook_worker_test.go#TestWebhookWorker_Integration"
        status: pass
  - id: WHOOK-04-01
    description: "DLQ table stores failed events, lists logs, and resolves details"
    requirement: WHOOK-04
    verification:
      - kind: integration
        ref: "internal/repository/webhook_dlq_test.go#TestWebhookDLQRepository"
        status: pass
  - id: WHOOK-04-02
    description: "Admin Echo endpoints serve config saving, DLQ detail, deletion, and manual retry under isolation"
    requirement: WHOOK-04
    verification:
      - kind: integration
        ref: "cmd/omnigo/admin_webhook_dlq_test.go#TestAdminWebhookDLQHandlers"
        status: pass
  - id: WHOOK-05-01
    description: "Webhook retry triggers NakWithDelay exponential backoff, and terminal status triggers immediate DLQ move"
    requirement: WHOOK-05
    verification:
      - kind: integration
        ref: "internal/platform/queue/webhook_worker_test.go#TestWebhookWorker_TerminalErrorDLQ"
        status: pass
