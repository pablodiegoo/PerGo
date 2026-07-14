---
spike: 016
name: selective-metadata-logging
type: standard
validates: "Given a workspace with message body logging disabled, when a message is processed, then audit logs persist only cryptographic metadata without storing the message body or media URL in plaintext."
verdict: VALIDATED
related: [012]
tags: [db, compliance, privacy]
---

# Spike 016: Selective Metadata Logging

## What This Validates
This spike validates the implementation of compliance privacy policies (such as `save_message_bodies = false` similar to Evolution API settings). It verifies that we can redact PII (specifically message body and media URL values) while preserving useful operational metadata (body length, media type, sender, recipient) for billing, volume metrics, and connection audit trails.

## Research
### Competing Approaches

| Approach | Tool/Library | Pros | Cons | Status |
|----------|-------------|------|------|--------|
| Store full bodies with encryption | AES-256-GCM | Encrypted database storage, data can still be read by authorized admins. | Large database footprint, high processing overhead for decrypting logs in index views. | Rejected |
| Zero audit logs | Complete disabling of logs | Absolute privacy. | No way to track billing metrics, rate limits, or trace delivery errors. | Rejected |
| Selective metadata redaction | Strip body & media_url, preserve metrics | Compliant with GDPR/LGPD, minimal database growth, tracks dispatch metrics cleanly. | Body cannot be retrieved later for history review. | Chosen |

## How to Run
Run the unit tests verifying the redaction engine:
```bash
go test .planning/spikes/016-selective-metadata-logging/logging_test.go -v
```

## What to Expect
- If `SaveMessageBodies` is enabled, message payloads are persisted completely.
- If `SaveMessageBodies` is disabled, the body and media URL fields are stripped from the payload.
- Message metadata like `body_length`, `media_type`, `from`, and `to` are preserved in the JSON payload structure.

## Investigation Trail
- **Iteration 1**: Designed the compliance toggles at the workspace level.
- **Iteration 2**: Drafted the `RedactEvent` function that parses JSON bytes and conditionally returns filtered/redacted JSON bytes.
- **Iteration 3**: Verified that the redacted JSON has zero traces of sensitive text or link properties but still retains metrics.

## Results
- **Verdict**: **VALIDATED**
- **Evidence**: Verified that the redaction engine successfully isolates metadata and strips the payload content when message body logging is turned off.
