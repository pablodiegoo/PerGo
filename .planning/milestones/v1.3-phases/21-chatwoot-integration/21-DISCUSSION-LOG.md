# Phase 21: Chatwoot Integration - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-17
**Phase:** 21-chatwoot-integration
**Areas discussed:** Chatwoot Configuration Storage, Chatwoot Contact Mapping & Synchronization, Chatwoot Webhook Authentication, Webhook Outbound Message Filtering

---

## Chatwoot Configuration Storage

| Option | Description | Selected |
|--------|-------------|----------|
| Dedicated `chatwoot_configs` table | Create dedicated database table per integration | |
| Reuse `connections` table | Add new `chatwoot` channel type to connections table | |
| Unified `integrations` table | Single table for all third-party integrations with encrypted JSON config | ✓ |

**User's choice:** Unified `integrations` table.
**Notes:** The user raised a concern about database schema pollution if separate tables were created for each integration. A generic `integrations` table with encrypted JSON configurations solves this by allowing any future integration to scale into the same schema without database migration overhead.

---

## Chatwoot Contact Mapping & Synchronization

| Option | Description | Selected |
|--------|-------------|----------|
| Local mapping table | Store contact mapping locally for fast direct posts | ✓ |
| On-demand API searches | Search Chatwoot APIs on every inbound event (slower) | |

**User's choice:** Local mapping table.
**Notes:** Mapping database table will map PerGo's internal `contact_id` to Chatwoot's `chatwoot_contact_id` and `chatwoot_conversation_id`, reducing API lookup latencies and avoiding Chatwoot API rate limit risks.

---

## Chatwoot Webhook Authentication

| Option | Description | Selected |
|--------|-------------|----------|
| Custom API Key query parameter | Authenticate using `?token=KEY` query parameter | ✓ |
| Webhook Signature verification | Verify webhook payload signature using a shared secret | |

**User's choice:** Custom API Key query parameter.
**Notes:** Reuses PerGo's existing API key verification system by appending the token parameter directly to Chatwoot's webhook settings.

---

## Webhook Outbound Message Filtering

| Option | Description | Selected |
|--------|-------------|----------|
| Payload-based filtering | Filter for outgoing, public messages sent by a human agent | ✓ |
| Database-based tracking | Track outbound message IDs locally in the database | |

**User's choice:** Payload-based filtering.
**Notes:** Checks properties in incoming webhooks to process only messages where `message_type == "outgoing"`, `private == false`, and `sender.type == "user"`. This ensures system logs and customer-to-Chatwoot message synchronization do not cause looping.

---

## the agent's Discretion

- Exact mapping database schema naming.
- Chatwoot client API wrapper implementation details.
- Error handling/retry strategies for Chatwoot API failures.

## Deferred Ideas

- None — discussion stayed within phase scope.

---

*Phase: 21-chatwoot-integration*
*Discussion log generated: 2026-07-17*
