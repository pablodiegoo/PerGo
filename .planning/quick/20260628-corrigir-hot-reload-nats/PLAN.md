---
status: complete
date: 2026-06-28
description: Implement robust NATS consumer recreation retry-after-delete strategy to support hot-reload restarts
---

# Plan - Corrigir Hot-Reload NATS

Prevent NATS consumer initialization errors during development hot-reloads (`make dev`).

## Tasks
1. **Analyze NATS Error**: Under NATS JetStream `WorkQueuePolicy` (used in `MESSAGES` stream), only one active consumer configuration is allowed per filter subject. During hot-reload restarts (via `air`), the new process starts before the old process fully releases its NATS subscription/connection. This triggers a `filtered consumer not unique on workqueue stream` error on NATS.
2. **Implement Retry-After-Delete Strategy**:
   - Create a helper `createConsumerWithRetry` in `jetstream.go`.
   - The helper attempts to call `CreateOrUpdateConsumer`. If it fails, it issues a `DeleteConsumer` command for that consumer name, then sleeps for `500ms` to allow the old process's connection to close, and retries.
   - Apply this helper to the public messages queue consumer in `EnsureConsumer`.
   - Apply this helper to both `webhooks-consumer` and `inbound-webhooks-consumer` in `webhook_worker.go`.
3. **Verification**: Run tests and compile to ensure zero regressions.
