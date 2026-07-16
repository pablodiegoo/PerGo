# Milestones

## v1.1 Campaign Engine (Shipped: 2026-07-16)

**Phases completed:** 6 phases (Phases 12, 12.1, 13, 14, 15, 16)

**Key accomplishments:**
- Campaign Engine: Bulk message scheduling, CSV sanitization, flexible regex variables mapping, NATS JetStream batch throttling, and enriched campaign logging.
- Media engine: Consolidated media storage pipelines and optimized storage.
- User action logs: Polymorphic actor action logs tracking dashboard and API key administrative tasks.
- CSS standardization: Cleaned and unified style guide with standard CSS tokens across dashboard pages.
- Deprecated workspace subviews: Removed redundant credential cards, routed WABA templates sync directly to Connections system.

## 1.0 1.0 (Shipped: 2026-07-14)

**Phases completed:** 13 phases, 34 plans, 67 tasks

**Key accomplishments:**

- Echo v5 server scaffold with pgxpool dual-access PostgreSQL, goose embedded migrations, health/readiness endpoints, and Docker Compose topology
- SHA-256 API key hashing with prefix lookup, AES-256-GCM envelope encryption, tenant-context convention, workspace/API key repositories with in-memory cache, and Echo v5 auth middleware
- Trace-ID middleware with UUID generation and header extraction, structured slog with trace context, buffered batch audit writer with pgx.CopyFrom, and monthly partitioned audit_logs with BRIN index
- pprof debug server on localhost:6060, expvar metrics with audit_drops counter, and LIFO shutdown orchestrator wiring Echo → debug → audit → NATS → pgxpool → sqlDB
- Server-rendered admin panel with templ + HTMX, HMAC-signed session auth, sidebar navigation, login/logout flow, and dashboard landing page with workspace count and audit activity
- Workspace CRUD and API key lifecycle management via admin panel with HTMX fragment updates, modal confirmations, and one-time plaintext key display
- Audit log review with parameterized filtering (workspace, trace_id, event_type, time range), 50-row pagination, CSV export, and HTMX fragment updates via admin panel
- POST /messages with JSON validation, structured error responses, and X-Trace-Id correlation header
- WorkQueue stream with publish-side dedup, worker stub, and composition root lifecycle wiring
- Production-hardens the message ingestion path with per-workspace rate limiting, queue depth backpressure, worker retry with exponential backoff, TTL enforcement, and delivery deduplication.
- Channel abstraction layer and WhatsApp Web adapter stub.
- Session management layer — device persistence, in-memory registry, and startup reconnection with storm protection.
- Admin UI for QR pairing and session telemetry — completes the operator-facing WhatsApp Web experience.
- Consolidated devices and channel credentials into a single unified connections table, migrated all records safely, and shimmed legacy repositories with full backwards compatibility.
- Implemented dynamic connection routing on ingest, NATS propagation, active whatsmeow connection limits per workspace, SOCKS5/HTTP proxy support, and connection re-pairing capability in the backend.
- Implemented the Notion-style collapsible sidebar navigation, dynamic active workspace dropdown selector, 4-step progressive onboarding checklist, operational developer telemetry widgets, and webhook simulation triggers.
- Conversational view data layer supporting multi-instance isolation, including schema migration, enriched inbound webhooks, and thread stitching queries.
- Split-pane inbox shell: sidebar link, conversation list with server-side read tracking, HTMX polling, and chat panel.
- Interactive chat panel, real-time message polling, outbound reply flow, and background toast notifications.
- Incremental message polling using server-side cursor swapped out-of-band (OOB) and native HTMX scroll support, completely eliminating custom client-side JavaScript.
- Collapsible settings configurations nested accordion sidebar and unified layout headers, with zero-flash workspace selector and removed top tabs.

---

## v1.0 v1.0 (Shipped: 2026-06-27)

**Phases completed:** 8 phases, 23 plans, 32 tasks

**Key accomplishments:**

- Echo v5 server scaffold with pgxpool dual-access PostgreSQL, goose embedded migrations, health/readiness endpoints, and Docker Compose topology
- SHA-256 API key hashing with prefix lookup, AES-256-GCM envelope encryption, tenant-context convention, workspace/API key repositories with in-memory cache, and Echo v5 auth middleware
- Trace-ID middleware with UUID generation and header extraction, structured slog with trace context, buffered batch audit writer with pgx.CopyFrom, and monthly partitioned audit_logs with BRIN index
- pprof debug server on localhost:6060, expvar metrics with audit_drops counter, and LIFO shutdown orchestrator wiring Echo → debug → audit → NATS → pgxpool → sqlDB
- Server-rendered admin panel with templ + HTMX, HMAC-signed session auth, sidebar navigation, login/logout flow, and dashboard landing page with workspace count and audit activity
- Workspace CRUD and API key lifecycle management via admin panel with HTMX fragment updates, modal confirmations, and one-time plaintext key display
- Audit log review with parameterized filtering (workspace, trace_id, event_type, time range), 50-row pagination, CSV export, and HTMX fragment updates via admin panel
- POST /messages with JSON validation, structured error responses, and X-Trace-Id correlation header
- WorkQueue stream with publish-side dedup, worker stub, and composition root lifecycle wiring
- Production-hardens the message ingestion path with per-workspace rate limiting, queue depth backpressure, worker retry with exponential backoff, TTL enforcement, and delivery deduplication.
- Channel abstraction layer and WhatsApp Web adapter stub.
- Session management layer — device persistence, in-memory registry, and startup reconnection with storm protection.
- Admin UI for QR pairing and session telemetry — completes the operator-facing WhatsApp Web experience.

---
