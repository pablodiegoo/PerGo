# PerGo

## What This Is

PerGo is a self-hosted, open-source Omnichannel Communications Platform as a Service (CPaaS) engineered in Go. It exposes a single unified REST API (`POST /messages`) that abstracts away the fragmentation of managing multiple messaging providers — WhatsApp Web (unofficial via whatsmeow), WhatsApp Cloud (WABA), and Telegram — under one standardized JSON payload. It is built for backend developers integrating omnichannel flows into CRMs/ERPs and for system operators managing channel connections and compliance.

## Core Value

A single API request delivers a message through any configured channel with automatic fallback — without per-message markup, without vendor lock-in, and with full custody of transaction data on infrastructure you control.

## Business Context

- **Customer**: Backend developers and system operators at organizations that need omnichannel messaging without commercial CPaaS markup (replacing Twilio-like vendors for a specific deployment)
- **Revenue model**: Self-hosted open-source — no per-message fees; cost is infrastructure only
- **Success metric**: 99.5% delivery success across all channels at 500+ req/s sustained throughput
- **Strategy notes**: See `docs/PRD PerGo.md` and `docs/architecture/` for full product and technical specifications

## Current State

- **Shipped Version**: v1.3 (2026-07-20)
- **Status**: Stable. Fully functional multi-tenant routing gateway with active contact profiles, multi-webhook subscriptions, sequential JSON Response Verbs engine, Meta WABA read receipt indicators, and built-in Chatwoot and Typebot integrations with stateful human/bot handoff control.

## Requirements

### Validated

- ✓ Chatwoot Integration: connection settings panel, built-in query-parameter authenticated webhook receiver, and bidirectional client/syncer sync engine (CHAT-01 to CHAT-04) — Phase 21
- ✓ Typebot Integration: settings panel, asynchronous forwarder, session mapping repository, and outbound webhook receiver (TYPE-01 to TYPE-04) — Phase 22
- ✓ Stateful Handoff Routing: contact bot_active/bot_paused_at database schema, automatic agent response interceptors, manual chat panel HTMX toggle badge, pause_bot messaging verb, and 12-hour inactivity cooldown reset (HAND-01 to HAND-06) — Phase 23
- ✓ Webhook Verbs Refactoring: polymorphic VerbHandler extraction and static registration (D-01 to D-05) — Phase 24
- ✓ Unified message ingestion gateway: `POST /messages` → validate → Trace-ID → NATS JetStream queue → `202 Accepted` — Phase 3
- ✓ Multi-tenant dashboard control panel: server-rendered (Echo + Templ + HTMX), workspace management, QR pairing, connection telemetry, audit review — Phase 2, 4
- ✓ Multi-session connection controller: WhatsApp Web device pairing, persistent session store in PostgreSQL, reconnect on restart — Phase 4
- ✓ Smart queueing, backpressure, and rate-limiting engine: NATS JetStream work queue, 1,000-message per-session queue limit with HTTP 429/422 backpressure, staggered dispatch (1-3s delay) for unofficial channels — Phase 3
- ✓ Automated smart fallback pipeline: ordered `fallback_channels` array, iterative dispatch with failure-driven channel switching — Phase 5
- ✓ Compliance, auditing, and logging engine: Trace-ID propagation across HTTP → NATS → worker → SQL, immutable partitioned `audit_logs` table, buffered batch writer — Phase 1
- ✓ WhatsApp Web channel adapter (whatsmeow): WebSocket sessions, multi-device support — Phase 4
- ✓ WhatsApp Cloud channel adapter (WABA REST API): official Meta integration — Phase 5
- ✓ Telegram channel adapter: Telegram Bot HTTP API — Phase 5
- ✓ API key authentication: SHA-256 hashed keys with prefix lookup, in-memory cache — Phase 1
- ✓ Credential encryption at rest: AES-256-GCM for session tokens and channel credentials — Phase 1
- ✓ Outbound webhook delivery: durable JetStream consumer for webhook dispatch with retries — Phase 6
- ✓ Observability: `net/http/pprof` profiling, structured `log/slog` logging, expvar metrics — Phase 1
- ✓ Conversational Inbox: Server-rendered split-pane conversational dashboard with live HTMX polling, dynamic conversation lists, message thread stitching, and Toast notifications — Phase 9
- ✓ Settings Layout: Nested settings accordion, active route state persistence, and zero-flash workspace selector — Phase 11
- ✓ Multi-Webhook Subscriptions: subscription configuration with glob/wildcard filtering, JetStream-driven parallel delivery workers, DLQ diagnostics — Phase 17
- ✓ Omnichannel Contact Merging: unified profile contact/identities schemas, auto-resolution on inbound/outbound events, transactional merge consolidation — Phase 18
- ✓ Webhook Response Verbs Engine: sequential execution of reply, wait (capped at 10s), forward, tag, and close actions, with 30s context limits and action logs audits — Phase 19
- ✓ WABA Read Receipts & Status updates: Meta callback processing, message status updates in database, and visual delivery checkmarks in Chat UI bubbles — Phase 20
- ✓ Typebot Integration: User configuration of Typebot credentials (API URL, Bot ID, Public Token) per workspace, built-in webhook receiver endpoint to parse and enqueue responses, and asynchronous inbound message forwarder maintaining active conversation session per contact — Phase 22

