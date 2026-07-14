# ADR-0002: Extract InboundProcessor from Session Manager

**Status:** Proposed  
**Date:** 2026-07-03

## Context

The WhatsApp inbound message handler is a 130-line anonymous function embedded in `internal/session/manager.go`'s `reconnectDevice`. It handles text extraction, media download from WhatsApp CDN, S3 upload, deduplication, PII opt-in, event construction, NATS publish, and audit — all inline. Zero test coverage exists because the handler can't be invoked without a live whatsmeow client.

## Decision

Extract an **InboundProcessor** module with interface `Handle(ctx, waMsg, media, workspaceID, senderJID) error`.

### Constructor (5 params)

```
dedupRepo, wsRepo, s3Client, publisher, auditWriter
```

### Boundaries

- **Processor owns:** S3 upload, dedup check, PII check, payload construction, NATS publish, audit write
- **Processor does NOT own:** WhatsApp CDN media download (stays in the thin event handler adapter — uses the active whatsmeow client)
- **Raw whatsmeow type accepted directly** — `*waEvents.Message` is passed through. No port abstraction needed (one adapter, hypothetical seam). The structs are plain data; no live connection required to construct them for tests.

### Interface

```go
type InboundMedia struct {
    Data     []byte
    MimeType string
    FileName string
    Caption  string
}

func (p *InboundProcessor) Handle(
    ctx context.Context,
    waMsg *waEvents.Message,
    media *InboundMedia,
    workspaceID uuid.UUID,
    senderJID string,
) error
```

## Test strategy

Table-driven with fakes for all 5 dependencies:

| Scenario | Expected |
|----------|----------|
| Text-only message | payload with body, publish, audit |
| Image with media | S3 upload, `media_url` in payload |
| Duplicate message | no publish, no audit |
| PII disabled | no location/contacts in payload |
| PII enabled + location | `location` field in payload |
| Media > 25MB | skipped S3, still publish text |
| S3 upload fails | logged, no media, still publish |
| Empty message | return early, no publish |

## Consequences

- **Testability:** inbound logic testable without whatsmeow
- **Locality:** inbound bugs concentrate in one module
- **Leverage:** same processor structure usable by Telegram inbound handler (future)
- **Deletion test:** deleting the processor re-exposes every check across the event handler
