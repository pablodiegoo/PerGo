# ADR-0007: Extract Webhook Dispatcher

**Status:** Accepted  
**Date:** 2026-07-13

## Context

The webhook delivery mechanism in PerGo was previously managed by a single NATS queue consumer named `WebhookWorker` (`internal/platform/queue/webhook_worker.go`).

This structure introduced several architectural concerns:
1. **Mixed Concerns**: The queue consumer was responsible for queue-polling logic (JetStream fetches, fetch timeouts, backpressure tracking, and worker thread concurrency) as well as core business delivery rules:
   - HMAC signature generation (`SignPayload`).
   - Workspace opt-in verification and compliance PII anonymization/redaction.
   - HTTP client POST dispatch execution.
   - DLQ record insertion on terminal delivery failure.
2. **Poor Locality**: Webhook configurations, signature algorithms, and compliance redaction rules were entangled in queue transport code.
3. **Hard to Test**: Testing delivery signatures, HTTP posting, and PII opt-out rules required setting up full NATS workers, queues, and streams in unit tests.

## Decision

We will extract all webhook delivery and compliance logic into a deep **Webhook Dispatcher** module under `internal/webhook`.

### Boundaries & Division of Labor

- **Adapter (Webhook Worker) owns:** NATS JetStream pull subscription polling, message fetching, Nak exponential backoffs, Acks, and attempt tracking.
- **Webhook Dispatcher owns:** Fetching webhook configuration from the database, evaluating workspace PII opt-in compliance rules, redacting payload bytes if opted out, signing payloads with HMAC-SHA256, sending the HTTP POST, and archiving failures in the DLQ repository.

```
┌──────────────────────────────────────────────┐
│  Adapter (NATS Webhook Worker)               │  ← Polls NATS, manages Acks/Naks
├──────────────────────────────────────────────┤
│  Seam: Dispatch(ctx, mode, rawEvent)         │  ← Decoupled interface
├──────────────────────────────────────────────┤
│  Deep Webhook Dispatcher                     │  ← PII Compliance, HMAC Signing,
│                                              │    HTTP Post, DLQ Database Write
└──────────────────────────────────────────────┘
```

### Seam and Interfaces

The dispatcher is built on top of pure interfaces (`ConfigStore`, `WorkspaceStore`, `HTTPClient`) defined in the `webhook` package, separating delivery execution from direct database connections or network setups.

## Test Strategy

We will write direct, fast unit tests in <!-- VERIFY: dispatcher_test.go --> using stubs for the dependency interfaces. This allows validating:
- Correct HMAC header signatures
- Selective hashing of the `from` field and removal of `location`/`contacts` for PII opted-out workspaces
- DLQ record insertions on terminal HTTP status codes (e.g. 404)
These unit tests run in milliseconds without NATS or Docker containers.

## Consequences

- **Locality:** Webhook compliance rules, signature logic, and HTTP postings concentrate in a single, dedicated package.
- **Leverage:** The Webhook Dispatcher can be reused outside of NATS consumers (e.g., ad-hoc manual retries initiated by an administrator via the control panel).
- **Testability:** Replaces queue worker integration tests with lightning-fast unit tests running on local in-memory stubs.
