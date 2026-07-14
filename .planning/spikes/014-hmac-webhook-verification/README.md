---
spike: 014
name: hmac-webhook-verification
type: standard
validates: "Given a JSON webhook event payload, when signed with an HMAC-SHA256 signature header using a workspace-specific secret key, then the receiving server can verify authenticity."
verdict: VALIDATED
related: [013]
tags: [security, hmac, webhook]
---

# Spike 014: HMAC Webhook Verification

## What This Validates
This spike validates the generation of HMAC-SHA256 payload signatures to ensure target CRM applications can reliably authenticate that webhook events originated from PerGo.

## Research
### Competing Approaches

| Approach | Tool/Library | Pros | Cons | Status |
|----------|-------------|------|------|--------|
| Unsigned Webhooks | HTTP POST | Easiest implementation. | No way for receiving servers to verify authenticity; susceptible to spoofing. | Rejected |
| Static Authorization Header | Bearer token | Simple verification check. | Token can be leaked or replayed; does not protect payload integrity. | Rejected |
| HMAC-SHA256 Signature | Signature with Timestamp | Standard enterprise approach (e.g. Twilio/Stripe). Guarantees authenticity, payload integrity, and replay protection. | Requires secret management and key generation. | Chosen |

## How to Run
Run the webhook signature tests to verify signing correctness:
```bash
go test ./internal/webhook -run TestDefaultDispatcher_Dispatch/Normal_dispatch_signs_and_sends_HTTP_request -v
```

## What to Expect
- HMAC calculation combines a timestamp, delimiter (`.`), and the raw JSON body.
- Transmission of signature via the `X-PerGo-Signature` header in the format `t=<timestamp>,v1=<signature>`.
- Workspace-specific signing secrets are securely managed.

## Investigation Trail
- **Iteration 1**: Added the `SignPayload` helper in `internal/webhook/dispatcher.go` computing `hmac.New(sha256.New, secret)`.
- **Iteration 2**: Set up signature verification assertions in `internal/webhook/dispatcher_test.go`.

## Results
- **Verdict**: **VALIDATED**
- **Evidence**: The signature generated in `dispatcher.go` matches expected cryptographic hashes in tests, and successfully protects webhook payload authenticity.
