# Requirements: PerGo

**Defined:** 2026-07-17
**Core Value:** A single API request delivers a message through any configured channel with automatic fallback — without per-message markup, without vendor lock-in, and with full custody of transaction data on infrastructure you control.

## v1.3 Requirements

### Chatwoot Integration

- [x] **CHAT-01**: User can configure a Chatwoot connection (API URL, Access Token, Inbox ID, and Account ID) per workspace via the active connections settings interface.
- [x] **CHAT-02**: System exposes a built-in native integration receiver endpoint (`POST /api/integrations/chatwoot`) to ingest outbound webhook payloads from Chatwoot.
- [x] **CHAT-03**: System parses Chatwoot agent reply payloads, extracts the recipient identity, and maps the payload to the PerGo outbound queue (`messages.outbound`) for dispatch.
- [x] **CHAT-04**: System synchronizes inbound contact messages (from WABA or Telegram connections) to Chatwoot as contact messages, automatically creating or updating contacts in Chatwoot using their active channel identities.

### Typebot Integration

- [ ] **TYPE-01**: User can configure a Typebot connection (API URL, Bot ID, Public Token) per workspace via the active connections settings interface.
- [ ] **TYPE-02**: System exposes a built-in native integration receiver endpoint (`POST /api/integrations/typebot`) to receive chat message responses from Typebot.
- [ ] **TYPE-03**: System parses Typebot responses and maps them to the PerGo outbound queue (`messages.outbound`) for dispatch.
- [ ] **TYPE-04**: System forwards inbound customer messages to the Typebot execution API, maintaining the active chatbot session context (session ID) per contact identity.

### Stateful Handoff Routing

- [ ] **HAND-01**: System persists a boolean `bot_active` flag (defaulting to `true`) and `bot_paused_at` timestamp on the `contacts` table to track conversational control.
- [ ] **HAND-02**: System forwards inbound messages to the Typebot integration ONLY if the `bot_active` flag is set to `true` for that contact.
- [ ] **HAND-03**: System automatically sets `bot_active` to `false` and records `bot_paused_at = NOW()` when a human agent reply webhook event is received from Chatwoot.
- [ ] **HAND-04**: System extends the Messaging Verbs Engine with a `pause_bot` verb (accepting an optional duration parameter) to pause bot responses programmatically via webhooks.
- [ ] **HAND-05**: User can manually toggle the `bot_active` flag per contact via the admin Inbox chat panel UI.
- [ ] **HAND-06**: System automatically resets `bot_active` to `true` after a configurable inactive cooldown duration (defaulting to 12 hours) from the last human agent message.

## Completed Requirements

*(No requirements completed yet for milestone v1.3. For previously completed requirements, see completed milestones under milestones/.)*

## v2 Requirements

- **CAMP-09**: Detailed analytics dashboards displaying delivery, read, and failure rates per campaign.
- **CAMP-10**: Drag-and-drop conversational flow builders.
- **INTEG-05**: Dynamic routing configuration for other messaging integrations (e.g. Zendesk, Slack).

## Out of Scope

| Feature | Reason |
|---------|--------|
| Multi-account Chatwoot workspaces | PerGo implements single Chatwoot account integration mapping per PerGo workspace for simplicity |
| Custom Typebot block executions | Custom bot blocks that require special media/location structures outside standard message types are deferred |

## Traceability

| CHAT-01 | Phase 21 | Complete |
| CHAT-02 | Phase 21 | Complete |
| CHAT-03 | Phase 21 | Complete |
| CHAT-04 | Phase 21 | Complete |
| TYPE-01 | Phase 22 | Pending |
| TYPE-02 | Phase 22 | Pending |
| TYPE-03 | Phase 22 | Pending |
| TYPE-04 | Phase 22 | Pending |
| HAND-01 | Phase 23 | Pending |
| HAND-02 | Phase 23 | Pending |
| HAND-03 | Phase 23 | Pending |
| HAND-04 | Phase 23 | Pending |
| HAND-05 | Phase 23 | Pending |
| HAND-06 | Phase 23 | Pending |

**Coverage:**

- Active requirements: 14 total
- Mapped to phases: 14
- Completed requirements: 0 total
- Mapped to phases: 0
- Unmapped active: 0 ✓

---
*Requirements defined: 2026-07-17*
*Last updated: 2026-07-17 after v1.3 definition*
