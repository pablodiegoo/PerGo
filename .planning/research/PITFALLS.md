# Pitfalls Research

**Domain:** Self-hosted omnichannel CPaaS (unofficial WhatsApp Web + NATS JetStream + PostgreSQL, Go)
**Researched:** 2026-06-25
**Confidence:** HIGH (findings drawn from official PostgreSQL/Go/NATS documentation and whatsmeow's own GitHub issue tracker — primary sources. The provider-level classifier returns LOW because the webfetch/exa providers are generic, but the underlying sources are authoritative.)

> **Scope note:** This file maps pitfalls to PerGo's three-milestone plan
> (M1 Core Foundation, M2 Queue & WhatsApp Web, M3 Official Channels + Fallback + Load Testing)
> and rates each pitfall by severity and by how well the existing PRD/architecture docs
> already address it.

---

## Critical Pitfalls

These cause rewrites, data loss, account termination, or SLO violations. Each must be
prevented in a specific phase, not discovered in production.

### Pitfall 1: WhatsApp actively bans whatsmeow-connected accounts (not just "protocol changes")

**Severity:** CRITICAL

**What goes wrong:**
WhatsApp's server-side detection flags accounts paired through whatsmeow (and the sibling
JS library Baileys) as using "unauthorized tools." The account receives an in-app warning
("Your account may be at risk … Using third-party tools to send bulk messages is a
violation … may result in your account being banned") and, in confirmed cases, is **force-logged-out**
with a `403` failure frame: *"You have been logged out for using an unofficial app.
Download the official WhatsApp and verify your account to log back in."* The paired
device session is deleted server-side and cannot be recovered — the operator must
re-pair from scratch on the official client first.

**Why it happens:**
WhatsApp detects the *client fingerprint*, not just the message volume. whatsmeow issue
#810 (opened May 2025, **still open** as of this research) reports bans hitting clients
that are **not bulk-messaging** — only replying to incoming messages manually or via AI.
The detection appears to target the library itself (both WhatsMeow and Baileys),
independent of safeguards like rate limiting. The PRD frames this risk as "unofficial
protocol changes" (a breakage/upgrade concern) — the reality is **active enforcement
that terminates accounts**, a far more severe failure mode.

**How to avoid:**
1. Treat unofficial WhatsApp Web as a **best-effort, disposable channel**, never the
   sole path for any message. The fallback pipeline (ordered `fallback_channels`) must
   always include an official channel (WABA) as a downstream option.
2. Honor the PRD's staggered dispatch (1–3s jittered) **strictly** — but understand
   this reduces, not eliminates, ban risk. It is not a guarantee.
3. Surface ban/logged-out events (whatsmeow `events.LoggedOut`) to the operator
   dashboard immediately and auto-disable the session so the system does not retry
   into a dead account.
4. Document the risk to operators prominently in the control panel at pairing time.
   Meta Verified on the business account has anecdotally reduced warnings (issue #810)
   — recommend it in the pairing UI.
5. Pin the whatsmeow dependency and monitor its issue tracker; detection changes land
   without warning and the library may or may not adapt.

**Warning signs:**
- whatsmeow emits `events.LoggedOut` / a `403` connect failure with
  `logout_message_header="You have been logged out for using an unofficial app."`
- A paired device that was healthy suddenly cannot reconnect after a `503` stream error.
- In-app "account may be at risk" warnings reported by the phone owner.
- `session_active` expvar drops without an operator-initiated disconnect.

**Phase to address:** **M2 (Queue & WhatsApp Web)** — the whatsmeow worker must handle
`LoggedOut` as a terminal session state, and the fallback engine (M3) must treat a
logged-out session as an immediate fallback trigger, not a retry.

**PRD coverage:** PARTIAL. PRD §8 lists "Unofficial Protocol Updates" as Critical/High
probability but mischaracterizes it as a library-breakage problem ("update adapter
libraries without redeploying"). It does NOT acknowledge account termination / ban risk.
The architecture's "channel layer is a plugin boundary" mitigates *protocol* breakage
but does nothing for *enforcement*. **Gap: ban-risk awareness and terminal-session
handling are unaddressed.**

---

### Pitfall 2: At-least-once delivery sends the same WhatsApp message to a human twice

**Severity:** CRITICAL

**What goes wrong:**
NATS JetStream `WorkQueuePolicy` guarantees **at-least-once**, not exactly-once. When a
worker crashes, restarts, or its `AckWait` expires after a successful `Dispatch` but
before `msg.Ack()` reaches the broker, JetStream **redelivers the message**. The
re-delivered worker calls `Dispatch` again → the human receives the WhatsApp message a
second time. WhatsApp message sends are **not naturally idempotent** — there is no
provider-side "send this message only once by idempotency key" for unofficial Web. A
duplicate outbound message to a customer is a visible, trust-eroding failure.

**Why it happens:**
The redelivery window is the gap between "provider accepted the message" and "broker
recorded the ack." This gap is non-trivial: the worker does `Dispatch` (1–3s staggered
wait + WebSocket send + provider round-trip), *then* `Ack`. A crash, OOM kill, or
`AckWait` timeout (the arch docs set `MaxDeliver: 5` with backoff) anywhere in that
window redelivers. Worse: the fallback pipeline tries multiple channels — a redelivery
may re-send on a *different* channel than the original succeeded on, producing a
duplicate on a channel the first attempt never used.

**How to avoid:**
1. **Deduplicate by `trace_id` before `Dispatch`.** Maintain a short-TTL "recently
   dispatched" set (in-memory `map[traceID]dispatchedChannel` with TTL, or a
   `dispatched_messages` table checked pre-send). If `trace_id` was already dispatched
   on this channel within the dedup window, `Ack` and skip — do not re-send.
2. **Ack only after the provider accepted the message** (already in the arch design),
   but make the ack-vs-redelivery race explicit: the dedup set is the safety net, not
   the ack timing.
3. **Do NOT layer provider-level retries on top of JetStream retries** (the arch docs
   already forbid this — good). Double-retry is the most common cause of duplicates.
4. For the fallback pipeline: record *which channel succeeded* in the dedup set so a
   redelivery knows the message already went out and should `Ack`-and-skip rather than
   re-attempt the next fallback channel.
5. Set `MaxDeliver` conservatively (5 is reasonable) and move exhausted messages to a
   DLQ with operator alerting rather than silently retrying forever.

**Warning signs:**
- Recipient reports receiving the same message twice.
- `jetstream_redeliver_total` expvar increments for messages that logged `StatusSent`.
- Audit log shows two `StatusSent` rows for the same `trace_id` on the same channel.

**Phase to address:** **M2 (worker + JetStream)** for the dedup set and ack semantics;
**M3 (fallback engine)** for per-channel-succeeded dedup across the fallback pipeline.

**PRD coverage:** WEAK. The architecture summary (01) explicitly names "at-least-once
vs. exactly-once" as a challenge and says side effects "must be idempotent at the
provider boundary, or deduplicated by `trace_id` before dispatch." But no concrete
dedup mechanism is specified in any doc — it is stated as a requirement without a
design. **Gap: the dedup set / `dispatched_messages` table is unimplemented and
unspecified. This is the single most likely production bug.**

---

### Pitfall 3: Goroutine leaks from forgotten senders and untracked session lifetimes

**Severity:** HIGH

**What goes wrong:**
The system spawns goroutines liberally: one per linked WhatsApp Web device (session
event loop), `2*NumCPU` JetStream workers, audit batch writers, webhook workers. Under
the 512MB / 2 vCPU ceiling, a goroutine leak compounds fast — each leaked goroutine
holds its stack (min 2KB, grows) and any captured references keep heap data alive
forever (goroutines are **not** garbage collected; they must exit on their own). At
scale (many sessions churning through connect/disconnect), leaked session goroutines
silently exhaust memory and the 90-day leak audit (PRD §9) fails.

The classic leak is the **"forgotten sender"**: a goroutine blocks on an unbuffered
channel send (`ch <- result`); the receiver has already moved on (context canceled,
timeout fired); the sender blocks forever. A second variant: a session goroutine whose
`context.CancelFunc` is never called because the registry lost the reference, so
`client.Connect()` outlives the session.

**Why it happens:**
- An unbuffered result channel where the receiver selects on `ctx.Done()` and returns
  before reading — the sender's `ch <- x` blocks with no reader (Ardan Labs pattern).
- `session.Registry.Remove(jid)` called **before** `wg.Wait()` completes, so a
  reconnect race re-`Put`s a session while the old goroutine is still draining.
- whatsmeow's internal goroutines (read pump, handler queue) are not joined on
  `Disconnect()` — the library's own goroutines can outlive the `Client` if
  `Disconnect` is not awaited.
- The audit buffer's `Close()` closes `b.ch` while a concurrent `Record()` is still
  running on the hot path → **panic on send to closed channel** (the arch docs' `Record`
  is non-blocking with a `select`/`default`, but `default` still attempts `b.ch <- e`
  which panics if `b.ch` is closed).

**How to avoid:**
1. **Never start a goroutine without knowing how it will stop.** Every session
   goroutine selects on `<-ctx.Done()` AND `<-s.done`; `Stop()` cancels ctx then
   waits on `s.done` (the arch docs 6.8 already do this — enforce it everywhere).
2. **Join on shutdown, don't fire-and-forget.** `manager.Stop(jid)` must `wg.Wait()`
  **before** removing from the registry, and the registry must hold a reference until
  the goroutine is confirmed dead.
3. **Make result channels buffered (cap 1)** when the receiver may abandon early —
   this is the documented fix for the forgotten-sender leak.
4. **Guard the audit buffer against close-while-record:** use a `sync.RWMutex` or a
   `closed atomic.Bool` checked in `Record()` before the send, OR drain `Record` via a
   dedicated goroutine that owns the channel. The current `close(b.ch)` in `Close()`
   races with in-flight `Record` from worker goroutines.
5. **Verify with `runtime.NumGoroutine()` and `pprof goroutine`** under load tests that
   cycle sessions (pair → disconnect → re-pair) repeatedly. Count must return to
   baseline after churn.

**Warning signs:**
- `runtime.NumGoroutine()` climbs monotonically through session churn and never returns
  to baseline.
- `pprof goroutine` dump shows dozens of goroutines blocked in
  `whatsmeow.(*Client).Connect` / `send` / `recv` for sessions no longer in the registry.
- RSS grows slowly over days with steady (not bursty) traffic — classic leak signature.
- Sporadic `panic: send on closed channel` from the audit path during deploy/shutdown.

**Phase to address:** **M2 (session manager + worker pool)** — the leak surface is
concentrated in the session lifecycle and the audit buffer, both M2. Add the goroutine
count assertion to M3 load testing (the 90-day leak audit is too late to catch a leak).

**PRD coverage:** GOOD. Architecture doc 04 has a dedicated "Preventing races & leaks"
table that nails the registry race, the worker shutdown, the audit-full non-blocking
pattern, and pprof on a separate listener. The two gaps: (a) the audit `Close()` race
with in-flight `Record` is not addressed, and (b) whatsmeow's *internal* goroutine
cleanup on `Disconnect` is assumed but not verified. **Minor gap; design is mostly
sound but needs a load-test assertion to prove it.**

---

### Pitfall 4: Audit write contention breaches the 50ms p99 ingestion budget

**Severity:** HIGH

**What goes wrong:**
The `audit_logs` table receives an event for **every state transition** of every
message (Queued, Sent, Delivered, Read, Failed). At 500 req/s with ~3–5 transitions per
message, that's 1,500–2,500 audit rows/s. If writes are synchronous or batch
collisions are frequent, the audit path stalls the worker goroutine, which stalls
JetStream ack, which backs up the queue, which breaches the 50ms ingestion SLO. A
secondary failure: partitioning by `workspace_id` (per the schema design) creates **hot
partitions** — one busy workspace's inserts serialize on a single partition's pages,
amplifying contention.

**Why it happens:**
- Synchronous `INSERT` per event on the hot path (the PRD §6 code shows a buffered
  channel, but if the buffer fills, the non-blocking `select/default` **drops** events —
  a compliance SLO violation, not a latency fix).
- `pgx.CopyFrom` into a partitioned table routes to the correct child partition per
  row, but all rows for one workspace hit the same partition → lock contention on that
  partition's buffer pages under burst.
- Missing or mis-sized fillfactor / autovacuum on append-only partitions → bloat and
  slower inserts over time.
- A unique constraint on `(trace_id, event)` for deduplication forces an index lookup
  per insert — but on a partitioned table the unique constraint **must include the
  partition key** (`workspace_id`), so it cannot enforce global trace_id uniqueness
  across partitions anyway (PostgreSQL limitation).

**How to avoid:**
1. **Keep the audit path fully asynchronous and off the ingestion goroutine** (arch
   docs 04 already do this: `Record` is non-blocking, batch writers use `pgx.CopyFrom`).
   Verify the 5000-cap buffer never fills at 2,500 events/s × 50ms flush = 125 events
   in flight per writer — well under capacity. The buffer is sized correctly.
2. **Partition by `created_at` day (range), not by `workspace_id` (list), if workspace
   cardinality is low or skewed.** The arch docs hedge ("partitioned by `workspace_id`
   (or by `created_at` day …)") — pick `created_at` range partitioning for the audit
   table: it distributes inserts across time partitions (no single hot partition), and
   `DROP PARTITION` gives instant retention pruning. Use `workspace_id` only as a
   *sub*-partition if a single day partition gets too large.
3. **Do not put a unique dedup constraint on the audit table.** Dedup of audit events
   is not required (audit is append-only history; a duplicate event row is harmless and
   the dedup happens upstream at dispatch, per Pitfall 2). A unique constraint would
   force per-insert index work and is constrained by the partition-key-inclusion rule
   anyway.
4. **Tune the partitions for append-only workload:** `fillfactor=100` (no UPDATEs, no
   HOT updates needed), aggressive `autovacuum` thresholds, and `BRIN` index on
   `created_at` instead of B-tree for the common "recent events" query.
5. **Alert on `audit_dropped` expvar** — a non-zero value means the buffer is
   undersized or writers are too slow; tune `writers` and `batchN` before it becomes a
   compliance breach.

**Warning signs:**
- `audit_dropped` expvar > 0 (events lost → compliance SLO at risk).
- Worker `Fetch` latency rises; JetStream `MaxAckPending` saturates; queue depth grows
  while DB `pg_stat_activity` shows long `INSERT`/`COPY` on the audit partition.
- p99 ingestion latency > 50ms with a DB-bound profile (check via pprof).
- One partition's `pg_stat_user_tables.n_tup_ins` dominates all others (hot partition).

**Phase to address:** **M1 (Core Foundation — schema + audit batch writer)** for the
buffered writer and partitioning strategy; **M3 (load testing)** to prove 2,500
events/s doesn't fill the buffer or stall workers.

**PRD coverage:** GOOD. The PRD §6 prescribes the buffered channel + batch writer, and
arch docs 04/06 give concrete sizing (cap 5000, 2 writers, batch 500/50ms, `pgx.CopyFrom`).
The gaps: (a) partitioning strategy is left as an unresolved either/or
(`workspace_id` vs `created_at`) — this research recommends `created_at` range; (b) no
guidance on fillfactor/BRIN/autovacuum for append-only partitions; (c) the unique
constraint limitation is not flagged. **Mostly addressed; partition strategy needs a
decision before M1 schema creation.**

---

### Pitfall 5: Multi-tenant data isolation leak (workspace_id is a convention, not a constraint)

**Severity:** HIGH

**What goes wrong:**
PerGo serves multiple workspaces from one binary and one PostgreSQL database. If a
query anywhere in the codebase omits the `workspace_id` filter — a missing `WHERE`, a
join that drops the tenant predicate, an admin-list query that fetches all rows — one
tenant can read or act on another tenant's audit logs, device sessions, or API keys.
This is the classic multi-tenant SaaS data leak: not a hack, but an application bug
that the database does not prevent because **there is no row-level enforcement**. The
schema partitions `audit_logs` by `workspace_id` but partitioning only *routes* rows;
it does not *restrict* a `SELECT * FROM audit_logs` from scanning all partitions.

**Why it happens:**
- Every query must carry `WHERE workspace_id = $1`. A single forgotten filter in one
  handler, one audit-review endpoint, or one admin dashboard query leaks cross-tenant
  data. With hand-written SQL (no ORM, per the tech decisions) there is no automatic
  tenant scoping — every author must remember.
- The API-key auth middleware resolves the workspace, but if a downstream query uses a
  `JID` or `trace_id` to look up a row without re-scoping by workspace, a tenant who
  knows/guesses another tenant's JID or trace_id can access it (IDOR).
- PostgreSQL Row-Level Security (RLS) is the database-level backstop, but the arch docs
  do not specify it — they rely entirely on application-layer filtering. RLS has its
  own footguns: the table owner and `BYPASSRLS` roles bypass policies, and referential
  integrity checks (unique/PK/FK) bypass RLS, creating covert channels.

**How to avoid:**
1. **Adopt a "tenant context is always in `context.Context`" rule.** Every
   `internal/platform/postgres` query function takes `ctx` and extracts `workspace_id`
   from it; the query is structurally unable to run without it. A linter or a thin
   `tenantQuery` wrapper that injects the `WHERE workspace_id` clause makes omission a
   compile/build-time error, not a code-review hope.
2. **Add PostgreSQL RLS as defense-in-depth** for `audit_logs`, `devices`, and
   `api_keys`: `ENABLE ROW LEVEL SECURITY` + `CREATE POLICY ... USING (workspace_id =
   current_setting('app.workspace_id'))`. Set `app.workspace_id` per transaction
   (`SET LOCAL`) from the resolved tenant. This catches any query that slips past the
   app layer.
3. **Connect to PostgreSQL as a non-superuser, non-owner role** so RLS actually
   applies (superusers and owners bypass it). Use `FORCE ROW LEVEL SECURITY` if the
   app role must be the owner.
4. **Never trust a client-supplied `workspace_id`.** It must always come from the
   authenticated API key, never from the request body. The ingest handler must
   overwrite `msg.WorkspaceID` with the auth-resolved value.
5. **Test isolation explicitly:** a multi-tenant integration test that enqueues as
   tenant A and attempts to read tenant B's audit logs / device sessions must fail.

**Warning signs:**
- An audit-review or admin query returns rows from the wrong workspace.
- A penetration test finds an IDOR via `trace_id` or `jid` without workspace scoping.
- `EXPLAIN` on an audit query shows all partitions scanned (no partition pruning →
  missing `workspace_id` filter).

**Phase to address:** **M1 (Core Foundation — schema + auth)** for the tenant-context
convention and the auth-resolved workspace_id overwrite; RLS policies can be added in
M1 or hardened in M3. **Do not defer isolation to "we'll add RLS later" — the
application-layer convention must be correct from the first query.**

**PRD coverage:** WEAK. The PRD names "data isolation" and "multi-tenant" but the
architecture docs specify no enforcement mechanism — no RLS, no tenant-context
wrapper, no mandatory `WHERE workspace_id` convention. The schema partitions by
`workspace_id` (which helps pruning, not isolation). The API-key auth resolves
workspace but nothing forces downstream queries to use it. **Gap: this is a systemic
design omission. Without an enforced convention, a cross-tenant leak is a matter of
when, not if, as the codebase grows.**

---

### Pitfall 6: AES-256-GCM credential encryption with unmanaged / reused nonces or a co-located key

**Severity:** HIGH

**What goes wrong:**
Two distinct failure modes, both severe:

**(a) Nonce reuse.** AES-GCM requires a nonce that is **unique for all time per key**.
Reusing a (key, nonce) pair is catastrophic — it breaks *both* confidentiality and
authenticity (an attacker can recover the plaintext and forge valid ciphertexts). If
the code generates nonces with a counter that resets on restart, or with a
deterministic derivation that collides across credentials, or simply reuses a fixed
nonce "just for testing," a single reuse compromises every credential encrypted under
that key.

**(b) Key co-location.** The PRD requires AES-256-GCM at rest for session tokens and
channel credentials, but the AES key must live *somewhere*. If it is an environment
variable or a file on the same host as the PostgreSQL data directory, a database
compromise or host compromise yields both the ciphertext and the key — encryption at
rest provides no protection against the most likely threat (DB exfiltration). The arch
summary notes "key management is a deployment concern, not an application one" — but
this is the pitfall: it is trivially easy to ship v1 with `ENCRYPTION_KEY=...` in the
env and consider the requirement met.

**Why it happens:**
- Nonce: the Go stdlib `cipher.NewGCM` uses a 12-byte nonce; developers sometimes use a
  fixed nonce, a timestamp (collides under burst), or `crypto/rand` without tracking
  uniqueness (safe up to 2^32 messages per key, but unbounded reuse is the risk).
  Go 1.24+ `NewGCMWithRandomNonce` auto-generates the nonce but still caps at 2^32
  messages per key.
- Key: a self-hosted, single-binary deployment has no KMS by default. The path of
  least resistance is an env var, which is exactly the path that defeats the purpose.

**How to avoid:**
1. **Generate a fresh random 12-byte nonce per `Seal` call** via `crypto/rand` and
   **prepend it to the ciphertext** (standard pattern: `nonce || ciphertext || tag`).
   Never derive the nonce deterministically from the credential. Track that the key is
   not used for more than 2^32 encryptions (vastly more than PerGo will ever hit, but
   document the ceiling).
2. **Use envelope encryption.** A master Key Encryption Key (KEK) wraps per-credential
   Data Encryption Keys (DEKs); the DEK is stored encrypted at rest alongside the
   ciphertext, and the KEK lives in a separate trust boundary (env/file/KMS at deploy
   time). A DB breach exposes encrypted DEKs, not the KEK.
3. **Support key rotation from day one** (even if unused): tag each encrypted blob with
   a `key_id` so a future rotation can re-encrypt with a new KEK without a flag day.
   The `devices.encrypted_session` and channel credential columns should carry a
   `key_id`/`key_version` alongside the ciphertext.
4. **Never log or `slog`-emit the plaintext key, nonces, or decrypted credentials.**
   Redact in structured logging.
5. **Document the deployment key-custody requirement** in a runbook: the KEK must not
   live on the application host's disk in a world-readable file; for the MVP a
   secret-injected env var read once at startup is acceptable *if* the threat model is
   "DB exfiltration," not "host compromise." Be explicit about which threat is covered.

**Warning signs:**
- Code review finds a fixed/hardcoded nonce, or `nonce := make([]byte, 12)` with no
  `rand.Read`.
- No `key_id`/`key_version` column next to encrypted fields → rotation is impossible
  without a schema migration + re-encrypt.
- The KEK is checked into the repo, the docker image, or a world-readable file.
- Decryption failures spike after a deploy (key changed without rotation path).

**Phase to address:** **M1 (Core Foundation — schema + crypto)** for the nonce-per-seal
pattern, envelope encryption, and `key_id` columns. Key rotation tooling can defer to
post-MVP but the *schema* must support it from M1 (adding `key_id` later is a
migration).

**PRD coverage:** PARTIAL. PRD §6 names "AES-256-GCM encryption at rest" and the tech
decisions confirm `crypto/aes` + `crypto/cipher` (GCM) — the *algorithm* is right. But
no doc addresses nonce management, key custody, envelope encryption, or rotation. The
arch summary explicitly punts key management to "deployment." **Gap: the application
must still generate nonces correctly and carry `key_id` from M1; the deployment
runbook must cover KEK custody. Both are unspecified.**

---

## Technical Debt Patterns

Shortcuts that seem reasonable but create long-term problems at PerGo's scale.

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| App-layer-only tenant filtering (no RLS) | No RLS policy setup/maintenance; simpler queries | One forgotten `WHERE workspace_id` = cross-tenant leak; grows with codebase | M1 only IF a `tenantQuery` wrapper + linter enforces it; add RLS by M3 |
| `workspace_id` list partitioning for `audit_logs` | Per-tenant pruning for admin queries | Hot partition for busy tenant; can't enforce global unique; partition explosion at high tenant cardinality | Never for audit — use `created_at` range partitioning (see Pitfall 4) |
| Env-var AES key, no envelope encryption | Zero infra; ship in v1 | DB breach + key co-located = full credential compromise; no rotation path | M1 acceptable IF threat model is DB-exfil only AND `key_id` column exists for later upgrade; never acceptable for host-compromise threat |
| No `dispatched_messages` dedup table | One fewer table + lookup on hot dispatch path | Duplicate messages to humans on every JetStream redelivery (Pitfall 2) | Never — at-least-once without dedup is a production bug |
| In-memory `atomic.Int64` backpressure counter only (no JetStream reconciliation) | Cheaper than `StreamInfo` per publish | Drift between counter and real queue depth → 429 fires too late or too early | Acceptable as default (arch docs 05 already say this) IF a 10s reconciler corrects drift |
| Treating LoggedOut as a retryable disconnect | Simpler session state machine | Retrying a banned account deepens the ban; wastes worker time | Never — `LoggedOut` is terminal, disable the session |
| `context.Background()` in audit batch writer | Writer outlives request ctx (correct intent) | Cancels don't cascade; but a writer that uses `context.Background()` with a 2s timeout is fine IF it has its own timeout | Acceptable — the arch code 6.7 already uses `context.WithTimeout(context.Background(), 2s)`; do NOT extend to other paths |

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| whatsmeow `events.LoggedOut` | Treating it like a transient disconnect and auto-reconnecting | It is terminal — the server deleted the session. Emit an operator alert, mark the device `disabled`, and stop reconnecting. Re-pairing requires the official phone app. |
| whatsmeow `store/sqlstore` Container in the shared PostgreSQL DB | Letting whatsmeow manage its own schema/tables without co-location planning | whatsmeow's `sqlstore` creates its own tables (`whatsmeow_device`, prekeys, etc.) in the target DB. Co-locate in PerGo's PostgreSQL but document that these tables are library-owned — do not hand-write migrations against them. Use a separate schema or clear naming to avoid collision with PerGo's `devices` table. |
| NATS JetStream `AckWait` vs. dispatch duration | Setting `AckWait` shorter than the 1–3s staggered dispatch + provider round-trip → spurious redelivery | `AckWait` must exceed worst-case dispatch time (stagger + send + ack round-trip). With 3s stagger + 5s send, `AckWait` < 8s causes redelivery storms. Use `Backoff` and `Nak` with explicit delay instead of relying on `AckWait` alone. |
| NATS `Nats-Msg-Id` dedup window | Relying on it for consumer-side idempotency | `Nats-Msg-Id` is **publish-side** dedup (prevents the same publish being stored twice). It does NOT prevent a consumer from processing a redelivered message twice. Consumer-side dedup (Pitfall 2) is separate. |
| pgx `CopyFrom` into partitioned table | Assuming `CopyFrom` routes rows automatically | It does route by partition key, but all rows for one workspace in a batch hit one partition. Batch across workspaces/time, or accept the hot-partition cost. |
| Telegram / WABA REST clients | One shared `http.Client` with no per-client timeout | Use a dedicated `http.Client{Timeout: 10s}` per provider (arch docs 05 already specify this). A shared default client with no timeout hangs the worker forever on a stalled provider. |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Unbuffered result channels in concurrent dispatch | Goroutine count climbs; pprof shows send-blocked goroutines | Buffer (cap 1) or select-on-done (Pitfall 3) | Any time a receiver cancels before the sender completes — common under backpressure |
| `sync.RWMutex` held across a network call in `session.Registry` | All session lookups stall while one `Put` does I/O | Never hold the lock across `whatsmeow.Connect`/network; arch docs 04 already forbid this | High session churn under load |
| Per-request `errgroup` fan-out for fallback | Sends the message N times in parallel (N duplicate sends) | Sequential fallback (arch docs 04 already forbid parallel) | Any fallback config with >1 channel — immediate, visible |
| Audit buffer cap too small for burst | `audit_dropped` increments under burst; compliance SLO breach | Size above `peak_events/s × flush_interval`; arch docs size 5000 for 500 req/s (correct) | Burst > 500 msg/s sustained for >10s |
| `pgxpool.MaxConns` too large | Latency rises (connection contention), memory grows | `2*CPU + queue` (arch docs 04); small pool forces reuse | >10 workers or a separate admin pool stealing connections |
| Sync DB write on the ingest hot path | p99 > 50ms; auth or audit INSERT dominates | Auth from in-memory cache; audit via async buffer (arch docs 01 mandate 2 I/O max) | Any synchronous INSERT on `POST /messages` |
| Partition pruning disabled (`enable_partition_pruning=off`) | Audit queries scan all partitions | Verify the GUC is on (default); check `EXPLAIN` shows pruning | Any misconfigured `postgresql.conf` |

## Security Mistakes

Domain-specific security issues beyond general web security.

| Mistake | Risk | Prevention |
|---------|------|------------|
| Storing the AES key on the same host/volume as the PostgreSQL data | DB exfiltration = full credential decryption; encryption at rest is theater | Envelope encryption with KEK in a separate trust boundary; document threat model (Pitfall 6) |
| Reusing an AES-GCM nonce (fixed, counter-reset, or deterministic derivation) | Catastrophic: breaks confidentiality AND authenticity for all data under that key | Fresh `crypto/rand` 12-byte nonce per `Seal`; prepend to ciphertext (Pitfall 6) |
| Trusting `workspace_id` from the request body | Tenant impersonation: client sets another tenant's workspace | Always overwrite with the auth-resolved workspace from the API key (Pitfall 5) |
| No RLS / no enforced tenant filter convention | Cross-tenant data leak via a forgotten `WHERE` | `tenantQuery` wrapper + RLS defense-in-depth (Pitfall 5) |
| API-key hashing with plain SHA-256 (no salt, no slow hash) | Offline brute-force if the `api_keys` table leaks | SHA-256 with a prefix lookup is the PRD design — acceptable IF the key has high entropy (e.g. 32+ random bytes). Do NOT allow user-chosen low-entropy keys. Consider bcrypt/argon2 if keys are ever human-chosen. |
| pprof exposed on the public Echo mux | Source code + heap dump (may contain credentials in memory) leak to the internet | Mount pprof on `localhost:6060` only (arch docs 04 already specify this — enforce it) |
| whatsmeow session secrets in logs | Credential leak via structured logs | Never `slog` the `encrypted_session` blob or decrypted token; redact device.JID in debug logs if tenant-sensitive |
| Webhook outbound POST includes full decrypted message body to an unverified URL | If a tenant registers a malicious webhook URL, they receive other tenants' messages if isolation is broken | Sign webhooks (HMAC); scope webhook config per-workspace; never send cross-tenant data |

## UX Pitfalls

Common operator/developer experience mistakes in this domain.

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| QR code pairing flow with no ban-risk warning | Operator pairs a business-critical WhatsApp number, gets banned, loses the number | Show a prominent warning at pairing: "Unofficial WhatsApp Web carries account-ban risk. Prefer WABA for production. Consider Meta Verified." (Pitfall 1) |
| `202 Accepted` with no delivery visibility | Developer cannot tell if the message was delivered or silently failed | Trace-ID in the `202` body + webhook delivery events + audit-log lookup by `trace_id` (PRD already plans this — ensure the webhook actually fires for failed/delivered) |
| No operator alert on `LoggedOut` event | Banned/logged-out session looks "connected" in the dashboard; messages queue silently | Real-time dashboard alert + auto-disable the session + email/Slack if configured (Pitfall 1) |
| Fallback silently sends via a different channel than the developer expected | Developer's branding/template is wrong on the fallback channel (e.g. WhatsApp template → Telegram plain text) | Webhook event must state *which channel* ultimately delivered; audit log records the winning channel per `trace_id` |
| 429 with no `Retry-After` or queue-depth hint | Developer retries immediately, deepening the queue | Always include `Retry-After: 5` (arch docs 05 already do) and consider a `X-Queue-Depth` hint |
| Audit log review shows events with no trace correlation | Operator cannot reconstruct an incident | 100% trace-correlated logging is a PRD SLO — verify via a test that every audit row has a non-null `trace_id` |

## "Looks Done But Isn't" Checklist

Things that appear complete in a demo but are missing critical production pieces.

- [ ] **WhatsApp Web worker:** Often missing `LoggedOut` terminal handling — verify the session is marked `disabled` and the operator is alerted, not auto-reconnected.
- [ ] **At-least-once delivery:** Often missing the `dispatched_messages` dedup set — verify a forced redelivery (kill the worker after `Dispatch`, before `Ack`) does NOT produce a duplicate send.
- [ ] **Multi-tenant isolation:** Often missing the enforced `WHERE workspace_id` — verify a tenant-A request cannot read tenant-B's audit logs or device sessions (integration test).
- [ ] **Credential encryption:** Often missing `key_id`/rotation support — verify the `devices` and credential columns carry a `key_version` and that a rotation path exists (even if unrun).
- [ ] **Trace-ID propagation:** Often lost at one boundary — verify `trace_id` appears in: HTTP response header → NATS message header → worker `ctx` → audit row → webhook payload. Test each hop.
- [ ] **Backpressure:** Often checked *after* enqueue — verify the 1000-limit returns `429` *before* the JetStream publish, not after.
- [ ] **Graceful shutdown:** Often drops in-flight work — verify SIGTERM drains workers (Ack or Nak in-flight), flushes the audit buffer, and cancels session goroutines within 30s.
- [ ] **Audit buffer `Close()`:** Often panics on concurrent `Record` — verify shutdown closes the channel only after the HTTP server stops accepting and workers stop calling `Record`.
- [ ] **Circuit breaker:** Often wraps the wrong thing — verify the breaker is per-provider-REST (WABA/Telegram), NOT around whatsmeow (arch docs 05 already specify; verify in code).
- [ ] **Fallback pipeline:** Often runs in parallel "for speed" — verify it is strictly sequential (parallel = N duplicate sends).

## Recovery Strategies

When pitfalls occur despite prevention, how to recover.

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| WhatsApp account banned/logged out (Pitfall 1) | HIGH (account may be unrecoverable) | 1. Disable the session in PerGo. 2. Operator opens official WhatsApp on the phone, re-verifies. 3. Re-pair via QR in PerGo. 4. If account is permanently banned, escalate to Meta / use WABA fallback. Messages queued for that session drain via fallback or DLQ. |
| Duplicate message sent to a human (Pitfall 2) | MEDIUM (trust/reputation) | 1. Add the `dispatched_messages` dedup set retroactively. 2. Audit-log query for duplicate `trace_id`+channel pairs to assess blast radius. 3. Notify affected tenants. 4. Tune `AckWait`/`MaxDeliver` to reduce redelivery frequency. Cannot "unsend" a delivered WhatsApp message. |
| Goroutine leak in production (Pitfall 3) | MEDIUM (restart + fix) | 1. Restart the process (immediate relief). 2. Capture `pprof goroutine` before restart to localize the leak. 3. Fix the forgotten-sender or unjoined goroutine. 4. Add a goroutine-count regression test. |
| Audit write contention / dropped events (Pitfall 4) | MEDIUM (compliance gap) | 1. Increase audit buffer cap / writer count. 2. Switch to `created_at` range partitioning if hot. 3. Backfill any dropped events from JetStream (the message stream is the source of truth; audit is derived). 4. Tune fillfactor/autovacuum. |
| Cross-tenant data leak (Pitfall 5) | HIGH (security incident) | 1. Treat as a security incident: audit which queries lacked the filter. 2. Add the missing filters + RLS policies. 3. Notify affected tenants per GDPR/LGPD breach rules. 4. Add isolation integration tests. Recovery is reputational/legal, not technical. |
| Nonce reuse / key compromise (Pitfall 6) | HIGH (re-encrypt all) | 1. Rotate the KEK immediately. 2. Re-encrypt all credentials with a new DEK + fresh nonces. 3. Assume all credentials under the compromised key are leaked → rotate channel tokens / re-pair devices. 4. Add nonce-uniqueness tests. |

## Pitfall-to-Phase Mapping

How the three-milestone roadmap should address these pitfalls.

| Pitfall | Severity | Prevention Phase | Verification |
|---------|----------|------------------|--------------|
| WhatsApp account ban / LoggedOut (P1) | CRITICAL | M2 (worker terminal-session handling) + M3 (fallback treats logged-out as trigger) | Unit test: `events.LoggedOut` → session `disabled`, no reconnect loop; dashboard alert fires. |
| Duplicate message on redelivery (P2) | CRITICAL | M2 (dedup set + ack semantics) + M3 (per-channel-succeeded dedup in fallback) | Integration test: kill worker after `Dispatch`, before `Ack` → redelivered message is Ack-and-skip, not re-sent. |
| Goroutine leak (P3) | HIGH | M2 (session lifecycle + audit Close race) | Load test: 1000 pair/disconnect cycles → `runtime.NumGoroutine()` returns to baseline. |
| Audit write contention (P4) | HIGH | M1 (partitioning decision + batch writer) + M3 (load test at 2500 events/s) | Load test: `audit_dropped == 0` at 500 req/s sustained; p99 < 50ms. |
| Multi-tenant isolation (P5) | HIGH | M1 (tenant-context convention + auth-resolved workspace) + M3 (RLS hardening) | Integration test: tenant A cannot read tenant B data via any endpoint or direct JID/trace_id. |
| Credential encryption / nonce / key custody (P6) | HIGH | M1 (nonce-per-seal + `key_id` columns + envelope pattern) | Code review: no fixed nonce; `key_id` column present; KEK not on DB volume; decrypt round-trip test. |

**Phase ordering rationale:** M1 (Core Foundation) must land the schema decisions that
are expensive to change later — partitioning strategy, `key_id` columns, tenant-context
convention. M2 (Queue & WhatsApp Web) must land the durability-correctness machinery —
dedup set, terminal-session handling, goroutine-lifecycle discipline. M3 (Official
Channels + Load Testing) is where correctness is *proven* under load and where the
fallback pipeline and RLS hardening complete the defense-in-depth. Deferring any M1/M2
pitfall to "we'll fix it in M3" is a false economy — the schema and concurrency
structure are set in M1/M2 and retrofits are painful.

## Sources

- whatsmeow GitHub repository (README, features) — https://github.com/tulir/whatsmeow — **official library source** (HIGH)
- whatsmeow issue #810: "Your account may be at risk" warning (OPEN, May 2025) — https://github.com/tulir/whatsmeow/issues/810 — **primary ban-risk evidence** (HIGH)
- whatsmeow issue #561: "You have been logged out for using an unofficial app" (403 failure, session deleted) — https://github.com/tulir/whatsmeow/issues/561 — **primary enforcement evidence** (HIGH)
- NATS JetStream Consumers docs (AckPolicy, MaxDeliver, AckWait, Backoff, pull consumers) — https://docs.nats.io/nats-concepts/jetstream/consumers — **official docs** (HIGH)
- NATS JetStream Model Deep Dive (WorkQueuePolicy, Message Deduplication via Nats-Msg-Id, exactly-once via double-ack, ack types) — https://docs.nats.io/using-nats/developer/develop_jetstream/model_deep_dive — **official docs** (HIGH)
- Go blog: "Go Concurrency Patterns: Pipelines and cancellation" (goroutine leaks, done-channel, forgotten sender) — https://go.dev/blog/pipelines — **official Go source** (HIGH)
- Ardan Labs: "Goroutine Leaks - The Forgotten Sender" (unbuffered channel send blocks after receiver cancels; buffered cap-1 fix) — https://www.ardanlabs.com/blog/2018/11/goroutine-leaks-the-forgotten-sender.html — **recognized Go authority** (MEDIUM-HIGH)
- PostgreSQL docs §5.12: Table Partitioning (declarative partitioning, limitations, best practices, partition pruning, unique-key-must-include-partition-key) — https://www.postgresql.org/docs/current/ddl-partitioning.html — **official docs** (HIGH)
- PostgreSQL docs §5.9: Row Security Policies (RLS, USING/WITH CHECK, BYPASSRLS, covert channels via referential integrity, race conditions) — https://www.postgresql.org/docs/current/ddl-rowsecurity.html — **official docs** (HIGH)
- Go `crypto/cipher` package docs (AEAD, NewGCM, nonce uniqueness requirement, 2^32 random-nonce ceiling, NewGCMWithRandomNonce Go 1.24+) — https://pkg.go.dev/crypto/cipher — **official stdlib docs** (HIGH)
- PerGo project docs (cross-referenced for PRD coverage assessment): `.planning/PROJECT.md`, `docs/PRD PerGo.md` §8, `docs/architecture/01-06` — **internal, authoritative for the project** (HIGH for coverage assessment)

---
*Pitfalls research for: self-hosted omnichannel CPaaS (PerGo) — unofficial WhatsApp Web, NATS JetStream, PostgreSQL, Go*
*Researched: 2026-06-25*
