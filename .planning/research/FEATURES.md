# Feature Research

**Domain:** Self-hosted omnichannel CPaaS / messaging gateway (Twilio-replacement for transactional messaging)
**Researched:** 2026-06-25
**Confidence:** HIGH (table-stakes & competitor analysis grounded in official Twilio + Telegram docs fetched directly; WABA specifics MEDIUM — Facebook docs blocked fetching, sourced from established industry knowledge)

---

## Feature Landscape

This research maps the CPaaS feature ecosystem against PerGo's existing PRD scope (sections 5 & 7) and architecture docs. Each feature is tagged with **PRD coverage**: `[COVERED]`, `[PARTIAL]`, `[GAP]`, or `[EXCLUDED]`.

### Table Stakes (Users Expect These)

Features users assume exist. Missing these = product feels incomplete. A backend developer who has used Twilio/Vonage will look for these on day one.

| Feature | Why Expected | Complexity | PRD Coverage | Notes |
|---------|--------------|------------|--------------|-------|
| Unified send API (`POST /messages`) | Single endpoint is the entire value proposition of a CPaaS abstraction | LOW | `[COVERED]` | PRD §5.1; Echo + JSON struct validation + Trace-ID + 202 Accepted. Core. |
| Message status lifecycle (queued→sent→delivered→read→failed) | Developers track delivery; without states you cannot build SLAs or retry logic | LOW | `[COVERED]` | PRD §6 names "Queued, Sent, Delivered, Read, Failed" as audit states. **Gap: the canonical status enum + transitions are not formally specified** (which state is terminal, which is retried, which triggers fallback). Twilio documents 11 states explicitly. |
| Delivery receipts via webhook (status callbacks) | Async send → 202 means the caller MUST get status out-of-band | MEDIUM | `[PARTIAL]` | PRD §5.6 + arch 05 specify outbound webhook delivery via dedicated JetStream consumer with retries + DLQ. **Gap: webhook payload schema, status event types, and HMAC request signing are NOT specified.** Twilio posts `MessageStatus`+`ErrorCode`; Telegram uses `X-Telegram-Bot-Api-Secret-Token` header. Signing is non-negotiable for trust. |
| Webhook request signing (HMAC / shared secret) | Consumers must verify a webhook came from you, not an attacker | LOW | `[GAP]` | Not in PRD or architecture. Twilio: HMAC-SHA1 signature over URL+params; Telegram: `secret_token` header. **Critical gap — a self-hosted gateway that posts unsigned webhooks is unsafe to deploy.** ~30 LoC. |
| Error codes & structured error responses | Callers branch on error type (retry vs abort vs fix payload); string matching is fragile | MEDIUM | `[PARTIAL]` | Arch 05 defines sentinel errors (`ErrQueueFull`, `ErrNoChannel`, `ErrAllFallbackFail`, `ErrInvalidPayload`) + HTTP status mapping. **Gap: no public numeric error code catalog, no `error_code`/`error_message`/`more_info` REST error body** like Twilio's. Sentinels are internal; the external API needs a stable, documented code surface. |
| Trace-ID on every response & webhook | Correlation across async boundaries; debugging | LOW | `[COVERED]` | PRD §5.6 + arch 01: Trace-ID generated at ingest, propagated HTTP→NATS headers→worker ctx→SQL. 100% trace-correlation SLO. Strong. |
| API key authentication | Every CPaaS uses bearer/key auth | LOW | `[COVERED]` | PRD §5.6 + arch 02: SHA-256 hashed keys, prefix cleartext lookup, in-memory cache with TTL. Schema includes `revoked_at` (revocation supported). |
| Multi-tenant workspace isolation | SaaS admin persona needs tenant segregation | MEDIUM | `[COVERED]` | PRD §5.2: workspaces, partitioned `audit_logs` by `workspace_id`. API keys scoped to workspace. |
| Queue + backpressure (reject when full) | Protect downstream; bursty load is normal | MEDIUM | `[COVERED]` | PRD §5.4 + arch 05: NATS JetStream WorkQueue, 1,000-msg/session limit, HTTP 429 + `Retry-After: 5` before enqueue. Best-in-class explicit. |
| Per-channel rate limiting / staggering | Providers ban you for bursts (esp. unofficial WhatsApp) | MEDIUM | `[COVERED]` | PRD §5.4: `golang.org/x/time/rate` token bucket, 1–3s staggered dispatch for WhatsApp Web. |
| Automatic retries with backoff | Transient failures are common; manual retry is unacceptable | MEDIUM | `[COVERED]` | Arch 05: JetStream `MaxDeliver: 5` with exponential `AckWait`/`MaxBackoff`; NAK-with-delay; terminal errors typed to skip retry and advance fallback. |
| Smart fallback across channels | Resilience when a primary channel is down | MEDIUM | `[COVERED]` | PRD §5.5: ordered `fallback_channels` array, iterative dispatch, failure-driven switching. Differentiator-grade but also expected of any "omnichannel" claim. |
| Credential encryption at rest | Self-hosted + data sovereignty = must encrypt secrets | LOW | `[COVERED]` | PRD §6 + arch 02: AES-256-GCM for sessions/credentials, SHA-256 for API keys, std `crypto` only. |
| Audit logging (immutable) | Compliance (GDPR/LGPD) is a stated value prop | MEDIUM | `[COVERED]` | PRD §5.6: immutable partitioned `audit_logs`, buffered batch writer to avoid write contention. |
| Connection/session lifecycle management | WhatsApp Web WebSockets must survive restarts | HIGH | `[COVERED]` | PRD §5.3 + arch 01: whatsmeow device store in PostgreSQL, in-memory registry with `sync.RWMutex`, per-device goroutines, reconnect on restart. |
| Health & readiness endpoints | Orchestrator (k8s/docker) probes | LOW | `[COVERED]` | Arch 05: `/healthz` (liveness), `/readyz` (pgx ping + nats ping). |
| Media message support (image/document/audio) | WhatsApp/Telegram are media-rich; text-only feels broken | HIGH | `[GAP]` | **PRD §7 says "text, media, and structured template payloads" are in scope, but §5 specifies NO media handling.** Twilio: `MediaUrl[]` (up to 10, 5MB), Media subresource. WhatsApp: media via URL or upload ID. Telegram: `sendPhoto`/`sendDocument`/`InputMedia`. **Significant spec gap** — the unified payload needs a media abstraction and the workers need upload/download paths. |
| Message templating (channel-native templates) | WABA REQUIRES pre-approved templates for business-initiated messages outside the 24h window | HIGH | `[GAP]` | PRD mentions "structured template payloads" in §7 but §5 has no template CRUD, no template storage, no `template_name`+`language`+`components` field, no Meta approval workflow integration. **This is WABA-blocking** — you cannot send business-initiated WABA messages without templates. Major gap. |
| Inbound message ingestion (mobile-originated / replies) | 2-way conversation is table-stakes for "messaging"; even transactional flows get replies (STOP, OPT-OUT, "yes") | HIGH | `[GAP]` | PRD is outbound-only (`POST /messages`). No `POST /inbound` or provider→PerGo inbound webhook receiver. Telegram has `getUpdates`/`setWebhook`; WABA has inbound webhooks; WhatsApp Web receives events via whatsmeow event handlers. **Likely a deliberate MVP-scope decision, but must be flagged** — without it PerGo cannot do opt-out/compliance or reply tracking. Recommend v1.x. |
| Idempotency on send (prevent duplicate delivery) | At-least-once queues + retries = risk of sending a message twice to a human | MEDIUM | `[PARTIAL]` | Arch 01 names the challenge: "deduplicated by `trace_id` before dispatch" OR idempotent at provider boundary. **Gap: no explicit idempotency-key field on `POST /messages`, no dedup store specified.** Twilio has no first-class idempotency key either, but recommends caller-supplied. Recommend an optional `Idempotency-Key` header. |
| Per-destination / per-number rate limiting | WhatsApp bans senders that blast one number; per-session limits aren't enough | MEDIUM | `[GAP]` | PRD rate-limits per *session* (per paired device), not per *recipient*. Twilio/SMS carriers enforce per-destination pacing. For unofficial WhatsApp this is a ban-risk mitigation. Medium gap. |
| Webhook retry policy with dead-letter queue | Failed webhook delivery must not silently drop events | MEDIUM | `[COVERED]` | Arch 05: webhook stream `MaxDeliver: 10`, exp `AckWait` (1s→10m), `webhooks_dlq` stream, surfaced on admin console. Excellent. |
| Message validity period / TTL | A queued OTP useless after 5 min should not still send at 30 min | LOW | `[GAP]` | Twilio: `validity_period` (1–36000s, default 36000). Not in PRD. Low complexity, high value for OTP/time-sensitive flows. Recommend `ttl_seconds` field. |
| Sane default + documented payload validation | Bad payloads should fail fast with clear messages | LOW | `[PARTIAL]` | PRD §5.1 mentions schema validation via struct tags. **Gap: validation error response format not specified.** Should return 400 with field-level errors. |
| Timezone-aware scheduling (optional) | Send "tomorrow 9am recipient-local" | MEDIUM | `[GAP]` | Twilio has `send_at` + scheduling. Not in PRD. Defer to v2 unless a real use case appears. |

