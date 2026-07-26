# Phase 30 Discussion Log

**Date:** 2026-07-26
**Areas covered:** 4 of 4

## Area 1: Dispatch-time Enforcement

### Q1: Where should the 24h window check run?
- **Options:** Both ingestion + dispatch (recommended) | Ingestion only | Dispatch only
- **Selected:** Both ingestion + dispatch

### Q2: How should the 5-minute safety buffer work?
- **Options:** 5min at dispatch only (recommended) | 5min at both levels | Configurable buffer
- **Selected:** 5min at dispatch only

### Q3: How should WindowChecker be extended?
- **Options:** Optional safetyBuffer parameter (recommended) | Separate IsWindowOpenForDispatch method | You decide
- **Selected:** Optional safetyBuffer parameter

### Q4: What should IsWindowOpen return?
- **Options:** WindowStatus struct (recommended) | (bool, error) like today | You decide
- **Selected:** WindowStatus struct

### Q5: Should the check apply to all channels or only WABA?
- **Options:** WABA-only (recommended) | All channels
- **Selected:** WABA-only

### Q6: Where should the ingestion-time check be wired?
- **Options:** Ingestion handler (recommended) | Dispatcher pre-check | You decide
- **Selected:** Ingestion handler

### Q7: What should happen on dispatch-time expiry?
- **Options:** Fail fast with terminal error (recommended) | Retry with delay | You decide
- **Selected:** Fail fast with terminal error

### Q8: Should the check be part of core Dispatcher interface or WABA-specific?
- **Options:** Skip for now, WABA-specific (recommended) | Wire into core dispatch
- **Selected:** Skip for now, WABA-specific

## Area 2: Session Expiration Events

### Q1: How should session.expiring_soon be triggered?
- **Options:** Background ticker (recommended) | Per-message scheduled check | You decide
- **Selected:** Background ticker

### Q2: How often should the background ticker run?
- **Options:** Every 5 minutes | Every 1 minute | Every 15 minutes | You decide
- **Selected:** Every 5 minutes

### Q3: How should session.expiring_soon be emitted?
- **Options:** Existing webhook subscription system (recommended) | Separate NATS subject | You decide
- **Selected:** Existing webhook subscription system

### Q4: How to prevent duplicate events?
- **Options:** notified_expiring_at column (recommended) | Separate session_notifications table | You decide
- **Selected:** notified_expiring_at column

## Area 3: 72h Entry Point Tracking

### Q1: How should CTWA entry points be tracked?
- **Options:** entry_point_type column on recipient_sessions (recommended) | Separate table | You decide
- **Selected:** entry_point_type column on recipient_sessions

### Q2: When should entry_point_type reset?
- **Options:** Reset to standard on non-ad inbound (recommended) | Keep ctwa until 72h expires | You decide
- **Selected:** Reset to standard on non-ad inbound

## Area 4: Error Response Design

### Q1: What should the HTTP 422 body contain?
- **Options:** Rich structured error (recommended) | Minimal error | You decide
- **Selected:** Rich structured error

### Q2: What error code should be used?
- **Options:** SESSION_WINDOW_EXPIRED (recommended) | WABA_SESSION_EXPIRED | WINDOW_CLOSED | You decide
- **Selected:** SESSION_WINDOW_EXPIRED

### Q3: Should dispatch-time failure use the same or different code?
- **Options:** Same code with source field (recommended) | Different code | You decide
- **Selected:** Same code with source: "dispatch" field
