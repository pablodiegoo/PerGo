# Phase 30: Session Window & Inbound Foundation — Research

**Researched:** 2026-07-26
**Requirements:** SESS-01, SESS-02, SESS-03, SESS-04, SESS-05

## 1. Summary of Findings by Research Area

### A. Session Window Verification (`WindowChecker` Extension)
- **Current Signature**: `IsWindowOpen(ctx, workspaceID, phone, channel, identity) (bool, error)` in `internal/session/window.go:29`.
- **Target Signature**: `IsWindowOpen(ctx context.Context, workspaceID uuid.UUID, recipientPhone string, channel string, recipientIdentity string, safetyBuffer time.Duration) (*WindowStatus, error)`
- **`WindowStatus` Struct**:
  ```go
  type WindowStatus struct {
      Open           bool          `json:"open"`
      ExpiresAt      time.Time     `json:"expires_at"`
      EntryPointType string        `json:"entry_point_type"` // "standard" (24h) or "ctwa" (72h)
      WindowDuration time.Duration `json:"window_duration"`  // 24h or 72h
  }
  ```
- **Window Logic**:
  - `duration = 24 * time.Hour` (default/standard). If `EntryPointType == "ctwa"`, `duration = 72 * time.Hour`.
  - `expiresAt = LastInboundAt.Add(duration)`.
  - `cutoff = expiresAt.Add(-safetyBuffer)`.
  - `Open = time.Now().UTC().Before(cutoff)`.
- **Safety Buffers**:
  - **Ingestion time (API)**: `safetyBuffer = 0` (full 24h/72h window).
  - **Dispatch time (WABA worker)**: `safetyBuffer = 5 * time.Minute` (23h55m cutoff for 24h; 71h55m cutoff for 72h).

### B. Ingestion-Time Pre-Flight Check Wiring (POST /messages)
- **Insertion Point**: Inside `OutboundProcessor.Ingest` in `internal/outbound/processor.go:160` (right after connection route resolution) or called in `MessageHandler.Create` in `internal/api/handler/message.go:87`.
- **Condition**: Gated strictly by `conn.Channel == "whatsapp_cloud" && req.TemplateName == ""` (freeform WABA message).
- **Execution**: Invokes `windowChecker.IsWindowOpen(ctx, workspaceID, req.To, "whatsapp_cloud", conn.SenderIdentity, 0)`.
- **HTTP 422 Response Format**:
  ```json
  {
    "code": "SESSION_WINDOW_EXPIRED",
    "message": "Customer service window expired for recipient",
    "details": {
      "window_expired_at": "2026-07-26T10:00:00Z",
      "window_duration": "24h",
      "hint": "Use type: template to reach this contact",
      "source": "ingestion"
    }
  }
  ```

### C. Dispatch-Time Re-Validation Wiring (WABA Worker)
- **Insertion Point**: `WABAAdapter.Dispatch` in `internal/channel/whatsapp/waba.go:192-200`.
- **Execution**: Calls `windowChecker.IsWindowOpen(ctx, workspaceID, m.To, "whatsapp_cloud", conn.SenderIdentity, 5*time.Minute)`.
- **Failure Action**:
  - If `!status.Open`, returns `channel.NewTerminalError(ErrSessionWindowExpired)`.
  - `DispatchOrchestrator` (`internal/platform/queue/orchestrator.go:201`) recognizes terminal error, ACKs NATS message (preventing infinite retry loops), marks dispatch status as `"failed"`, and emits `message.window_expired` webhook event to subject `"webhooks.events"`.

### D. Background Expiration Ticker & Webhook Events
- **Component**: Create `SessionTicker` in `internal/session/expiring_ticker.go`.
- **Execution**: Ticker runs every 5 minutes (`time.NewTicker(5 * time.Minute)`).
- **Query**: Queries `recipient_sessions` where `last_inbound_at` is between `now() - duration + 1h` and `now() - duration + 1h + 5m` AND `notified_expiring_at IS NULL`.
- **Notification**: For each expiring session:
  1. Emits event `session.expiring_soon` to NATS subject `"webhooks.events"`.
  2. Updates `notified_expiring_at = time.Now().UTC()`.
- **Reset**: When a new user inbound arrives, `RecipientSessionRepository.Upsert` sets `notified_expiring_at = NULL`, allowing future expiration warnings.

### E. 72h CTWA Free Entry Point Tracking
- **Meta Inbound Parsing**: In `internal/channel/whatsapp/waba_inbound.go:65-91`, inspect Meta Cloud API payload for `referral` object on `messages[i]`.
  ```go
  type wabaReferralObj struct {
      SourceURL  string `json:"source_url,omitempty"`
      SourceID   string `json:"source_id,omitempty"`
      SourceType string `json:"source_type,omitempty"` // "ad" or "post"
      Headline   string `json:"headline,omitempty"`
      Body       string `json:"body,omitempty"`
  }
  ```
- **Flow**: If `msg.Referral != nil` (or `msg.Referral.SourceType == "ad"`), set `InboundEvent.Metadata["entry_point_type"] = "ctwa"`.
- **Processor**: In `InboundProcessor.Process` (`internal/inbound/processor.go:247`), pass `entry_point_type` from `ev.Metadata` to `RecipientSessionRepository.Upsert`.
- **Reset**: Any subsequent non-ad user message passes `entry_point_type = "standard"`, resetting the entry point type back to standard.

---

## 2. Integration Points (File Paths & Line Numbers)