### Differentiators (Competitive Advantage)

Features that set PerGo apart. These align with the Core Value from PROJECT.md (no markup, no lock-in, full data custody, official + unofficial channels).

| Feature | Value Proposition | Complexity | PRD Coverage | Notes |
|---------|-------------------|------------|--------------|-------|
| Unofficial WhatsApp Web support (whatsmeow) | Send via paired phone accounts — no WABA approval, no per-message Meta cost, no template pre-approval for session messages | HIGH | `[COVERED]` | PRD §5.3/§5.4. Signature differentiator vs Twilio/Vonage (which only do official WABA). Risk: protocol breakage (mitigated by plugin boundary). |
| Self-hosted / full data sovereignty | Transaction data never leaves your infra — GDPR/LGPD-native, no vendor data processing | MEDIUM | `[COVERED]` | Core value. Twilio/Vonage/MessageBird cannot offer this. |
| Zero per-message markup | Cost = infrastructure only; scales without linear API spend | LOW | `[COVERED]` | Core value. The whole reason to replace Twilio. |
| Single binary, <512MB RAM, 2 vCPU | Runs on a $5 VPS; no Kafka cluster, no Redis, no microservices | MEDIUM | `[COVERED]` | Arch 02: three deps earn their place (pgx, nats.go, whatsmeow). Operationally far lighter than commercial CPaaS. |
| Multi-session WhatsApp Web pooling | Many paired devices in one process; per-device isolation | HIGH | `[COVERED]` | PRD §5.3. Most OSS WhatsApp projects handle one session; pooling is a real capability. |
| Smart fallback with terminal-error classification | Don't retry a template-window-expired error; advance to next channel immediately | MEDIUM | `[COVERED]` | Arch 05: `ErrTerminal` typed errors → advance fallback without NAK. Sophisticated vs naive retry loops. |
| Unified payload across official + unofficial WhatsApp | Same JSON sends via WABA or WhatsApp Web — caller doesn't care which | MEDIUM | `[COVERED]` | Implicit in the dispatcher interface (PRD §5.1). Real abstraction value. |
| Operator-grade admin panel with live QR pairing | Self-service device pairing without SSH/CLI | MEDIUM | `[COVERED]` | PRD §5.2: Echo+Templ+HTMX, dynamic QR, connection telemetry, audit review. |
| Explicit backpressure contract (429 + Retry-After) | Callers can implement correct client-side throttling | LOW | `[COVERED]` | Arch 05. Many OSS gateways just 500 or block. |
| AES-256-GCM credential encryption by default | Security not opt-in | LOW | `[COVERED]` | Arch 02. |
| Open-source, auditable, swappable | No vendor lock-in; can fork the router | LOW | `[COVERED]` | Inherent to self-hosted OSS. |
| (Potential) Per-workspace sender routing policy | Tenant A uses WABA, tenant B uses WhatsApp Web pool | MEDIUM | `[PARTIAL]` | Workspaces exist; routing-policy-per-workspace not explicitly specified. Natural extension. |

