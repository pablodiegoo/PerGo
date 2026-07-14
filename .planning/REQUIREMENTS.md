# Requirements: PerGo

**Defined:** 2026-07-14
**Core Value:** A single API request delivers a message through any configured channel with automatic fallback — without per-message markup, without vendor lock-in, and with full custody of transaction data on infrastructure you control.

## v1 Requirements

### Campaign Management

- [ ] **CAMP-01**: User can upload a CSV mailing list via the admin panel.
- [ ] **CAMP-02**: System validates and sanitizes mailing lists, filtering out numbers that do not match E.164 length constraints (10-15 digits).
- [ ] **CAMP-03**: System deduplicates the mailing list automatically to prevent duplicate sends.
- [ ] **CAMP-04**: User can map WABA template variables dynamically using plain text fields with regex-based multi-variable interpolation (e.g. `{{nome}} de {{cidade}}`).
- [ ] **CAMP-05**: User can configure campaign batch parameters: batch size (number of messages per batch) and inter-batch delay.
- [ ] **CAMP-06**: System calculates and displays the estimated campaign dispatch duration based on batch size, delay, and jitter parameters.
- [ ] **CAMP-07**: System schedules and dispatches campaign messages in the background using NATS JetStream, applying staggered dispatch delay with random jitter.
- [ ] **CAMP-08**: System persists campaign dispatch metadata (campaign_id, template_name, variables_json) in the main `outbound_logs` table, with optimized composite indexes.

## v2 Requirements

### Analytics & Visual Builder
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
| CAMP-01 | Phase 12 | Pending |
| CAMP-02 | Phase 12 | Pending |
| CAMP-03 | Phase 12 | Pending |
| CAMP-04 | Phase 12 | Pending |
| CAMP-05 | Phase 12 | Pending |
| CAMP-06 | Phase 12 | Pending |
| CAMP-07 | Phase 12 | Pending |
| CAMP-08 | Phase 12 | Pending |

**Coverage:**
- v1 requirements: 8 total
- Mapped to phases: 8
- Unmapped: 0 ✓

---
*Requirements defined: 2026-07-14*
*Last updated: 2026-07-14 after initial v1.1 definition*
