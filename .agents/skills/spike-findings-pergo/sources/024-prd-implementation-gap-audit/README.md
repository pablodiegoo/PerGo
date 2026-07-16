---
spike: 024
name: prd-implementation-gap-audit
type: standard
validates: "Given the context/ PRD documents, when compared exhaustively against the implemented codebase, then we identify every unimplemented feature and architecture gap"
verdict: VALIDATED
related: [001, 012, 013, 014, 015, 016, 017, 018]
tags: [audit, gaps, prd, architecture]
---

# Spike 024: PRD Implementation Gap Audit

## What This Validates
Given the context/ PRD documents (PRD PerGo.md + PRD-architecture-deepening.md), when compared exhaustively against the implemented codebase (internal/, migrations, domain model), then we produce a complete inventory of unimplemented features and architecture gaps.

## Research

### Documents Analyzed
1. **context/PRD PerGo.md** — Full product requirements (9 sections, 284 lines)
2. **context/PRD-architecture-deepening.md** — Architecture deepening ADRs 0001-0004 (98 lines)
3. **Spike MANIFEST.md** — 23 validated spikes (001–023)
4. **Spike findings skill** — 12 feature areas with references

### Codebase Analyzed
- 67 `.go` source files in `internal/`
- 22 SQL migrations in `internal/platform/postgres/migrations/`
- 13 database tables materialized
- All test files cross-referenced

## Investigation Trail

### Phase 1: Architecture Deepening ADRs (context/PRD-architecture-deepening.md)

Three parallel subagents exhaustively analyzed every Go file referenced by each ADR.

| ADR | Status | Key Finding |
|-----|--------|-------------|
| ADR-0001: DispatchOrchestrator | ✅ Done | Fully extracted. Minor deviation: `Process()` has extra `attempt` param; constructor has 7 deps not 6. |
| ADR-0002: InboundProcessor | ✅ Done | Fully extracted. Design improved: uses channel-agnostic `InboundEvent` instead of raw WhatsApp types. Session handler still ~110 lines (reasonable — CDN download adapter work). |
| ADR-0003: Unify TemplateComponent | ✅ Done | Complete. No duplicate types remain. `domain.TemplateComponent` is single source of truth. |
| ADR-0004: CredentialProvider Port | ✅ Done | Complete. Both `ConnectionRepository` and `CredentialsRepository` use `CredentialProvider` interface. |

### Phase 2: Core PRD Functional Requirements (context/PRD PerGo.md)

| PRD Section | Requirement | Status | Evidence |
|-------------|-------------|--------|----------|
| §5.1 | Unified POST /messages gateway | ✅ | `internal/api/handler/message.go` |
| §5.1 | JSON validation + Trace-ID | ✅ | `internal/api/middleware/trace.go` |
| §5.1 | NATS JetStream queue + 202 Accepted | ✅ | `internal/outbound/processor.go` |
| §5.2 | Multi-tenant dashboard (Echo+Templ+HTMX) | ✅ | `templates/` + admin handlers |
| §5.2 | Workspace management | ✅ | `internal/repository/workspace.go` |
| §5.2 | QR code pairing | ✅ | `internal/session/qr.go` |
| §5.3 | Multi-session connection controller | ✅ | `internal/session/manager.go` + `registry.go` |
| §5.3 | Persistent session store (PostgreSQL) | ✅ | `connections` table, whatsmeow sqlstore |
| §5.3 | Reconnect on restart | ✅ | `manager.go:ReconnectAll()` |
| §5.4 | NATS JetStream WorkQueuePolicy | ✅ | `internal/platform/queue/jetstream.go` |
| §5.4 | 1,000-msg backpressure (HTTP 429) | ✅ | `outbound/processor.go:69-72`, `handler/message.go:87-94` |
| §5.4 | 1-3s staggered dispatch (WhatsApp Web) | ✅ | `channel/whatsapp/adapter.go:23-84` |
| §5.5 | Fallback channels array | ✅ | `domain/message.go:51` + `queue/orchestrator.go:138-241` |
| §5.5 | Iterative dispatch with failure switching | ✅ | Terminal errors advance; transient errors NAK |
| §5.6 | Trace-ID propagation (HTTP→NATS→worker→SQL) | ✅ | Full chain verified |
| §5.6 | Immutable partitioned audit_logs | ✅ | Monthly partitions via `010_fix_audit_partitioning.sql` |
| §5.6 | Buffered batch writer | ✅ | `audit/batch.go` — pgx CopyFrom, 100 batch / 50ms flush |
| §6 | SHA-256 hashed API keys | ✅ | `internal/platform/crypto/hash.go` |
| §6 | AES-256-GCM credential encryption | ✅ | `internal/platform/crypto/encrypt.go` |
| §6 | net/http/pprof profiling | ✅ | `internal/platform/obs/pprof.go` |
| §6 | Structured log/slog logging | ✅ | `internal/platform/obs/logging.go` |

### Phase 3: Validated Spikes vs Implementation

