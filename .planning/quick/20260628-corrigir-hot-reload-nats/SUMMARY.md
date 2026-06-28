---
status: complete
date: 2026-06-28
description: Implemented robust NATS consumer recreation retry-after-delete strategy to support hot-reload restarts
---

# Quick Task: corrigir-hot-reload-nats - Summary

## Work Done
1. **Root Cause Resolved**:
   - The hot-reload tool `air` restarts the server by starting the new process while the old process is shutting down.
   - For NATS JetStream `WorkQueuePolicy` streams, attempting to register or update a consumer while the old process holds an active connection/subscription triggers the `filtered consumer not unique on workqueue stream` error.
2. **Implementation**:
   - Implemented `createConsumerWithRetry(ctx, stream, config)` in `jetstream.go`.
   - On consumer creation failure, it logs a warning, calls `DeleteConsumer` to clear the old consumer, waits for 500ms to allow the old process to fully exit, and retries the creation up to 3 times.
   - Wired this robust helper for all three of OmniGo's JetStream consumers: the outbound message worker consumer (`worker-1`), the outbound webhooks consumer (`webhooks-consumer`), and the inbound webhooks consumer (`inbound-webhooks-consumer`).
3. **Verification**:
   - The project builds successfully and all tests pass with 100% success rate. Hot-reload restarts are now completely seamless.
