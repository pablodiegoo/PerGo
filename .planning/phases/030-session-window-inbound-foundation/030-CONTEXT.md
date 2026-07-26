# Phase 30: Session Window & Inbound Foundation - Context

**Gathered:** 2026-07-26
**Status:** Ready for planning

<domain>
## Phase Boundary

Extend PerGo's existing `recipient_sessions` table and `WindowChecker` to enforce WABA's 24-hour customer service window at both API ingestion and worker dispatch time, track 72h free entry point windows from Click-to-WhatsApp ads, and emit session lifecycle webhook events. WABA-only concern — does not affect WhatsApp Web (whatsmeow) or Telegram channels.

</domain>

<decisions>
## Implementation Decisions

### Dispatch-time Enforcement
- **D-01:** Window check runs at BOTH ingestion (HTTP handler) and dispatch (WABA worker). Dual enforcement: ingestion catches obvious expired sessions immediately with HTTP 422; dispatch re-checks with safety buffer to catch queue-latency boundary violations.
- **D-02:** 5-minute safety buffer at dispatch only. Ingestion uses the full 24h (simple); worker uses 23h55m to account for NTP drift + queue latency.
- **D-03:** Extend existing `WindowChecker.IsWindowOpen()` with an optional `safetyBuffer time.Duration` parameter — zero at ingestion, 5min at dispatch. Single code path, two callers.
- **D-04:** `IsWindowOpen` returns a `WindowStatus` struct with `Open bool`, `ExpiresAt time.Time`, `EntryPointType string` — callers get rich context for error messages and smart fallback (used by Phase 32 DISP-04).
- **D-05:** Session window check applies to WABA (`whatsapp_cloud`) channel only. WhatsApp Web and Telegram do not have this restriction.
- **D-06:** Ingestion-time check wired into the `POST /messages` API handler — if `channel == whatsapp_cloud && type != template`, call `WindowChecker`. Clean separation from dispatch.
- **D-07:** When dispatch-time re-check finds the window expired (queue-latency edge case): fail fast with terminal error, NAK the message from JetStream permanently (no retry). Emit a `message.window_expired` webhook event so the caller knows.
- **D-08:** Session window check is WABA-specific — only wired into the WABA dispatch path. Not part of the core `Dispatcher` interface. Phase 32 handles smart template fallback.

### Session Expiration Events
- **D-09:** `session.expiring_soon` triggered by a background ticker goroutine that runs every 5 minutes. Queries `recipient_sessions WHERE last_inbound_at BETWEEN now()-23h AND now()-23h+interval` (where interval = ticker period). Simple, bounded, predictable.
- **D-10:** Events emitted through the existing webhook subscription system — same `event_type: session.expiring_soon` pattern used by `message.sent`, `message.received`, etc. No new NATS subject needed.
- **D-11:** Dedup via a `notified_expiring_at TIMESTAMPTZ` column on `recipient_sessions`. Set on first notification, skip on subsequent ticks until session is refreshed. Reset to NULL on new inbound (in the existing `Upsert` path).

### 72h Entry Point Tracking
- **D-12:** Add `entry_point_type VARCHAR(20) DEFAULT 'standard'` column to `recipient_sessions`. Values: `standard` (24h window) or `ctwa` (72h window). Set from Meta webhook referral data when a conversation starts from a Click-to-WhatsApp ad.
- **D-13:** `WindowChecker` uses `entry_point_type` to pick 24h vs 72h window duration. Simple conditional in `IsWindowOpen`.
- **D-14:** Reset `entry_point_type` to `standard` on any non-ad inbound message. The next normal user message starts a fresh 24h window. Simple and safe.

### Error Response Design
- **D-15:** Rich structured error matching PerGo's existing pattern: `{"code": "SESSION_WINDOW_EXPIRED", "message": "...", "details": {"window_expired_at": "...", "window_duration": "24h", "hint": "Use type: template to reach this contact"}}`. HTTP 422 status.
- **D-16:** Error code is `SESSION_WINDOW_EXPIRED` — clear, searchable, distinct from Meta's own error codes.
- **D-17:** Dispatch-time failure uses the SAME `SESSION_WINDOW_EXPIRED` code but with `source: "dispatch"` in details (vs `source: "ingestion"` at API level). Consistent for API consumers, distinct for debugging.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Session Infrastructure (existing code)
- `internal/repository/recipient_session.go` — RecipientSessionRepository with Upsert(), Get(), UpdateLastReadAt(). Already wired into inbound/processor.go.
- `internal/session/window.go` — WindowChecker with IsWindowOpen(). Basic 24h check, returns (bool, error). Must be extended per D-03 and D-04.
- `internal/session/window_test.go` — Existing WindowChecker tests. Must be updated for new signature.
- `internal/inbound/processor.go` — Lines 247-248: already calls `recipientSessionRepo.Upsert()` on every inbound event. Session timestamp tracking is already wired.

### WABA Webhook Handler
- `internal/api/handler/waba_webhook.go` — HandlePost parses inbound WABA webhooks. Must be extended to detect referral/CTWA entry points and pass entry_point_type to session upsert.

### Research (v1.7 milestone)
- `.planning/research/PITFALLS.md` §Session Window Pitfalls — Clock drift, queue latency race, timezone confusion, 72h CTWA rules.
- `.planning/research/FEATURES.md` §Session Window Enforcement — 24h mechanics, trigger/reset rules, 72h free entry points.
- `.planning/research/ARCHITECTURE.md` §Data Flow Changes — Outbound dispatch flow with session window check.

### Spike Findings
- `.agents/skills/spike-findings-pergo/references/conversational-session.md` — Validated schema for `recipient_sessions` table, Upsert pattern, constraints.

### Requirements
- `.planning/REQUIREMENTS.md` — SESS-01 through SESS-05 requirements for this phase.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `RecipientSessionRepository` (internal/repository/recipient_session.go): Fully operational with Upsert, Get, UpdateLastReadAt, UpdateLastReadAtByContact. Needs `entry_point_type` and `notified_expiring_at` column additions.
- `WindowChecker` (internal/session/window.go): Basic 24h check. Must be extended with `safetyBuffer` parameter and `WindowStatus` return type.
- `InboundProcessor` (internal/inbound/processor.go): Already calls `recipientSessionRepo.Upsert()` on every inbound event (line 247-248). Session tracking is already wired — no new inbound wiring needed beyond passing `entry_point_type`.
- Webhook subscription engine: Existing event emission pattern via `event_type` filters. `session.expiring_soon` follows this pattern.

### Established Patterns
- Error responses: `{"code": "...", "message": "...", "details": {...}}` pattern used across all API handlers.
- Database migrations: goose SQL migrations in `migrations/` directory.
- Channel-specific logic: WABA-specific code lives in `internal/channel/whatsapp/` or is gated by `channel == "whatsapp_cloud"` checks in shared handlers.

### Integration Points
- `POST /messages` handler: Add session window pre-flight check before queue enqueue for `whatsapp_cloud` channel.
- WABA dispatch worker: Add dispatch-time re-validation with 5-minute safety buffer before Meta Graph API call.
- WABA inbound adapter: Detect `referral` object in webhook payload to set `entry_point_type = "ctwa"`.
- Background session ticker: New goroutine started alongside existing server lifecycle (registered in `main.go` or app startup).

</code_context>

<specifics>
## Specific Ideas

No specific requirements — open to standard approaches. The decisions above are clear enough for implementation.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>

---

*Phase: 30-Session Window & Inbound Foundation*
*Context gathered: 2026-07-26*
