# Project Research Summary

**Project:** PerGo — self-hosted omnichannel CPaaS / messaging gateway (Twilio-replacement for transactional messaging)
**Domain:** Self-hosted CPaaS gateway in Go (unofficial WhatsApp Web via whatsmeow + official WABA + Telegram, NATS JetStream durability, PostgreSQL, server-rendered admin console)
**Researched:** 2026-06-25
**Confidence:** HIGH

## Executive Summary

PerGo is a single-binary, self-hosted messaging gateway that abstracts WhatsApp Web (unofficial, via whatsmeow), WhatsApp Cloud API (WABA), and Telegram behind one `POST /messages` REST endpoint with ordered channel fallback, durable delivery, and multi-tenant workspaces. Experts build this class of system as a **durable work-queue pipeline**: a thin ingestion handler (two I/O max — cached API-key lookup + broker publish — returning 202 in ≤50ms p99), NATS JetStream `WorkQueuePolicy` as the durability boundary, stateless channel workers behind a plugin `Dispatcher` interface, an async bounded-buffer audit fan-in written via `pgx.CopyFrom`, and PostgreSQL as the system of record for identity + audit only. The PRD/architecture docs already prescribe this shape; research validates it against external evidence (official NATS, whatsmeow sqlstore, and pgx v5 docs, plus a production whatsmeow reference implementation).

The recommended approach is to keep all seven PRD-stack choices (Echo, templ+HTMX, NATS JetStream, pgx/v5, whatsmeow, x/time/rate, log/slog) — **all validated** — with three urgent corrections: target **Echo v5** (not v4; v4 EOLs 2026-12-31), use canonical import `go.mau.fi/whatsmeow`, and pin a one-driver PostgreSQL stack by bridging whatsmeow/goose onto pgx via `pgx/v5/stdlib` instead of `lib/pq`. Floor is Go 1.25 (toolchain 1.26.4). Surrounding gaps the PRD left under-specified — migrations (goose/v3), UUID (google/uuid — already a whatsmeow transitive dep), config (env vars, no daemon), testcontainers for integration tests, goreleaser for distribution — are now pinned.

The top risks are not technology questions but **product-completeness and durability-correctness** gaps. Feature research finds the PRD covers the differentiation thesis (unofficial WhatsApp, self-hosted, no markup, pooling, fallback) well but leaves **table-stakes completeness** gaps: webhook HMAC request signing (crITICAL, ~30 LoC), a formal message status enum + webhook payload schema, and a public REST error code catalog — all unsafe-to-deploy absences. WABA as planned in milestone 3 is **blocked** without template management + 24h session-window awareness. Architecture/pitfalls research finds the single most likely production bug is **duplicate message delivery to a human** (at-least-once JetStream redelivery with no `dispatched_messages` dedup set specified), and an under-acknowledged **WhatsApp account-ban** risk (whatsmeow clients are actively force-logged-out, not merely subject to "protocol changes") — `events.LoggedOut` must be a terminal session state driving fallback, and the QR-pairing UI must warn operators. Multi-tenant isolation is currently a convention, not a constraint (no RLS, no enforced `WHERE workspace_id`); AES-GCM encryption has no nonce management, key custody, or rotation story. All of these must be designed into M1 (schema decisions are expensive to retrofit) or M2 (durability machinery), not deferred to M3.

## Key Findings

### Recommended Stack

All seven PRD core choices are validated as correct for 2026; three require version/import-path corrections, and the supporting stack (migrations, UUID, config, testing, release tooling) was under-specified and is now pinned. Full detail in `STACK.md`.

**Core technologies (all validated, all required by Go 1.25 floor):**
- **Go 1.26.x toolchain (1.25 floor)** — every heavy dep declares `go 1.25.0`; standardise the team/CI toolchain to avoid surprise toolchain fetches.
- **Echo v5.2.1** (`labstack/echo/v5`) — HTTP router + middleware; v5 is the current major line (since 2026-01-18) with native `*slog.Logger`; **do NOT start on v4** (EOL 2026-12-31).
- **NATS JetStream** (`nats.go` v1.52.0 + Server 2.10+) — durable `WorkQueuePolicy` stream (ack-deletes, single consumer per subject, `MaxDeliver` retry); far less operational weight than Kafka at 500 req/s.
- **PostgreSQL 16+** via **pgx/v5** (v5.10.0) — sole datastore (no Redis); `pgxpool.Pool` for PerGo + `pgx/v5/stdlib` bridge for whatsmeow/goose (both speak `database/sql`) → **one** PG driver stack.
- **whatsmeow** (`go.mau.fi/whatsmeow`, dated pseudo-version — **no semver tags**, pin deliberately) — the only viable Go WhatsApp Web multi-device library; `sqlstore.Container` supports Postgres; writes device keys plaintext (see gaps).
- **a-h/templ** (v0.3.1020) + **HTMX 2.x** (`htmx.org@2.0.10`) — compile-time type-safe HTML + server-driven fragments for the operator console; avoid htmx v4 beta.
- **log/slog** (stdlib) + **golang.org/x/time/rate** (v0.15.0) — `Limiter.Wait(ctx)` yields the P while pacing unofficial dispatch 1–3s.

