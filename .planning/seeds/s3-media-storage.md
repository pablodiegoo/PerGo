---
title: Pluggable Media Storage (S3/MinIO)
trigger_condition: Before any production deployment or when media volume exceeds local disk feasibility
planted_date: 2026-07-25
context: gsd-explore session — WABA features gap analysis. CPaaS table-stakes infrastructure.
spike_needed: true
---

# Pluggable Media Storage (S3/MinIO)

## Idea

Replace the current local filesystem media storage with a pluggable `media.Store` interface that supports local (dev), S3, MinIO, and potentially GCS/Azure Blob — essential for production multi-instance deployments.

## Current State

PerGo's `media.Downloader` downloads inbound WhatsApp media (images, documents, audio, video) from Meta's Graph API and stores them locally. This breaks in:
- Multi-instance deployments (instance A downloads, instance B can't serve)
- Container restarts (ephemeral filesystems lose media)
- Disk space constraints (audio/video can be large)

## Proposed Architecture

```go
type Store interface {
    Upload(ctx context.Context, key string, data io.Reader, contentType string) (url string, err error)
    Download(ctx context.Context, key string) (io.ReadCloser, error)
    GenerateURL(ctx context.Context, key string, expiry time.Duration) (string, error)
    Delete(ctx context.Context, key string) error
}
```

### Implementations
- `LocalStore` — filesystem (dev/single-instance)
- `S3Store` — AWS S3 / MinIO / any S3-compatible
- Future: `GCSStore`, `AzureBlobStore`

### Key path format
`{workspace_id}/{channel}/{contact_phone}/{media_type}/{timestamp}_{original_filename}`

## Spike Scope

Before implementation, spike should validate:
1. Can we retrofit `media.Store` into the existing `media.Downloader` without breaking the inbound pipeline?
2. What's the right S3 Go SDK? (`aws-sdk-go-v2` vs `minio-go` — minio-go is S3-compatible AND works with MinIO natively)
3. Signed URL generation for serving media to frontend without exposing bucket
4. Content-Type detection and preservation across upload/download

## Dependencies

- No blocking dependencies — this is foundational infrastructure

## Inspiration

- Evolution API: S3/MinIO storage under `{instanceId}/{remoteJid}/{mediaType}/{fileName}`
- Chatwoot: ActiveStorage with S3 backend