| Component | File Path | Line Range / Location | Purpose |
|-----------|-----------|-----------------------|---------|
| `RecipientSessionRepository` | `internal/repository/recipient_session.go` | L18–L47 | Extend `Upsert` to accept `entryPointType string` and reset `notified_expiring_at = NULL`. Add `GetExpiringSessions` query. |
| `WindowChecker` | `internal/session/window.go` | L29–L39 | Update `IsWindowOpen` signature with `safetyBuffer` param and `*WindowStatus` return type. |
| `WindowChecker Test` | `internal/session/window_test.go` | L20–L90 | Update unit tests to verify `WindowStatus`, 5m safety buffer, and 72h CTWA window behavior. |
| Outbound Processor | `internal/outbound/processor.go` | L160–L180 | Wire ingestion-time window check for `whatsapp_cloud` non-template messages. |
| Message Handler | `internal/api/handler/message.go` | L87–L145 | Map `*SessionWindowError` to HTTP 422 `SESSION_WINDOW_EXPIRED` structured error response. |
| WABA Dispatcher | `internal/channel/whatsapp/waba.go` | L192–L200 | Update dispatch-time `IsWindowOpen` check with `safetyBuffer = 5 * time.Minute`. |
| WABA Inbound Adapter | `internal/channel/whatsapp/waba_inbound.go` | L65–L145, L216–L228 | Unmarshal `referral` object from Meta webhook payload and set `entry_point_type = "ctwa"` in `Metadata`. |
| Inbound Processor | `internal/inbound/processor.go` | L247–L253 | Pass `entry_point_type` metadata to `recipientSessionRepo.Upsert(...)`. |
| Webhook Publisher | `internal/platform/queue/webhook_worker.go` | L63, L20–L28 | Reuses `"webhooks.events"` NATS subject to deliver `session.expiring_soon` and `message.window_expired` events to subscribers. |
| Application Main | `cmd/pergo/main.go` | Server startup | Register and launch `SessionTicker` background goroutine. |

---

## 3. Code Patterns to Follow

### Dual Enforcement Pattern
- **Ingestion (`safetyBuffer = 0`)**: Catches expired sessions early at HTTP ingestion time, returning HTTP 422 before publishing to NATS JetStream.
- **Dispatch (`safetyBuffer = 5 * time.Minute`)**: Re-evaluates session status inside `WABAAdapter.Dispatch` right before Meta Graph API POST call to guard against queue latency boundary violations.

### Error Mapping Pattern
Follow existing `domain.ErrorResponse` pattern in `internal/api/handler/message.go`:
```go
type SessionWindowError struct {
    Status *session.WindowStatus
    Source string // "ingestion" or "dispatch"
}
```
HTTP 422 JSON response:
```json
{
  "code": "SESSION_WINDOW_EXPIRED",
  "message": "Customer service window expired for recipient",
  "details": {
    "window_expired_at": "2026-07-26T10:00:00Z",
    "window_duration": "24h",
    "hint": "Use type: template to reach this contact",
    "source": "ingestion"
  }
}
```

### Background Worker Pattern
Follow `Worker` pattern in `internal/platform/queue/worker.go` and `internal/platform/audit/batch.go`:
```go
type SessionTicker struct {
    repo      *repository.RecipientSessionRepository
    publisher Publisher
    cancel    context.CancelFunc
    done      chan struct{}
}
```
Use `time.NewTicker(5 * time.Minute)` inside run loop with `select { case <-ctx.Done(): ... case <-ticker.C: ... }`.

---

## 4. Migration Numbering Info

- **Directory**: `internal/platform/postgres/migrations/`
- **Latest Migration**: `030_connection_slugs.sql`
- **New Migration**: `031_extend_recipient_sessions.sql`

```sql
-- +goose Up
-- +goose StatementBegin
ALTER TABLE recipient_sessions 
    ADD COLUMN IF NOT EXISTS entry_point_type VARCHAR(20) NOT NULL DEFAULT 'standard',
    ADD COLUMN IF NOT EXISTS notified_expiring_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_recipient_sessions_expiring 
    ON recipient_sessions(last_inbound_at) 
    WHERE notified_expiring_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_recipient_sessions_expiring;
ALTER TABLE recipient_sessions 
    DROP COLUMN IF EXISTS notified_expiring_at,
    DROP COLUMN IF EXISTS entry_point_type;
-- +goose StatementEnd
```

---

## 5. Risks & Gotchas Discovered

1. **Clock Drift**: Meta's servers and self-hosted PerGo server clock may differ by seconds/minutes. The 5-minute safety buffer at dispatch guarantees PerGo does not attempt outbound calls that Meta would reject with Graph API Error `131047`.
2. **Template Messages Exemption**: Template messages (`type: "template"`) MUST bypass window checking at both ingestion and dispatch. Freeform media/interactive/text messages MUST undergo window checking.
3. **WABA-Channel Scoping**: WhatsApp Web (`whatsmeow`) and Telegram do NOT enforce 24h customer service windows. All window enforcement checks MUST be gated by `channel == "whatsapp_cloud"`.
4. **Idempotent Expiration Events**: Without `notified_expiring_at`, the 5-minute ticker would re-notify the same expiring session multiple times within the 1-hour expiration window. `Upsert` MUST reset `notified_expiring_at = NULL` whenever a new inbound message arrives.
5. **Entry Point Reset**: CTWA 72h window applies ONLY while the ad conversation is active. Per D-14, any standard non-ad user message resets `entry_point_type = "standard"`.

---

## RESEARCH COMPLETE