**Supporting:** goose/v3 (v3.27.1, embedded migrations via `go:embed`), google/uuid (v1.6.0 — already a whatsmeow transitive dep, avoids a second UUID lib), caarlos0/env/v11 (optional; plain `os.Getenv` also fine), testcontainers-go (v0.43.0, integration tests), goreleaser/v2 (release). Prometheus client is **optional — only if a scraping infra exists**. **Do NOT use:** Echo v4, `lib/pq`, Kafka/Redis, ORM/query builder, viper/YAML config daemon, htmx v4 beta, zerolog/zap, OpenTelemetry SDK in MVP, gofrs/uuid (google/uuid already present).

### Expected Features

Feature research (`FEATURES.md`) maps the CPaaS ecosystem against the PRD. The differentiation thesis is well-covered; table-stakes completeness has gaps.

**Must have (table stakes — PRD COVERED):** unified `POST /messages` + 202 + Trace-ID; API-key auth (SHA-256 + prefix + revocation, cached); NATS JetStream queue + 1,000/session backpressure (429 + `Retry-After: 5`); per-session staggered rate limiting; retries with backoff + terminal-error classification; smart ordered fallback; AES-256-GCM credential encryption; partitioned audit logging with buffered batch writer; multi-tenant workspaces with scoped keys; WhatsApp Web session lifecycle + QR pairing; health/readiness + pprof/expvar/slog; admin panel.

**Must add before launch (PRD GAPS — production-blocking):**
- **Webhook HMAC request signing** — a self-hosted gateway posting unsigned webhooks is unsafe to deploy. ~30 LoC. (Twilio HMAC-SHA1, Telegram `secret_token` header.)
- **Formal message status enum + transitions** + **webhook payload schema** — PRD §6 lists states but doesn't formalize the state machine (terminal/retry/fallback-trigger/webhook-emitting).
- **Public REST error body `{"code","message","more_info"}`** + error code catalog — internal sentinels exist; the external API needs a stable documented surface.

**Must add if WABA is in v1 (WABA-BLOCKING gaps):**
- **WABA template management** (CRUD + Meta approval-status tracking + `template_name`/`language`/`components` send fields) — business-initiated WABA messages outside the 24h window REQUIRE approved templates.
- **24h customer-service window awareness** — routing must know the window to avoid futile dispatch and trigger fallback correctly.

**Should have (v1.x — defer after core outbound proven):** media message support (image/document/audio — PRD §7 claims "media" but §5 specifies none; **significant gap**), inbound message ingestion (MO — 2-way/opt-out/compliance; likely a deliberate MVP-scope choice, confirm with orchestrator), per-destination rate limiting (complements per-session; ban-risk mitigation), `ttl_seconds` validity period, `Idempotency-Key` dedup, per-workspace sender routing policy.

**Defer (v2+):** message scheduling (`send_at`), content redaction/PII retention, SMS/RCS (separate regulatory stack), OpenTelemetry tracing export, Prometheus exporter (only when scraping infra exists), contact/consent API, per-key spend limits.

**Anti-features (explicit exclusions, keep):** Voice/WebRTC/SIP, visual flow builder, group management, Kafka/Redis/gRPC mesh, ORM/DI framework, OpenTelemetry-in-MVP, SMS/MMS/RCS, phone-number purchasing, per-message billing/metering, built-in AI generation, two-way conversational state machine inside PerGo (router is stateless; consumer owns session windows), link shortening.

### Architecture Approach