### Active

*(All current milestone requirements have been validated. Run `/gsd-new-milestone` to start the next requirements definition.)*

### Out of Scope

- Real-time Voice and WebRTC orchestration (SIP trunking, audio calls, Pion) — not core to transactional messaging
- Community and group management (creating groups, member permissions, announcement groups) — direct message delivery only; Group JID targeting allowed but no admin features
- Visual conversation flow builders / drag-and-drop bot designers — PerGo is a backend router; chat logic lives in consumer applications via REST + webhooks
- Redis / memcached cache layer — unmeasured need at 500 req/s; add only if auth or session lookup proves hot
- Kafka — NATS JetStream covers durability + queue groups with less operational weight at this scale
- gRPC internal mesh — single binary, REST + JetStream suffice
- ORM / query builder / DI framework / config daemon — hand-written SQL with pgx, 12-factor env config
- OpenTelemetry distributed tracing SDK in MVP — Trace-ID propagated explicitly via context + NATS headers + slog; add OTel only if a tracing backend is introduced

## Context

**Existing documentation:** The project has a comprehensive PRD (`docs/PRD PerGo.md`) and a six-part architecture document set (`docs/architecture/01-06`). These were produced before GSD initialization and define the product scope, technical stack, directory structure, concurrency model, resilience strategy, and core code examples. They are the authoritative source for implementation detail.

**Deployment context:** Building for a specific real use case (not a general release). Solo developer directing AI agents for implementation. No hard deadline — quality and architectural correctness take priority over speed.

**Architecture posture (from `docs/architecture/01-architectural-summary.md`):**
- NATS JetStream is the single durability boundary for outbound work — workers are stateless and crash-safe
- PostgreSQL is the system of record for identity (workspaces, API keys, device sessions, audit logs) — never a hot-path queue
- Ingest path is two external operations: auth (cached) + JetStream publish — everything else off the request goroutine
- Channel layer is a plugin boundary (consumer-side `Dispatcher` interface) so unofficial protocol breakage never touches core
- Every external dependency must answer "what does this do that the std lib cannot?" — only `pgx`, `nats.go`, `whatsmeow` clearly earn their place

**Milestone plan (from PRD):**
1. Core Foundation (Echo API, PostgreSQL schemas, logging engine, Templ control panel)
2. Queue & WhatsApp Web (NATS JetStream, whatsmeow worker, rate limiting, backpressure)
3. Official Channel Integration (WABA, Telegram, smart fallback engine, load testing)

## Constraints