### Anti-Features (Commonly Requested, Often Problematic)

Features to deliberately NOT build. Includes PRD §7 exclusions plus additional ones supported by research.

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|-----------------|-------------|
| Real-time Voice / WebRTC / SIP trunking | "Full CPaaS = voice" | Massive scope expansion; different latency/protocol stack (Pion, RTP, SIP); orthogonal to transactional messaging. Twilio Voice is a separate product. | Stay text/media/template only (PRD §7). Voice is a different product. |
| Visual conversation flow builder / drag-drop bot designer | "I want to design bots visually" | PerGo is a backend router, not a bot platform. Flow builders become full apps (Chatwoot, Twilio Studio). Bloats scope, couples to UI complexity. | Consumer app implements chat logic via REST + webhooks (PRD §7). |
| Group/community management (create groups, permissions, announcement groups) | "Administer WhatsApp groups" | Different domain (membership CRUD, permissions); high API surface; official APIs limit it. | Direct-message delivery only; Group JID *targeting* allowed but no admin features (PRD §7). |
| Kafka as the broker | "Kafka is the industry standard" | Operational weight (Zookeeper/KRaft, partitions, consumer groups) unjustified at 500 req/s. NATS JetStream gives work-queue + durability in one binary. | NATS JetStream (arch 02). |
| Redis cache layer | "Add Redis for speed" | Unmeasured need at 500 req/s; API-key auth fits in-memory map with TTL. Adds an ops dependency for no proven gain. | In-memory map + TTL; add Redis only if measurement shows hot path (arch 02). |
| ORM / query builder / DI framework | "Productivity" | Hand-written SQL with pgx CollectRows is clearer for a small known query set; ORMs hide SQL and add magic. | Hand-written SQL + pgx (arch 02). |
| OpenTelemetry SDK in MVP | "Standard observability" | Adds a SDK + exporter + tracing backend dependency for a single-binary system. Trace-ID via context+NATS headers+slog meets the 100% correlation SLO. | Explicit Trace-ID propagation; add OTel only if a tracing backend is introduced (arch 02). |
| gRPC internal mesh | "Microservices" | Single binary; REST + JetStream suffice. Adds protobuf tooling burden on callers. | REST/JSON public API; JetStream internal (arch 02). |
| Link shortening + click tracking | "Marketing analytics" | Marketing-feature; pulls in URL storage, click analytics, redirect service. PerGo is transactional. | Defer; consumer app can shorten before sending. |
| Two-way conversational state machine inside PerGo | "Manage conversation context" | Couples router to business logic; session windows belong to the consumer app. | Stateless router; consumer tracks session windows via webhooks. |
| SMS / MMS / RCS channels | "Add SMS too" | Different regulatory stack (A2P 10DLC, toll-free, alphanumeric sender, carrier registration — see Twilio docs). SMS compliance is a product unto itself. | Keep WhatsApp + Telegram focus; add SMS only if a regulated sender stack is wanted. |
| Phone number purchasing / porting | "Buy numbers in-app" | Telecom regulatory workflow; PerGo has no carrier relationships. | Out of scope; users bring their own numbers/accounts. |
| Built-in AI / LLM message generation | "AI-powered messaging" | PerGo routes, it doesn't compose. Couples to model vendors, prompt management. | Consumer app generates content; PerGo delivers. |
| Per-message billing / metering | "Charge tenants per message" | Self-hosted = no billing. Adding metering couples to a payments stack. | Infrastructure-cost model only (PROJECT.md). |

