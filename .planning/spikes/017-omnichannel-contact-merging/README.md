---
spike: 017
name: omnichannel-contact-merging
type: standard
validates: "Given a contact with active conversations on both WhatsApp and Telegram, when queried via a unified contacts API, then their identities are linked to a single customer profile with consolidated history."
verdict: VALIDATED
related: [012]
tags: [db, schema, omnichannel]
---

# Spike 017: Omnichannel Contact Merging

## What This Validates
This spike validates the implementation of a Chatwoot-style database mapping model that unifies recipient channel-specific identities (e.g. Telegram username, WhatsApp number) under a single `Contact` profile. It proves that we can dynamically resolve identities and merge profile references to consolidate conversation threads across multiple channels.

## Research
### Competing Approaches

| Approach | Tool/Library | Pros | Cons | Status |
|----------|-------------|------|------|--------|
| Channel-isolated Contacts | Store independent profiles per channel | Simplest schema. No merging complexity. | Same customer appears as separate contacts in the UI; conversation histories are fragmented. | Rejected |
| Unified Contact and Identity tables | Split profile details from channel-specific addresses | Unifies recipient representation. Supports merging profiles and linking multiple addresses. | Requires joint queries/joins to lookup. | Chosen |

## How to Run
Run the unit tests verifying the contact resolver and merging logic:
```bash
go test .planning/spikes/017-omnichannel-contact-merging/merging_test.go -v
```

## What to Expect
- Inbound messages trigger `ResolveContact`, which matches on active channel identities or registers a new contact + identity mapping.
- Merging unifies two contacts, migrating all associated channel identities and conversation histories to the primary contact ID.
- Resolving a secondary identity (e.g., Telegram) after a merge correctly resolves to the primary contact profile.

## Investigation Trail
- **Iteration 1**: Designed the in-memory database representation with `Contact`, `ContactIdentity`, and `Conversation` models.
- **Iteration 2**: Wrote the `ResolveContact` logic mimicking how Chatwoot registers channels.
- **Iteration 3**: Implemented `MergeContacts` updating composite mapping records.

## Results
- **Verdict**: **VALIDATED**
- **Evidence**: The prototype successfully unified independent Telegram and WhatsApp contacts, updating conversation pointer attributes and resolving single-identity queries cleanly.