- **Tech stack**: Go 1.22+ with Echo (HTTP), a-h/templ + HTMX (admin UI), NATS JetStream (broker), PostgreSQL via pgx/v5 (persistence), whatsmeow (WhatsApp Web), golang.org/x/time/rate (rate limiting), log/slog (logging) — per `docs/architecture/02-technical-decisions.md`
- **Performance**: >= 500 messages/sec sustained throughput; <= 50ms p99 ingestion latency; < 512MB RAM on 2 vCPU — measured against real production loads
- **Reliability**: >= 99.5% delivery success across all active channels; 100% trace-correlated logging for all requests and webhooks
- **Security**: AES-256-GCM encryption at rest for credentials; SHA-256 hashed API keys; data sovereignty (self-hosted, GDPR/LGPD compliant)
- **Backpressure**: 1,000-message per-session queue limit enforced before enqueue (HTTP 429/422 when exceeded)
- **Unofficial channel safety**: Staggered dispatch (1-3s random delay) for WhatsApp Web to minimize account suspension risk
- **Dependencies**: Three packages earn their place — pgx, nats.go, whatsmeow. Everything else on probation against the std lib.

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Echo over net/http+chi for HTTP router | PRD prescribes Echo; handler ergonomics and middleware parity with admin stack — keep handlers thin and std-http.Handler-compatible so router is swappable | Validated (Phase 1, 2) |
| pgx/v5 over database/sql+sqlx | Binary protocol, prepared-statement cache, native COPY, batch pipeline — right call for the audit batch writer | Validated (Phase 1) |
| NATS JetStream over Kafka or in-process channels | Work-queue semantics give at-least-once durability with single consumer per message; far less operational weight than Kafka at this scale; in-process channels lose work on crash | Validated (Phase 3) |
| PostgreSQL as sole datastore (no Redis) | 500 req/s load envelope doesn't require cache layer; API-key auth served from in-memory map with TTL refresh — add Redis only if measurement shows hot path | Validated (Phase 1) |
| No ORM, no query builder | Hand-written SQL with pgx CollectRows/ForEachRow helpers; query count is small and known | Validated (Phase 1) |
| log/slog over zerolog/zap | Structured, leveled, context-aware, std lib (Go 1.21+) — no external dependency unless benchmark proves need | Validated (Phase 1) |
| In-house circuit breaker and backoff over sony/gobreaker | State machine is small; avoid external dependency semantics mismatch — revisit if requirements grow | Validated (Phase 3) |
| No OpenTelemetry in MVP | Trace-ID propagated explicitly via context + NATS headers + slog; add OTel only if tracing backend introduced | Validated (Phase 1) |
| Domain-oriented packages over MVC layers | Each package importable on its own, depends only on internal/platform; channel adapters are siblings sharing an interface, not a hierarchy | Validated (Phase 1) |
| cmd/pergo as sole composition root | No internal/app "god package"; main.go wires deps, starts HTTP + workers | Validated (Phase 1) |
| Database-driven inbox read status | Track read/unread states server-side in recipient_sessions to support multi-operator synchronization and prevent cookie limits | Validated (Phase 9) |
| Multi-instance connection isolation | Extend recipient_sessions PK with recipient_identity to allow multiple active numbers of the same channel | Validated (Phase 9) |
| Storing Contact tags as PostgreSQL TEXT[] with a GIN index | Leverage native PostgreSQL array search efficiency for contact tagging | Validated (Phase 19) |
| Resetting contact closed_at to NULL automatically during ResolveContact | Reopens conversation thread automatically on new inbound message | Validated (Phase 19) |
| Decoupling webhook verbs execution from HTTP dispatcher request context | Uses context.Background() inside async goroutine execution to avoid worker timeouts | Validated (Phase 19) |
| Meta status receipts updating message dispatches status directly | Leverage provider_message_id index to map Meta callbacks back to original dispatches | Validated (Phase 20) |
| Visual indicator delivery SVGs (sent, delivered, read, failed) | Checkmark graphics parsed from dispatch status via LEFT JOIN in ListThreadByContact | Validated (Phase 20) |
| Reuse central integrations table for Typebot | Store Typebot API URL, Bot ID, and Public Token encrypted as JSON envelope in integrations table | Validated (Phase 22) |
| Dedicated typebot_sessions mapping table | Map contacts, workspaces, and connections to remote Typebot session IDs in PostgreSQL | Validated (Phase 22) |
| Webhook receiver for bot replies | Expose public integration endpoint to enqueue bot responses directly to NATS JetStream | Validated (Phase 22) |
| Polymorphic VerbHandler implementations (D-01, D-05) | Extract reply, wait, forward, tag, close, and pause_bot actions into separate structs implementing a shared interface within the webhook package | Validated (Phase 24) |
| Static VerbHandler routing map (D-02) | Map action strings to respective VerbHandler interface instances within the NewVerbsEngine constructor | Validated (Phase 24) |
| Raw JSON parameter delegation (D-03) | Let individual VerbHandlers unmarshal and validate their own parameter schemas from json.RawMessage | Validated (Phase 24) |
| Shared VerbContext passing (D-04) | Resolve contact profile once at execution start and pass down trace/identity context to prevent redundant DB queries | Validated (Phase 24) |
| Populate ConnectionID, SenderIdentity, and TraceID in TypebotForwarder (D-01 to D-03) | Enriched outbound queue messages with metadata to maintain traceability boundaries and support downstream dispatching | Validated (Phase 24.2) |
| Deterministic Typebot message mapping (D-01 to D-09) | Map media messages to [Media Attachment] placeholders and contact identities to custom session formats to act as a proper channel-to-Typebot bridge | Validated (Phase 24.2.1) |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd-transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd-complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-07-20 after v1.3 milestone completion*