Architecture research (`ARCHITECTURE.md`) validates the PRD's durable work-queue pipeline against external evidence and surfaces 10 structural gaps. The PRD's domain-oriented package layout is correct; two structural refinements emerge: `platform/postgres/` must host **two** pool constructors (`pgxpool.Pool` for PerGo + `*sql.DB` for whatsmeow's `sqlstore`) sharing one database, and `platform/migrations/` must run **two** migration systems at boot (goose for PerGo tables + `Container.Upgrade(ctx)` for whatsmeow's `whatsmeow_*` tables — non-overlapping schemas, same DB).

**Major components:**
1. **Ingestion gateway (Echo)** — parse, validate, attach Trace-ID, enforce backpressure, publish to broker, return 202; two I/O max on hot path (cached auth + publish).
2. **NATS JetStream `WorkQueuePolicy` stream** — durability boundary; single consumer per subject, ack-deletes; at-least-once (NOT exactly-once — dedup is a hard requirement, not optional).
3. **Channel worker pool** — N pull-consumer goroutines (`Fetch(10)` + `FetchMaxWait`) driving the in-process `RoutingEngine`; **topology A (single consumer + in-process RoutingEngine)** is the recommended default — a second durable consumer on the same stream trips JetStream's "overlapping consumers" error.
4. **Routing engine** — sequential fallback (parallel = N duplicate sends; `errgroup` explicitly wrong); `channel.Terminal` errors advance immediately without redelivery.
5. **Channel adapters** — consumer-side `Dispatcher` interface with `whatsappweb` / `whatsappcloud` / `telegram` siblings; the **load-bearing plugin boundary** that isolates unofficial-protocol breakage from core.
6. **Session manager** — `sync.RWMutex` + `map[JID]*Session` (NOT `sync.Map`); one goroutine per device; per-session `*rate.Limiter` (1–3s jittered, burst 1) — the mechanism that makes 500 msg/s reachable on 2 vCPU.
7. **Audit engine** — bounded `chan Event` (cap 5000) + M batch-writer goroutines → `pgx.CopyFrom` **via `pool.Acquire`** (CopyFrom is connection-level, NOT pool-level); `Record` is non-blocking (drop+count on full buffer; track via `audit_dropped` expvar).
8. **Webhook delivery** — separate `LimitsPolicy` JetStream stream + consumer, `MaxDeliver=10`, exp backoff, `webhooks_dlq`; **no auto-DLQ for the messages stream** (MaxDeliver-exhausted messages stay in stream — must add an advisory listener + `messages_dlq`).

### Critical Pitfalls

Top pitfalls from `PITFALLS.md` (CRITICAL/HIGH severity, mapped to prevention phase):

