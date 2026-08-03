# ADR-0009: Minimalist & Deep Module API Design Principles

**Status:** Accepted  
**Date:** 2026-08-02

## Context

As PerGo expands from a pure API gateway (v1.0-v1.7) into a full Developer CPaaS + Campaign Broadcaster platform (v1.8), there is a risk of API sprawl — creating fragmented endpoints, redundant DTO schemas, and shallow modules that leak internal concurrency (NATS, PostgreSQL queues, rate limiters) to external consumers and callers.

To preserve PerGo's core value ("a single unified API request delivers a message through any channel without vendor lock-in"), we require a strict architectural standard for API design and module interfaces.

## Decision

Adopt the **Deep Module & Minimalist API Paradigm** across all public REST APIs and internal Go packages. A module or API is "deep" when it exposes a small, simple, and intuitive surface area while encapsulating maximum operational power and complexity beneath.

### 1. Public REST API Principles

1. **Single Point of Entry for Ingestion**:
   - Outbound messaging MUST route through `POST /messages` using a unified JSON payload.
   - Channel routing MUST be determined by human-friendly Connection Slugs (e.g., `slug: "wa-vendas"`, `slug: "telegram-suporte"`) rather than internal GUIDs or provider-specific parameters.
2. **Strict Encapsulation of Async Complexity**:
   - API endpoints MUST return `202 Accepted` immediately upon validating ingestion and publishing to NATS JetStream, returning a `trace_id` and `message_id`.
   - Internal retry loops, rate-limiting delays (staggered dispatch), session window checks, and fallback channel switching MUST remain invisible to the HTTP client caller.
3. **Uniform Error Structure**:
   - All REST APIs MUST return standard RFC 7807 Problem Details JSON format (`type`, `title`, `status`, `detail`, `trace_id`).
   - Rejections (e.g., HTTP 422 Outside 24h Window, HTTP 429 Backpressure Queue Full) MUST provide actionable detail without exposing internal SQL or stack traces.

### 2. Internal Go Module Seam Principles

1. **Deep Seam Ingestion**:
   - Internal processors (`OutboundProcessor`, `InboundProcessor`, `BroadcasterEngine`) MUST expose small interface surfaces (e.g., `Process(ctx, Payload) error`).
   - Callers should not need to orchestrate multiple internal helpers (auth -> rate limiter -> queue -> DB); the processor encapsulates the orchestration flow.
2. **Port/Adapter Seam Discipline**:
   - External dependencies (whatsmeow, WABA Graph API, Telegram Bot API, SMTP) MUST be hidden behind provider-agnostic `Dispatcher` interfaces.
   - Internal queuing (NATS JetStream) MUST be hidden behind a `Publisher` interface to allow test doubles in unit/integration tests.

## Consequences

- **Developer Ergonomics**: Developers can integrate PerGo in minutes using simple HTTP requests or lightweight SDK wrappers.
- **Maintainability**: Low surface area means fewer breaking changes when internal engine logic or database schemas evolve.
- **Testability**: Deep modules enable high-level testing at the HTTP REST and worker seam without mocking dozens of shallow internal classes.
- **Trade-off**: Requires rigorous design discipline during code review to prevent adding single-purpose endpoints or leaking internal state flags to API contracts.
