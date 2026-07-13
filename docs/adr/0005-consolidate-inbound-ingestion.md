# ADR-0005: Consolidate Inbound Ingestion

**Status:** Proposed  
**Date:** 2026-07-13

## Context

Inbound message ingestion logic (deduplication, recipient session tracking, PII opt-in checks, S3 media uploads, NATS event publishing, and audit logging) was previously implemented in three separate places:
1. WhatsApp Web inbound handler (`internal/session/manager.go`) delegating to a whatsmeow-specific `session.InboundProcessor`
2. Telegram webhook handler (`internal/api/handler/telegram_webhook.go`)
3. WABA webhook handler (`internal/api/handler/waba_webhook.go`)

This duplication created significant architectural friction. Changes to inbound event schemas, media key structures, or compliance filters had to be synchronized across three modules. The webhook handlers became shallow wrappers with bloated implementations that were difficult to test.

## Decision

We will extract a channel-agnostic **InboundProcessor** in a new package `internal/inbound`.

### Boundaries & Division of Labor

- **Adapters (Webhooks & Event Listeners) own:** Payload binding, secret verification, and downloading raw media bytes from provider CDNs (using their specific access tokens).
- **Processor owns:** Database deduplication, workspace PII opt-in loading, S3 media uploads, recipient session upserting, NATS JetStream event publishing, and audit logging.

```
┌──────────────────────────────────────────────┐
│  Adapter (Webhook / Event Listener)          │  ← Binds JSON, downloads CDN media
├──────────────────────────────────────────────┤
│  Seam: Process(ctx, *InboundEvent)           │  ← Channel-agnostic payload
├──────────────────────────────────────────────┤
│  Deep InboundProcessor                       │  ← Dedup, PII check, S3 upload,
│                                              │    NATS publish, Audit write
└──────────────────────────────────────────────┘
```

### InboundEvent Interface

```go
type InboundMedia struct {
	Bytes     []byte
	MediaType string // "image", "document", "audio", "video"
	Filename  string
	Caption   string
}

type InboundLocation struct {
	Latitude  float64
	Longitude float64
	Name      string
	Address   string
}

type InboundContact struct {
	Name  string
	Phone string
}

type InboundEvent struct {
	WorkspaceID uuid.UUID
	MessageID   string
	Channel     string
	From        string
	To          string
	Body        string
	Media       *InboundMedia
	Location    *InboundLocation
	Contacts    []InboundContact
}
```

### Error Resiliency

- **Fatal errors** (duplicate message detected, DB down, NATS down) return concrete errors to the adapters.
- **Non-fatal errors** (S3 upload fail, audit log write fail) are logged (`slog.Error`) internally, and the processor continues processing to publish the message text.

## Test Strategy

We will write table-driven integration tests for `InboundProcessor` using `getTestPool(t)` to execute real database queries against:
- `WorkspaceRepository`
- `InboundDedupRepository`
- `RecipientSessionRepository`

We will inject fake/noop implementations for NATS publishing and S3 uploads to isolate tests from running infrastructure.

## Consequences

- **Locality:** Inbound business rules are unified in one deep module.
- **Leverage:** A single interface serves three active channel adapters (WhatsApp Web, WABA, Telegram).
- **Testability:** Inbound event processing can be tested entirely in-process using table-driven tests without setting up HTTP fakes or Mock engines.