| Spike | Feature | Implemented? | Gap? |
|-------|---------|--------------|------|
| 001 | Multi-instance schema (connections table) | ✅ | — |
| 002 | API routing payload (from field) | ✅ | — |
| 003 | Dynamic adapter registry | ✅ | — |
| 004 | Inbox conversation list | ✅ | — |
| 005 | Inbox chat view (split-pane) | ✅ | — |
| 006 | Inbox realtime polling | ✅ | — |
| 007 | Inbox polling stability | ✅ | — |
| 008 | Connection management unification | ✅ | — |
| 009 | WABA template inbox delivery | ✅ | — |
| 010 | Settings nested sidebar | ✅ | — |
| 011 | Settings layout optimization | ✅ | — |
| 012 | Conversational session schema | ✅ | ⚠️ Uses audit_logs CTEs, no dedicated `conversations` table |
| 013 | Queue-decoupled webhook dispatcher | ✅ | — |
| 014 | HMAC webhook verification | ✅ | — |
| **015** | **Messaging verbs engine** | **❌ NOT IMPLEMENTED** | No verb types, no executor, no webhook response processing |
| 016 | Selective metadata logging | ✅ | — |
| **017** | **Omnichannel contact merging** | **❌ NOT IMPLEMENTED** | No contacts/contact_identities tables, no Go code |
| **018** | **Multi-webhook subscriptions** | **❌ NOT IMPLEMENTED** | Single URL per workspace; no subscription table or event filtering |
| 019 | Session caching router | ✅ | — |
| 020 | Campaign engine | ✅ | — |
| 021 | User action logs | ✅ | — |
| 022 | CSS standardization | ✅ | — |
| 023 | Deprecated workspace subviews | ✅ | — |

### Phase 4: Schema Completeness

| Table | Exists? | Migration | Notes |
|-------|---------|-----------|-------|
| workspaces | ✅ | 001 | Root tenant entity |
| api_keys | ✅ | 001 | SHA-256 hashed |
| audit_logs | ✅ | 001+002+010 | Partitioned by month |
| connections | ✅ | 012 | Consolidated from devices + channel_credentials |
| recipient_sessions | ✅ | 006+013+014 | Composite PK, last_read_at for unread |
| message_dispatches | ✅ | 007+016 | Campaign enrichment columns |
| webhook_configs | ✅ | 008 | Single config per workspace |
| webhook_dlqs | ✅ | 008 | DLQ persistence |
| inbound_dedups | ✅ | 009 | Deduplication tracking |
| waba_templates | ✅ | 005+022 | Connection-scoped |
| telegram_contacts | ✅ | 015 | Telegram-specific (siloed) |
| campaigns | ✅ | 016+022 | Full campaign lifecycle |
| user_action_logs | ✅ | 021 | Polymorphic actors |
| **contacts** | **❌** | — | **Missing: unified customer profiles** |
| **contact_identities** | **❌** | — | **Missing: cross-channel identity linking** |
| **webhook_subscriptions** | **❌** | — | **Missing: multi-URL event-filtered subscriptions** |
| **conversations** | **⚠️** | — | **Optional: currently materialized from audit_logs CTEs** |

## Results

### Verdict: VALIDATED ✓

The audit is complete and comprehensive. 20 of 23 validated spikes are fully implemented in production code. The architecture deepening ADRs (0001-0004) are all done. The core PRD functional requirements (§5.1–§5.6, §6) are fully satisfied.

### Three Unimplemented Spike Features

#### Gap 1: Omnichannel Contact Merging (Spike 017) 🔴 HIGH

**What's missing:**
- `contacts` table (id, name, email, workspace_id)
- `contact_identities` table (id, contact_id FK, channel, sender_identity, UNIQUE)
- `Contact` and `ContactIdentity` domain structs
- `ResolveContact()` — auto-create/link on inbound message receipt
- `MergeContacts()` — transactional merge of secondary into primary
- API endpoints for contact lookup and merge
- Migration of `telegram_contacts` into unified model

**Impact:** Conversations are identified by raw `(from, channel, to)` tuples. A WhatsApp user and Telegram user that are the same person cannot be linked. The `telegram_contacts` table is a channel-specific workaround.

#### Gap 2: Multi-Webhook Subscriptions (Spike 018) 🔴 HIGH

**What's missing:**
- `webhook_subscriptions` table (workspace_id, URL, secret, event_types[], active)
- Subscription registry/resolver with event-type matching (wildcard `message.*`)
- Concurrent fan-out dispatch to multiple matched URLs per event
- Per-subscription DLQ tracking
- Admin UI for managing subscriptions

**Impact:** Currently only one webhook URL per workspace via `webhook_configs`. Operators needing to send events to multiple systems (CRM + analytics + monitoring) must build their own fan-out.

#### Gap 3: Messaging Verbs Engine (Spike 015) 🟡 MEDIUM

**What's missing:**
- Verb type definitions (reply, wait, forward, tag, close)
- Verb executor that processes a sequence of JSON-serialized verbs
- Webhook response parsing — currently webhooks are fire-and-forget; the verbs engine would process response payloads
- Integration with the dispatch pipeline

**Impact:** PerGo can send webhook events but cannot process declarative response instructions. External systems must make separate API calls to reply, which increases latency and complexity for CRM integrations.

### Minor Deviations (Not Gaps)

| Item | Detail | Severity |
|------|--------|----------|
| HTTP 422 for backpressure | PRD says "429 or 422"; code only returns 429 | 🟢 Trivial — 429 is correct for rate-limiting |
| DispatchOrchestrator signature | Extra `attempt int` param | 🟢 Trivial — reasonable enhancement |
| InboundProcessor design | Channel-agnostic `InboundEvent` instead of raw WhatsApp types | 🟢 Improvement — better than PRD spec |
| `conversations` table | Uses audit_logs CTEs instead of dedicated table | 🟡 Performance — works but won't scale to millions |
