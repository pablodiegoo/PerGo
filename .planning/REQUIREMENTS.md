# Requirements: PerGo

**Defined:** 2026-07-16
**Core Value:** A single API request delivers a message through any configured channel with automatic fallback — without per-message markup, without vendor lock-in, and with full custody of transaction data on infrastructure you control.

## v1.2 Requirements

### Webhook Subscriptions

- [ ] **SUBS-01**: User can manage multiple active webhook subscriptions per workspace (create, list, update, delete) via the admin dashboard.
- [ ] **SUBS-02**: System filters webhook dispatches by event types with wildcard matching support (e.g., `message.*` matches all message events).
- [ ] **SUBS-03**: System dispatches webhook events concurrently to all matching subscription URLs using NATS JetStream fan-out.
- [ ] **SUBS-04**: System tracks delivery failures and retries with exponential backoff on a per-subscription basis, persisting permanently failed events to the Webhook DLQ.

### Omnichannel Contact Merging

- [ ] **CONT-01**: System stores customer contacts and links multiple channel identities (e.g. WhatsApp number, Telegram handle) to a single contact profile.
- [ ] **CONT-02**: System automatically resolves, matches, or creates a contact profile upon receiving an inbound message on any active connection.
- [ ] **CONT-03**: User can merge multiple contact profiles together via the API or dashboard console, consolidating their conversation and dispatch history.
- [ ] **CONT-04**: System displays unified conversation threads in the Inbox dashboard, grouping messages from all linked identities of the same contact.

### Webhook Messaging Verbs Engine

- [ ] **VERB-01**: System parses declarative messaging verbs (`reply`, `wait`, `forward`, `tag`, `close`) returned in the JSON response of webhook dispatches.
- [ ] **VERB-02**: System executes declarative verbs in sequence (e.g. tagging a session, waiting 2s, and replying with a template).
- [ ] **VERB-03**: Webhook response execution errors are recorded in the workspace audit log and visible to operators.

## Completed Requirements

### Campaign Management
- [x] **CAMP-01**: User can upload a CSV mailing list via the admin panel.
- [x] **CAMP-02**: System validates and sanitizes mailing lists, filtering out numbers that do not match E.164 length constraints (10-15 digits).
- [x] **CAMP-03**: System deduplicates the mailing list automatically to prevent duplicate sends.
- [x] **CAMP-04**: User can map WABA template variables dynamically using plain text fields with regex-based multi-variable interpolation (e.g. `{{nome}} de {{cidade}}`).
- [x] **CAMP-05**: User can configure campaign batch parameters: batch size (number of messages per batch) and inter-batch delay.
- [x] **CAMP-06**: System calculates and displays the estimated campaign dispatch duration based on batch size, delay, and jitter parameters.
- [x] **CAMP-07**: System schedules and dispatches campaign messages in the background using NATS JetStream, applying staggered dispatch delay with random jitter.
- [x] **CAMP-08**: System persists campaign dispatch metadata (campaign_id, template_name, variables_json) in the main `outbound_logs` table, with optimized composite indexes.

## v2 Requirements
- **CAMP-09**: Detailed analytics dashboards displaying delivery, read, and failure rates per campaign.
- **CAMP-10**: Drag-and-drop conversational flow builders.

## Out of Scope

| Feature | Reason |
|---------|--------|
| Real-time voice orchestration | Non-messaging channels are outside the scope of PerGo |
| Separate tables for campaign dispatches | Enriched outbound logs chosen for simpler analytics query creation |

## Traceability

| Requirement | Phase | Status |
|-------------|-------|--------|
| CAMP-01 | Phase 12 | Validated |
| CAMP-02 | Phase 12 | Validated |
| CAMP-03 | Phase 12 | Validated |
| CAMP-04 | Phase 12 | Validated |
| CAMP-05 | Phase 12 | Validated |
| CAMP-06 | Phase 12 | Validated |
| CAMP-07 | Phase 12 | Validated |
| CAMP-08 | Phase 12 | Validated |
| SUBS-01 | Phase 17 | Pending |
| SUBS-02 | Phase 17 | Pending |
| SUBS-03 | Phase 17 | Pending |
| SUBS-04 | Phase 17 | Pending |
| CONT-01 | Phase 18 | Pending |
| CONT-02 | Phase 18 | Pending |
| CONT-03 | Phase 18 | Pending |
| CONT-04 | Phase 18 | Pending |
| VERB-01 | Phase 19 | Pending |
| VERB-02 | Phase 19 | Pending |
| VERB-03 | Phase 19 | Pending |

**Coverage:**
- Active requirements: 11 total
- Mapped to phases: 11
- Completed requirements: 8 total
- Mapped to phases: 8
- Unmapped active: 0 ✓

---
*Requirements defined: 2026-07-16*
*Last updated: 2026-07-16 after v1.2 definition*
