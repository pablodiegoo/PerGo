# ADR-0006: Extract Outbound Processor

**Status:** Accepted  
**Date:** 2026-07-13

## Context

The outbound message ingestion pipeline (payload validation, rate limiting/backpressure tracking, media downloading, S3 storage caching, route resolution, and NATS JetStream publishing) was previously implemented inline inside the `POST /api/v1/messages` HTTP handler (`MessageHandler.Create`).

This created several architectural issues:
1. **Shallow Web Handler**: The HTTP handler was highly complex, leaking business logic (validation, limit checking, media caching, connection routing) directly into the routing layer.
2. **Poor Locality**: Outbound ingestion rules and rate limiters were spread across middlewares and handler methods.
3. **Hard to Test**: Testing outbound ingestion logic required constructing mock Echo contexts, HTTP requests, and responses, adding verbosity to unit tests.

## Decision

We will extract all outbound message ingestion logic into a deep **Outbound Processor** module under `internal/outbound`.

### Boundaries & Division of Labor

- **Adapter (HTTP Handler) owns:** Request body binding, context extraction (Workspace ID, Trace ID), and mapping generic domain errors to protocol-specific HTTP status codes.
- **Outbound Processor owns:** Ingestion validation, per-workspace queue depth backpressure checks, media downloading/S3 caching, connection routing resolution, NATS publishing, and queue depth increment tracking.

```
┌──────────────────────────────────────────────┐
│  Adapter (Echo MessageHandler)               │  ← Parses body, sets headers, maps HTTP status
├──────────────────────────────────────────────┤
│  Seam: Ingest(ctx, workspaceID, traceID, req)│  ← Decoupled interface
├──────────────────────────────────────────────┤
│  Deep Outbound Processor                     │  ← Backpressure, Validation, S3 Cache,
│                                              │    Route Resolution, NATS publish
└──────────────────────────────────────────────┘
```

### Seam and Error Classification

To prevent leaking web-layer concerns, the processor exposes generic ports for its dependencies (`QueueDepthChecker`, `MediaUploader`, `RouteResolver`, `Publisher`) and returns clean domain errors:
- `ValidationError`: wraps schema validation issues (mapped to HTTP 400).
- `MediaError`: holds media size/limit or download issues (mapped to HTTP 422/500).
- `RouteError`: missing workspace connection routing (mapped to HTTP 422).
- `ErrQueueFull`: sentinel backpressure limit error (mapped to HTTP 429).

## Test Strategy

We will write direct, fast unit tests in <!-- VERIFY: processor_test.go --> using in-memory mock implementations of the dependency interfaces. This allows verifying:
- Schema validation rules
- Media size limits (25MB)
- Backpressure limiters
- Routing lookup failures
All of these will run in milliseconds without NATS, LocalStack, or PostgreSQL.

## Consequences

- **Locality:** All outbound business rules and validation logic concentrate in a single, dedicated package.
- **Leverage:** The processor interface is highly reusable, allowing potential CLI or gRPC adapters to hook into outbound ingestion without duplication.
- **Testability:** Replaces verbose HTTP mock request tests with clean, fast unit tests running on local in-memory fakes.
