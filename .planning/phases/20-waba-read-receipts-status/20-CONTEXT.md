# Phase 20: WABA Read Receipts & Status Updates - Context

**Gathered:** 2026-07-16
**Status:** Ready for planning

<domain>
## Phase Boundary

This phase implements tracking for outbound WABA (WhatsApp Cloud API) message status changes (sent, delivered, read). When Meta sends status receipt webhooks, PerGo matches them back to the original database dispatches and propagates the status updates to both system webhooks and the admin Inbox UI.

## Key Decisions

1. **Database Schema**: Add `provider_message_id VARCHAR(255) UNIQUE` to the `message_dispatches` table.
2. **Outbound Dispatch Mapping**: Update the WABA channel adapter to parse Meta's response `wamid` (message ID) and update the dispatch record with `provider_message_id = wamid`.
3. **Inbound Status Hook**: Refactor the WABA webhook parser to parse the `statuses` array and append a status event to the returned inbound events list, instead of ignoring it.
4. **NATS & Inbound Processing**: Route status events through NATS to the inbound processor. The processor updates the corresponding dispatch record and triggers real-time updates.
5. **Real-time UI Propagation**: The Inbox chat view's message bubbles will query the status from the dispatch record and render delivery/read indicators (e.g. gray single check, gray double check, blue double check).
</domain>