1. **WhatsApp actively bans whatsmeow-connected accounts** (CRITICAL) — NOT merely "protocol changes"; server-side detection force-logs-out clients (`events.LoggedOut`, 403 "You have been logged out for using an unofficial app"), session deleted server-side, unrecoverable. whatsmeow issue #810 shows bans hitting non-bulk clients. **Avoid:** treat unofficial WhatsApp Web as disposable; fallback must always include an official channel downstream; `LoggedOut` is a **terminal** session state (auto-disable, alert operator, do NOT retry); warn at QR-pairing time; recommend Meta Verified. Address in **M2** (terminal-session handling) + **M3** (fallback treats logged-out as trigger).
2. **Duplicate message to a human on JetStream redelivery** (CRITICAL) — NATS is at-least-once; a worker crash between `Dispatch` and `Ack` redelivers → second send. WhatsApp sends are NOT provider-idempotent. **Avoid:** maintain a short-TTL `dispatched_messages` (or in-memory `map[traceID]channel`) dedup set checked **before** `Dispatch`; Ack-and-skip on hit; record *which channel succeeded* so a redelivery doesn't re-attempt the next fallback channel. **The single most likely production bug.** Address in **M2** (dedup set + ack semantics) + **M3** (per-channel-succeeded dedup).
3. **Goroutine leaks from forgotten senders and untracked session lifetimes** (HIGH) — unbuffered result channels block forever once the receiver cancels; `Registry.Remove` before `wg.Wait()` causes reconnect races; the audit `Close()` panics on concurrent `Record`. **Avoid:** every session goroutine selects on `<-ctx.Done()` AND `<-s.done`; `manager.Stop` `wg.Wait()`s before removing from registry; buffer result channels cap 1; guard audit `Close()` with a `closed atomic.Bool` checked in `Record`. Verify via `runtime.NumGoroutine()` returning to baseline through 1000 pair/disconnect cycles. Address in **M2**.
4. **Audit write contention breaches the 50ms p99 budget** (HIGH) — 500 req/s × ~3–5 transitions = 1,500–2,500 audit rows/s; partitioning by `workspace_id` creates hot partitions under a busy tenant; unique dedup constraints on partitioned tables must include the partition key (PostgreSQL limitation). **Avoid:** partition `audit_logs` by **`created_at` range** (NOT `workspace_id` list) for even inserts + instant `DROP PARTITION` retention; `fillfactor=100`, BRIN on `created_at`, aggressive autovacuum; **no unique dedup constraint** on audit (dedup lives upstream at dispatch per Pitfall 2); alert on `audit_dropped`. Decide partition strategy in **M1** before schema creation.
5. **Multi-tenant isolation is a convention, not a constraint** (HIGH) — no RLS, no enforced `WHERE workspace_id`; one forgotten filter = cross-tenant leak; IDOR via `trace_id`/`jid` is possible. **Avoid:** adopt "tenant context is always in `context.Context`" rule — every `platform/postgres` query takes `ctx` and extracts `workspace_id`; use a `tenantQuery` wrapper/linter to make omission a build error; overwrite client-supplied `msg.WorkspaceID` with auth-resolved value; add PostgreSQL RLS as defense-in-depth (connect as non-owner role); integration test that tenant A cannot read tenant B. Address convention in **M1**; harden RLS in **M3**.
6. **AES-256-GCM encryption with unmanaged nonces / co-located key** (HIGH) — nonce reuse breaks confidentiality AND authenticity for all data under that key; env-var key co-located with DB volume defeats the encryption. **Avoid:** fresh `crypto/rand` 12-byte nonce per `Seal` (prepend to ciphertext); envelope encryption (KEK wraps DEK, KEK in separate trust boundary); **`key_id`/`key_version` columns from M1** (rotation is impossible without a schema migration otherwise); redact keys/nonces/decrypted credentials from slog; document KEK custody runbook. Address in **M1**.

## Implications for Roadmap

Based on combined research, the validated ordering is: **M1 Core Foundation must land schema decisions expensive to retrofit; M2 Queue & WhatsApp Web must land durability-correctness machinery and de-risk the fragile unofficial channel; M3 Official Channels + Fallback + Load Testing proves correctness under load and completes defense-in-depth.** Three milestones, hard dependencies between them. The PRD's existing milestone split is validated; the refinements below absorb the gaps.

### Phase 1: Core Foundation (M1)

**Rationale:** Identity, audit, crypto, and trace scaffolding are hard dependencies of every later phase — and the schema decisions (partition strategy, `key_id` columns, tenant-context convention) are expensive to retrofit. Build first.
**Delivers:** Echo server + `cmd/pergo` composition root; PostgreSQL with **both** pool constructors (`pgxpool` + `*sql.DB` for whatsmeow) and **both** migration runners (goose + `Container.Upgrade`); schemas for `workspaces`, `api_keys` (SHA-256+prefix), `devices`, `audit_logs` (partitioned by **`created_at` range**); `platform/trace`, `platform/crypto` (AES-256-GCM with fresh nonces + envelope pattern + `key_id` columns), `platform/backoff`; API-key auth middleware + in-mem cache; audit engine (buffer + batch writer + `pool.Acquire`→`CopyFrom`); Templ+HTMX admin shell (workspaces, key gen; QR deferred to M2); `/healthz` + `/readyz` + pprof on `localhost:6060`; graceful shutdown scaffolding (root ctx, `Echo.Shutdown`, 30s ceiling). **Tenant-context convention enforced from the first query** (linter/wrapper).
**Addresses features:** API key auth, audit logging, credential encryption, multi-tenant workspaces (enforced), health/readiness, admin panel shell.
**Avoids pitfalls:** P4 (audit partition strategy), P5 (tenant convention from day one), P6 (nonce/key_id schema). NATS is NOT brought up beyond a connectivity check — ingest returns 503 until M2; keeps M1 focused on identity + audit.

### Phase 2: Queue & WhatsApp Web (M2)