---

## Feature Dependencies

```
[Unified POST /messages API]
    ├──requires──> [API key auth]
    ├──requires──> [Trace-ID generation & propagation]
    ├──requires──> [Payload validation]
    └──requires──> [NATS JetStream queue + backpressure]

[Queue + backpressure]
    └──requires──> [Per-session rate limiter] ──enhances──> [Unofficial WhatsApp safety]

[Channel dispatch workers]
    ├──requires──> [MessageDispatcher interface] (plugin boundary)
    ├──requires──> [Per-channel rate limiting]
    └──requires──> [Circuit breakers] (REST channels only)

[Smart fallback pipeline]
    ├──requires──> [Terminal-error classification] (ErrTerminal typing)
    └──requires──> [Multiple channel adapters]

[WhatsApp Web adapter]
    ├──requires──> [whatsmeow multi-session connection manager]
    ├──requires──> [PostgreSQL device store + AES-256-GCM session encryption]
    └──requires──> [QR pairing admin UI]

[WABA adapter]
    ├──requires──> [Template management + Meta approval workflow]  <-- GAP
    └──requires──> [24h session-window awareness]                  <-- GAP

[Outbound webhook delivery]
    ├──requires──> [Message status lifecycle enum]  <-- PARTIAL
    ├──requires──> [Webhook HMAC request signing]   <-- GAP (critical)
    └──requires──> [Webhook DLQ] (covered)

[Audit logging]
    ├──requires──> [Trace-ID propagation]
    └──requires──> [Buffered batch writer] (covered)

[Inbound message ingestion]  <-- GAP (v1.x)
    └──requires──> [Provider→PerGo inbound webhook receiver / whatsmeow event handler]
    └──requires──> [Inbound webhook forwarding to consumer]

[Media message support]  <-- GAP
    └──requires──> [Unified media field in payload]
    └──requires──> [Per-channel upload/download path]
    └──requires──> [Media storage or URL-pass-through policy]
```

