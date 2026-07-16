# PRD Implementation Gaps

## Requirements

- All features described in `context/PRD PerGo.md` and `context/PRD-architecture-deepening.md` must be implemented before declaring feature-complete.
- Three validated spikes (015, 017, 018) represent features that were designed and proven in isolation but never integrated into the production codebase.

## Gap Inventory

### Gap 1: Omnichannel Contact Merging (Spike 017) 🔴 HIGH

**Tables needed:**
```sql
CREATE TABLE contacts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id),
    name VARCHAR(255),
    email VARCHAR(255),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE contact_identities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    contact_id UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    channel VARCHAR(50) NOT NULL,
    sender_identity VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(channel, sender_identity)
);
```

**Go code needed:**
- `Contact` and `ContactIdentity` domain structs
- `ContactRepository` with `ResolveContact(ctx, workspaceID, channel, senderIdentity)` — auto-creates contact + identity on first inbound
- `MergeContacts(ctx, primaryID, secondaryID)` — transactional merge
- Migration of `telegram_contacts` into `contact_identities`
- Inbox queries updated to join through contacts for cross-channel conversation view

**Impact:** Currently, conversations are identified by raw `(from, channel, to)` tuples from audit_logs GROUP BY. Same person on WhatsApp and Telegram appears as two separate conversations.

### Gap 2: Multi-Webhook Subscriptions (Spike 018) 🔴 HIGH

**Table needed:**
```sql
CREATE TABLE webhook_subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id),
    url TEXT NOT NULL,
    secret TEXT NOT NULL,
    event_types TEXT[] NOT NULL DEFAULT '{}',
    active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
```

**Go code needed:**
- `WebhookSubscription` domain struct
- `WebhookSubscriptionRepository` with CRUD + `ListActiveByWorkspaceAndEvent(ctx, workspaceID, eventType)`
- Event-type matching with wildcard support (`message.*` matches `message.sent`, `message.delivered`)
- Concurrent fan-out dispatch in `webhook/dispatcher.go` — replace single-URL dispatch with multi-subscription fan-out
- Per-subscription DLQ tracking
- Admin UI for managing subscriptions (list, create, edit, delete, test)

**Impact:** Only one webhook URL per workspace today. Operators needing events to CRM + analytics + monitoring must build their own relay.

### Gap 3: Messaging Verbs Engine (Spike 015) 🟡 MEDIUM

**Go code needed:**
- Verb type definitions: `Reply`, `Wait`, `Forward`, `Tag`, `Close`
- `VerbExecutor` that processes a sequence of JSON-serialized verbs from webhook response bodies
- Integration into `webhook/dispatcher.go` — parse response body for verb instructions after successful delivery
- Dispatch pipeline integration for `Reply` verb (publish new outbound message)

**Impact:** Webhooks are currently fire-and-forget. CRM systems must make separate API calls to reply, adding latency and complexity.

## What to Avoid

- Don't try to implement all three gaps simultaneously — they're independent and should be separate phases.
- Don't break existing `audit_logs`-based conversation queries when adding the contacts model — the CTE approach works and should continue as a fallback.
- Don't remove `webhook_configs` table when adding `webhook_subscriptions` — migrate the data in a backward-compatible migration.
- Don't implement the verbs engine before multi-webhook subscriptions — subscriptions are a prerequisite for verbs (you need the response body from the webhook call).

## Constraints

- The `contacts` table must support the existing inbox without breaking the `audit_logs` GROUP BY queries. Add contacts as an enrichment layer, not a replacement.
- Webhook fan-out must be concurrent but respect per-URL rate limits. Use a bounded goroutine pool.
- The verbs engine must validate verb sequences — e.g., `Wait` must specify a timeout, `Reply` must have a valid message payload.

## Recommended Implementation Order

1. **Multi-Webhook Subscriptions** (G2) — lowest effort, highest operator impact
2. **Omnichannel Contact Merging** (G1) — foundation for unified customer profiles
3. **Messaging Verbs Engine** (G3) — power feature, can be deferred

## Origin

Synthesized from spike: 024
Source files available in: sources/024-prd-implementation-gap-audit/
