# Technical Research: Phase 13 - Deepen Media Engine

This research maps the code structures, interfaces, and friction points associated with downloading, hashing, and storing media files in PerGo.

## 1. Existing System & Friction Points

### Media Download & Upload Duplication
The logic for media ingestion is currently split between inbound and outbound messaging flows:
- **Outbound Ingestion ([outbound/processor.go](file:///home/pablo/Coding/OmniGo/internal/outbound/processor.go#L92-L132))**:
  Downloads remote media using the `media.Downloader` seam, validates its size limits (25MB), hashes the bytes, uploads to the S3 bucket using `uploader.Upload`, and rewrites the URL to the proxy scheme (`/media/<workspace>/<hash>.<ext>`).
- **Inbound Ingestion ([inbound/processor.go](file:///home/pablo/Coding/OmniGo/internal/inbound/processor.go#L164-L184))**:
  Sniffs MIME types, computes hashes, creates the S3 key layout (`workspace/hash.ext`), uploads using `s3Client.Upload`, and creates the event media structure with the proxy URL scheme.

This duplicates:
1. The calculation of SHA-256 hashes for raw bytes.
2. Extension resolution from MIME type.
3. Key structure formatting (`<workspace_id>/<hash>.<ext>`).
4. Proxy URL structure formatting (`/media/<workspace_id>/<hash>.<ext>`).
5. Size boundary enforcement (25MB limit).

### Shallow Interfaces
The `media.Engine` interface currently has:
- `Download(ctx context.Context, url string, headers map[string]string, maxBytes int64) (*DownloadResult, error)`
- `Upload(ctx context.Context, key string, data []byte, contentType string) error`

This interface is nearly as complex as its implementation, forcing callers to coordinate the step-by-step pipeline.

## 2. Refactoring Blueprint

We will consolidate this behavior behind a deep interface:
```go
type Engine interface {
	ProcessOutbound(ctx context.Context, workspaceID uuid.UUID, mediaURL string) (string, error)
	ProcessInbound(ctx context.Context, workspaceID uuid.UUID, mediaType string, data []byte) (string, error)
}
```

The `DefaultEngine` implementation will internally handle:
- Size boundary validation.
- Remote HTTP fetching.
- Hashing and MIME sniffing.
- Key formatting and S3 uploads.
- Proxy URL formatting.

## 3. Errors to Propagate
- `media.ErrMediaSizeExceeded` (standard terminal error, mapped to 422 HTTP).
- `media.ErrMediaDownloadFailed` (mapped to 400 bad request).