### Dependency Notes

- **WABA adapter requires template management.** This is the hardest dependency: business-initiated WABA messages *cannot* be sent without a Meta-approved template. PerGo's WABA adapter is blocked from real use until template CRUD + storage + approval-status tracking exists. This should reshape milestone planning — template management is not optional for WABA, it is a prerequisite.
- **Webhook signing must precede any production webhook consumer.** A self-hosted gateway posting unsigned webhooks is a security hole. Low effort, high urgency.
- **Media support unblocks multiple channel capabilities.** Without it, the "media" claim in PRD §7 is unfulfilled and WhatsApp/Telegram feel crippled.
- **Inbound ingestion is the gateway to 2-way features** (opt-out compliance, reply tracking, conversation sessions). Outbound-only is defensible for MVP but blocks compliance-heavy use cases.
- **Per-session rate limiting does NOT substitute for per-destination limiting.** A single session blasting one recipient still triggers bans. They are complementary, not interchangeable.

---

## MVP Definition

### Launch With (v1) — Minimum Viable

The smallest coherent product that delivers the Core Value (unified send + fallback + self-hosted) AND is safe to deploy.

- [x] Unified `POST /messages` + 202 + Trace-ID — the entire value prop
- [x] API key auth (hashed, prefix lookup, revocation)
- [x] NATS JetStream queue + 1,000/session backpressure (429 + Retry-After)
- [x] WhatsApp Web adapter (whatsmeow) + QR pairing + session persistence
- [x] Telegram adapter
- [x] Smart fallback pipeline with terminal-error classification
- [x] Outbound webhook delivery (durable JetStream consumer + DLQ)
- [ ] **Webhook HMAC request signing — GAP, must add before launch (security)**
- [ ] **Formal message status enum + webhook payload schema — GAP, must specify**
- [ ] **Public REST error body (`code`, `message`, `more_info`) — GAP, must specify**
- [x] Audit logging (partitioned, buffered batch writer)
- [x] AES-256-GCM credential encryption
- [x] Admin panel (workspaces, QR, telemetry, audit review)
- [x] Health/readiness + pprof + expvar + slog

### Add After Validation (v1.x) — Once Core Outbound Is Proven

- [ ] WABA adapter **with template management** — trigger: first use case needing official channel / higher volume / template compliance. **Note: if WABA is in the launch milestone (PRD milestone 3), template management MUST move to v1 — it is not optional for WABA.**
- [ ] Media message support (image/document/audio) — trigger: first use case needing anything beyond text
- [ ] Inbound message ingestion (MO) + forward-to-consumer webhooks — trigger: first 2-way / opt-out / reply-tracking requirement
- [ ] Message `ttl_seconds` / validity period — trigger: OTP or time-sensitive flows
- [ ] Per-destination rate limiting — trigger: first ban incident or high-volume single-recipient campaign
- [ ] Optional `Idempotency-Key` header + dedup — trigger: duplicate-delivery reports
- [ ] Per-workspace sender routing policy — trigger: multi-tenant channel divergence

### Future Consideration (v2+) — Defer Until Product-Market Fit