**Rationale:** The broker is the durability boundary and WhatsApp Web is the highest-risk channel (ban risk + protocol fragility) — de-risk both before adding the easier stateless official channels. Also where the leak surface (session lifecycle, audit Close race) concentrates.
**Delivers:** NATS JetStream `WorkQueuePolicy` stream (`messages.<ws>.<channel>` subjects) with **`MaxMsgsPerSubject=1000` + `DiscardNew`** for native broker backpressure; `messaging/queue` publish **with `Nats-Msg-Id = trace_id`** for publish-side dedup (GAP #1); `messaging/worker` pull consumer (topology A: single consumer + in-process `RoutingEngine`); `session` package (registry, per-session `rate.Limiter`, manager with backoff reconnect); **startup reconnect-storm protection** (semaphore cap ~8 + jittered backoff over `GetAllDevices`); **WA Web version auto-refresh** (`SetWAVersion` + retry on "client outdated"); `channel/whatsappweb` adapter; in-memory backpressure counter reconciled against `StreamInfo` every 10s; **`dispatched_messages` / in-memory dedup set checked before `Dispatch`** (Pitfall 2); **`events.LoggedOut` as terminal session state** (auto-disable, alert, no retry loop — Pitfall 1); ingest handler wired to real publish (429 on `ErrQueueFull`); QR pairing admin page (live refresh via HTMX/SSE) **with prominent ban-risk warning**.
**Addresses features:** Queue + backpressure, per-session rate limiting + staggering, WhatsApp Web adapter + QR pairing, retries + backoff, connection lifecycle.
**Avoids pitfalls:** P1 (terminal-session handling), P2 (dedup set + ack semantics), P3 (session lifecycle discipline, audit Close race fix). **Load-test the ingest path here** (202 throughput, 429 backpressure) before adding channels.

### Phase 3: Official Channels, Fallback, Webhooks & Prove-It (M3)

**Rationale:** Official channels are stateless REST and lower-risk; fallback needs ≥2 channels to demonstrate; this is where correctness is proven under load and where defense-in-depth completes. WABA lands here — only viable once templates + 24h window awareness exist.
**Delivers:** `channel/whatsappcloud` (WABA REST) **with template management + Meta approval-status tracking + `template_name`/`language`/`components` send fields + 24h session-window awareness** (WABA-blocking gaps) and `channel/telegram` (Bot REST), each behind `platform/breaker` (breaker open → fallback trigger, not retry — breaker is per-provider-REST, NOT around whatsmeow); `RoutingEngine` sequential fallback with `channel.Terminal` typing; **per-channel-succeeded dedup across the fallback pipeline** (Pitfall 2); webhook delivery (second JetStream stream + consumer, `MaxDeliver=10`, exp backoff, `webhooks_dlq`) **with HMAC request signing + formal payload schema + message status enum + public REST error catalog** (production-blocking gaps); `messages_dlq` via MaxDeliver advisory listener + admin view; **PostgreSQL RLS hardening** (Pitfall 5); end-to-end load test (500 req/s sustained, 50ms p99 ingest, 99.5% delivery, <512MB RAM, `audit_dropped==0`, `runtime.NumGoroutine()` returns to baseline through 1000 pair/disconnect cycles); 30/60/90-day eval metrics via `expvar`.
**Addresses features:** Smart fallback with terminal-error classification, circuit breakers (REST channels), webhook delivery + DLQ, **webhook HMAC signing**, **status enum + schema**, **REST error catalog**, WABA adapter + templates + 24h window, Telegram adapter, admin panel DLQ views.
**Avoids pitfalls:** P1 (fallback treats logged-out as trigger), P2 (fallback-succeeded dedup), P5 (RLS defense-in-depth), audit write contention proven at 2,500 events/s, goroutine-leak regression asserted.

### Phase Ordering Rationale

- **M1 → M2 is a hard dependency** — identity + audit must exist before any message flows; ingest can stub 503 until M2 wires the real publish (keeps M1 focused).
- **M2 → M3 is a risk-ordering dependency** — WhatsApp Web is the fragile, unofficial channel (ban + protocol breakage); de-risk it before the easy stateless official ones. Bringing official channels into M2 would split attention from the highest-risk integration. Fallback needs ≥2 channels, so it lands in M3 by construction.
- **Schema decisions are made in M1, not deferred** — `created_at` audit partitioning, `key_id` columns, tenant-context convention. Retrofitting these is a migration.
- **Durability machinery is made in M2, not deferred** — dedup set, terminal-session handling, goroutine lifecycle. These are core to correctness, not features.
- **M1/M2 pitfalls must NOT be deferred to M3** — the schema and concurrency structure are set in M1/M2 and retrofits are painful. M3 is for *proving* correctness under load and completing defense-in-depth (RLS), not for first implementing it.

### Research Flags

Phases likely needing deeper research during planning (`/gsd-plan-phase --research-phase`):
- **Phase 2 (M2):** **WhatsApp ban-risk + whatsmeow version drift surface.** whatsmeow issue #810 is still open and enforcement changes land without warning — the terminal-session handling design and the WA Web version auto-refresh mechanism warrant phase-specific research against the latest whatsmeow state. Also the JetStream `Nats-Msg-Id` dedup + consumer topology decision benefits from a focused re-check.
- **Phase 3 (M3) — WABA substream:** **WABA template + 24h window specifics.** Facebook/WABA docs were blocked from fetching during research (HTTP 400); WABA facts came from established industry knowledge (MEDIUM confidence). Verify against primary Meta docs when this substream is planned. Also the HMAC signing scheme choice (HMAC-SHA1 a la Twilio vs `secret_token` header a la Telegram) for *outbound* consumer webhooks deserves a brief design pass.
- **Phase 3 (M3) — Webhook payload schema + REST error catalog:** the canonical status enum + transitions (terminal/retry/fallback-trigger/webhook-emitting) and the error code numbering/categorization are unspecified in the PRD — a small design pass is warranted, though it is low external-research (mostly internal contract design).

Phases with standard patterns (skip research-phase):
- **Phase 1 (M1):** Echo v5 + pgx/v5 + goose + Templ/HTMX admin shell are all well-documented; the partition strategy and crypto nonce pattern are settled by this research. Standard patterns apply.
- **Phase 2 (M2) — JetStream worker + session manager:** the pull-consumer pattern and per-session rate-limiter pattern are well-documented in NATS/stdlib docs and validated here; only the whatsmeow version/ban drift sub-surface needs research.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Versions verified against the Go module proxy (canonical source) + cross-referenced against official GitHub/docs; feature/behaviour claims verified against primary source (whatsmeow `sqlstore/container.go`, Echo `API_CHANGES_V5.md`). No training-data version assertions. |
| Features | HIGH (table stakes & competitor analysis) / MEDIUM (WABA) | Twilio + Telegram fetched directly from official docs. WABA specifics sourced from industry knowledge because Facebook docs blocked fetching — re-verify WABA before the M3 WABA substream. |
| Architecture | HIGH | Validated against official NATS JetStream, whatsmeow store/sqlstore, pgx v5 docs, plus a production whatsmeow-based reference implementation. 10 structural gaps surfaced with primary-source citations. |
| Pitfalls | HIGH | Findings drawn from official PostgreSQL/Go/NATS docs and whatsmeow's own GitHub issue tracker (issues #810 ban, #561 force-logout) — primary, authoritative sources. |

**Overall confidence:** HIGH

### Gaps to Address

- **WABA primary-source verification** — Facebook docs blocked fetching this session. WABA facts (24h window, template requirement, template categories, media via URL/upload ID) are stable industry knowledge but should be re-verified against Meta's primary docs when the M3 WABA substream is planned.
- **whatsmeow device-key encryption-at-rest** — whatsmeow's `sqlstore` writes device keys plaintext; the PRD's "AES-256-GCM for session tokens and channel credentials" does NOT cover whatsmeow's internal keys without extra work. Decide explicitly in the relevant phase: (a) accept DB-level/filesystem/full-disk encryption as the boundary, (b) implement a custom `store.DeviceContainer` (significant effort, must track whatsmeow upgrades), or (c) use `pgcrypto` on those columns. **Do not assume whatsmeow encrypts.**
- **Inbound message ingestion scope** — PRD is outbound-only; research treats this as a likely deliberate MVP-scope choice but flags that 2-way/opt-out/compliance flows will need it. **Confirm with orchestrator** whether outbound-only is intentional for v1; if yes, defer inbound to v1.x.
- **Webhook HMAC signing scheme** — Twilio uses HMAC-SHA1 over URL+params; Telegram uses `secret_token` header. Outbound *consumer* webhook signing is an internal contract choice — design pass in M3 (low external research, internal design).
- **API-key hashing strength** — PRD uses SHA-256 with prefix lookup. Acceptable IF keys are high-entropy (32+ random bytes); **do NOT allow user-chosen low-entropy keys** — consider bcrypt/argon2 if human-chosen keys ever emerge.
- **Idempotency at provider boundary (full dedup table)** — research recommends a short-TTL `dispatched_messages` dedup set for MVP, NOT a full `trace_id`-keyed dedup table (over-engineering for MVP). Revisit if a compliance requirement demands exactly-once audit.
- **Prometheus exporter** — only if a scraping infra exists; revisit when a deployment introduces one.

## Sources

### Primary (HIGH confidence)
- **Go module proxy (`proxy.golang.org/@latest`)** — canonical version + release-date verification for echo/v4+v5, templ, nats.go, pgx/v5, whatsmeow, x/time, goose, golang-migrate, gofrs/uuid, google/uuid, caarlos0/env, prometheus/client_golang, backoff, gobreaker, testify, testcontainers-go, goreleaser.
- **Echo GitHub README + `API_CHANGES_V5.md`** — v5 is current major line (2026-01-18), v4 EOL 2026-12-31, breaking-change inventory.
- **NATS JetStream docs** (`docs.nats.io`) — Streams (WorkQueuePolicy single-consumer-per-subject, MaxMsgsPerSubject, DiscardNew), Consumers (pull consumers, AckExplicit, MaxAckPending, MaxDeliver, Backoff-overrides-AckWait, NAK with delay), Model Deep Dive (`Nats-Msg-Id` dedup + DuplicateWindow, no auto-DLQ on MaxDeliver).
- **whatsmeow store/sqlstore godoc** (`pkg.go.dev/go.mau.fi/whatsmeow/store/sqlstore`) — `Container.New/NewWithDB` (sqlite3+postgres dialects, `*sql.DB`), `Upgrade`, `GetDevice` (AD-JID), `GetAllDevices`, `PostgresArrayWrapper`; device keys stored as raw byte columns (plaintext).
- **pgx v5 godoc** — `Conn.CopyFrom` (conn-level NOT pool-level), `CopyFromRows/Slice/Func`, `CollectRows/ForEachRow`, Go 1.25+/PG14+ on v5.10.
- **Twilio Programmable Messaging — Messages resource** (official docs, fetched 2026-06-25) — 11 message states, `MediaUrl[]`, `contentSid`, `validity_period`, `scheduleType`/`sendAt`, signature validation.
- **Telegram Bot API** (official docs, fetched 2026-06-25, Bot API 10.1) — `getUpdates`/`setWebhook`, `secret_token` header (webhook auth), `WebhookInfo` (`pending_update_count`, `last_error_*`), rich `Update` types, `error_code`+`description`+`ResponseParameters`.
- **whatsmeow GitHub issue tracker** — issue #810 (ban-risk warnings, OPEN May 2025), issue #561 (force-logout for unofficial app use, 403).
- **PostgreSQL docs** — §5.12 Table Partitioning (declarative, limitations, unique-key-must-include-partition-key), §5.9 Row Security Policies (BYPASSRLS, covert channels via referential integrity).
- **Go `crypto/cipher` package docs** — AEAD, NewGCM, nonce uniqueness, 2^32 random-nonce ceiling, `NewGCMWithRandomNonce` (Go 1.24+).
- **PerGo PRD** (`docs/PRD PerGo.md` §5–9) and **architecture docs** (`docs/architecture/01-06`) — authoritative for current scope, exclusions, technical posture; coverage assessment against research.

### Secondary (MEDIUM confidence)
- **gdbrns/go-whatsapp-multi-session-rest-api** (GitHub reference impl) — production whatsmeow+PostgreSQL patterns: startup reconnect-storm protection (semaphore + jitter), WA Web version auto-refresh (`SetWAVersion`), 86 webhook event types with retries/quotas, JWT-vs-API-key scoping divergence.
- **Self-hosted OSS comparisons** (Chatwoot, Whaticket, Evolution API, UniMsg) — general ecosystem knowledge; not re-verified by fetch this session.
- **WABA / WhatsApp Cloud API** — Facebook docs blocked fetching (HTTP 400); facts (24h customer service window, template requirement, template categories, media via URL/upload ID) sourced from established industry knowledge. **Re-verify against Meta primary docs before M3 WABA substream.**

### Tertiary (LOW confidence)
- None — all findings trace to primary or established-community sources. The only residual LOW zone is the WABA specifics pending primary-source re-verification (listed under Gaps).

---
*Research completed: 2026-06-25*
*Ready for roadmap: yes*