- [ ] Message scheduling (send_at) — defer unless a concrete scheduling use case appears
- [ ] Content redaction / PII retention controls — defer; self-hosted already gives data custody
- [ ] SMS / RCS channel — defer; regulatory stack is a separate product
- [ ] OpenTelemetry tracing export — defer until a tracing backend is introduced
- [ ] Prometheus metrics exporter — defer until scraping infra exists (arch 02)
- [ ] Contact / consent management API — defer; consumer app owns consent
- [ ] Per-key quotas / spend limits — defer; self-hosted has no billing

---

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority | PRD Status |
|---------|------------|---------------------|----------|------------|
| Unified POST /messages | HIGH | LOW | P1 | COVERED |
| API key auth | HIGH | LOW | P1 | COVERED |
| Queue + backpressure | HIGH | MEDIUM | P1 | COVERED |
| WhatsApp Web adapter + QR | HIGH | HIGH | P1 | COVERED |
| Telegram adapter | HIGH | MEDIUM | P1 | COVERED |
| Smart fallback | HIGH | MEDIUM | P1 | COVERED |
| Audit logging | HIGH | MEDIUM | P1 | COVERED |
| Webhook delivery + DLQ | HIGH | MEDIUM | P1 | COVERED |
| **Webhook HMAC signing** | **HIGH** | **LOW** | **P1** | **GAP — add** |
| **Message status enum + webhook schema** | **HIGH** | **LOW** | **P1** | **PARTIAL — specify** |
| **Public REST error body/codes** | **HIGH** | **MEDIUM** | **P1** | **PARTIAL — specify** |
| Credential encryption | HIGH | LOW | P1 | COVERED |
| Admin panel | MEDIUM | MEDIUM | P1 | COVERED |
| WABA adapter | HIGH | MEDIUM | P1* | COVERED (adapter) / GAP (templates) |
| **WABA template management** | **HIGH (WABA-blocking)** | **HIGH** | **P1 if WABA in v1** | **GAP — add** |
| **24h session-window awareness** | **HIGH (WABA)** | **MEDIUM** | **P1 if WABA in v1** | **GAP — add** |
| Media message support | HIGH | HIGH | P2 | GAP |
| Inbound message ingestion | HIGH | HIGH | P2 | GAP (deliberate?) |
| Per-destination rate limiting | MEDIUM | MEDIUM | P2 | GAP |
| Message TTL / validity period | MEDIUM | LOW | P2 | GAP |
| Idempotency-Key dedup | MEDIUM | MEDIUM | P2 | PARTIAL |
| Per-workspace routing policy | MEDIUM | MEDIUM | P2 | PARTIAL |
| Message scheduling | LOW | MEDIUM | P3 | GAP |
| Content redaction / PII controls | LOW | LOW | P3 | GAP |
| SMS / RCS channel | LOW | HIGH | P3 | EXCLUDED |
| Voice / WebRTC | LOW | HIGH | P3 | EXCLUDED (PRD §7) |
| Visual flow builder | LOW | HIGH | P3 | EXCLUDED (PRD §7) |
| Group management | LOW | HIGH | P3 | EXCLUDED (PRD §7) |

\* WABA adapter priority depends on whether WABA ships in MVP. If yes, template management is co-P1.

---

## Competitor Feature Analysis

| Feature | Twilio (commercial CPaaS) | Vonage / MessageBird | OSS (Chatwoot/Whaticket/Evolution-API) | PerGo Approach |
|---------|---------------------------|----------------------|----------------------------------------|-----------------|
| Unified send API | `POST /Messages` (SMS/MMS/WhatsApp/RCS/Messenger) | `POST /messages` (SMS/WhatsApp/Messenger) | Per-channel endpoints, often WhatsApp-only | `POST /messages` unified across WhatsApp Web/WABA/Telegram |
| Channels | SMS, MMS, WhatsApp (WABA), RCS, Facebook Messenger | SMS, WhatsApp, Messenger, Viber | Mostly WhatsApp (official + unofficial); some Telegram | WhatsApp Web (unofficial) + WABA + Telegram — NO SMS |
| Unofficial WhatsApp | No (official WABA only) | No | Yes (whatsmeow/baileys-based) | Yes — primary differentiator |
| Self-hosted / data custody | No (cloud only) | No | Yes | Yes — core value |
| Per-message markup | Yes (the cost being avoided) | Yes | No (self-hosted) | No — core value |
| Message statuses | 11 documented (queued, sending, sent, failed, delivered, undelivered, receiving, received, accepted, scheduled, read, canceled) | Similar enum | Often ad-hoc | 5 named (Queued, Sent, Delivered, Read, Failed) — **needs formalization** |
| Webhook signing | HMAC-SHA1 signature (well-documented) | Signed webhooks | Often unsigned (!) | **GAP — must add HMAC** |
| Error codes | Documented numeric catalog + `more_info` URL | Documented error codes | Ad-hoc | Internal sentinels only — **needs public catalog** |
| Templates | Content API / Content Editor (cross-channel templates) | Per-channel template mgmt | WhatsApp template support varies | **GAP — no template management** |
| Media | `MediaUrl[]`, Media subresource, 5MB | Media support | Varies | **GAP — not specified** |
| Inbound / 2-way | Full inbound + Conversations API | Full inbound | Yes (it's their focus) | **GAP — outbound only** |
| Scheduling | `send_at` + Messaging Services | Yes | Rare | Not in scope (v2+) |
| Rate limiting | Per-account + market throughput | Per-account | Ad-hoc | Per-session + staggered (strong for unofficial) |
| Multi-tenancy | Subaccounts, Messaging Services, Multi-Tenancy feature | Subaccounts | Workspaces (varies) | Workspaces + scoped API keys (good) |
| Compliance tooling | A2P 10DLC, toll-free verification, Consent API, SMS Pumping Protection | Compliance offerings | Minimal | GDPR/LGPD via self-hosting + audit logs (different angle) |
| Observability | Insights dashboards, Voice/Messaging Insights | Dashboards | Minimal | pprof + expvar + slog + Trace-ID (operator-grade, no fancy UI) |
| Flow builder | Twilio Studio | Vonage Studio | Some (Chatwoot) | Explicitly excluded (PRD §7) |
| Voice/Video | Full Voice + Video | Voice | No | Explicitly excluded (PRD §7) |

**Strategic read:** PerGo deliberately occupies the *self-hosted, no-markup, unofficial-WhatsApp, transactional-outbound* niche that neither commercial CPaaS (Twilio/Vonage — cloud, markup, official-only) nor typical OSS projects (Chatwoot — inbox-first, not a router; Whaticket/Evolution — WhatsApp-centric, weaker multi-tenant/audit) fill well. The differentiators are real. The gaps are in *table-stakes completeness* (webhook signing, error codes, templates, media, inbound) — not in the differentiation thesis.

---

## Coverage Gap Analysis vs Existing PRD

This is the key deliverable: what the PRD covers vs what is missing that users would expect.

### Gaps That Block Production Deployment (must address before launch)

1. **Webhook HMAC request signing — CRITICAL.** The PRD/architecture specify *durable* webhook delivery but not *authenticated* delivery. Unsigned webhooks are unsafe. ~30 LoC, low effort. (Twilio HMAC-SHA1, Telegram `secret_token` header.)
2. **Formal message status enum + transitions.** PRD §6 lists states but doesn't formalize the state machine (which are terminal, which retry, which trigger fallback, which emit webhooks). Needed for a stable external contract.
3. **Public REST error response format + error code catalog.** Architecture 05 has internal sentinels; the external API needs a documented `{ "code": int, "message": str, "more_info": url }` body so consumers can branch programmatically (Twilio's is the reference).

### Gaps That Block Specific Channels (WABA especially)

4. **WABA template management — WABA-BLOCKING.** The PRD milestone 3 includes WABA, but §5 specifies no template storage, CRUD, Meta approval-status tracking, or `template_name`/`language`/`components` send fields. Business-initiated WABA messages outside the 24h window REQUIRE approved templates. Either move template management into the WABA milestone or accept WABA can only do session-window messages at launch.
5. **24-hour customer service window awareness.** WABA enforces a 24h window after a user message; outside it, only templates are allowed. The routing/fallback engine needs to know this to avoid futile dispatches and to trigger fallback correctly. Not in PRD.

### Gaps in Stated-but-Unspecified Scope

6. **Media message support.** PRD §7 explicitly says "text, media, and structured template payloads" are in scope, but §5 specifies no media handling. Either narrow the §7 claim to "text + templates" or add media to the spec (unified media field, per-channel upload/download, storage policy). High complexity.
7. **Inbound message ingestion (MO).** Not mentioned. Likely a deliberate MVP-scope choice (PerGo = outbound router), but 2-way/opt-out/compliance flows will need it. Flag as v1.x and confirm the deliberate-scope reading with the orchestrator.

### Gaps in Robustness/Quality (v1.x)

8. **Per-destination rate limiting.** Per-session limiting doesn't protect against single-recipient blasting (a WhatsApp ban risk). Complementary to per-session.
9. **Message TTL / validity period.** Low effort, high value for OTP/time-sensitive flows. `ttl_seconds` field.
10. **Idempotency key.** At-least-once queues risk duplicate delivery to humans. Optional `Idempotency-Key` header + short dedup window.
11. **Validation error response format.** Spec validation exists; the 400 body format doesn't.
12. **Per-workspace sender routing policy.** Workspaces exist; explicit routing policy per workspace is a natural multi-tenant feature.

### Confirmed Well-Covered (no action)

- Unified send API, API key auth, Trace-ID, queue+backpressure, per-session rate limiting + staggering, retries+backoff, smart fallback with terminal-error typing, circuit breakers (REST channels), audit logging (partitioned + buffered), credential encryption, connection lifecycle, webhook DLQ, health/readiness, observability (pprof/expvar/slog), multi-tenant workspaces, admin panel with QR.

### Deliberate Exclusions (confirmed, keep)

- Voice/WebRTC, group management, visual flow builder (PRD §7) — correct exclusions.
- Kafka, Redis, gRPC mesh, ORM, OpenTelemetry-in-MVP (arch 02) — correct for the scale/posture.
- SMS/RCS, phone number purchasing, billing/metering, AI generation — recommended additional exclusions (see Anti-Features).

---

## Sources

- **Twilio Programmable Messaging — Messages resource** (official docs, fetched 2026-06-25): message status values (11 states), message properties (`direction`, `errorCode`, `numSegments`, `numMedia`, `price`), `statusCallback`, `MediaUrl[]` (5MB, up to 10), `contentSid`/`contentVariables` (Content API templates), `validity_period`, `attempt`, `scheduleType`/`sendAt`, `contentRetention`/`addressRetention`, `smart_encoded`, `shortenUrls`, `riskCheck`, signature validation. **Confidence: HIGH.** URL: https://www.twilio.com/docs/messaging/api/message-resource
- **Telegram Bot API** (official docs, fetched 2026-06-25, Bot API 10.1 / June 11 2026): `getUpdates` vs `setWebhook` (mutually exclusive), `secret_token` → `X-Telegram-Bot-Api-Secret-Token` header (webhook auth), webhook retry-on-non-2xx + give-up, `WebhookInfo` (`pending_update_count`, `last_error_date`, `last_error_message`), supported ports (443/80/88/8443), `max_connections` (1-100), local Bot API server option, rich `Update` types (message, edited_message, callback_query, business_message, message_reaction...), media types (animation/audio/document/photo/video/voice/sticker/location/contact/poll), `error_code`+`description`+`ResponseParameters`. **Confidence: HIGH.** URL: https://core.telegram.org/bots/api
- **WhatsApp Cloud API (WABA)** — Facebook docs blocked fetching (HTTP 400). Facts (24-hour customer service window, template requirement for business-initiated messages, template categories, media via URL/upload ID) sourced from established industry knowledge. **Confidence: MEDIUM** — stable, well-documented industry facts but not re-verified against primary source this session. Recommend phase-specific verification when WABA milestone is planned.
- **PerGo PRD** (`docs/PRD PerGo.md` §5, §6, §7) and **architecture docs** (`docs/architecture/01-06`) — read directly; authoritative for current scope, exclusions, and technical posture. **Confidence: HIGH** (primary source for the project itself).
- **Self-hosted OSS comparisons** (Chatwoot, Whaticket, Evolution API, UniMsg) — general ecosystem knowledge; not re-verified by fetch this session. **Confidence: MEDIUM.**

---
*Feature research for: self-hosted omnichannel CPaaS / messaging gateway (Twilio-replacement, transactional)*
*Researched: 2026-06-25*
