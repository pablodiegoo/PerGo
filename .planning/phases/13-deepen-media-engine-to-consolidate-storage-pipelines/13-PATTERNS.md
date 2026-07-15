# Pattern Map: Phase 13 - Deepen Media Engine

This pattern map lists the analog structures, files to be modified, and code snippets demonstrating the refactored design patterns.

## Files to Modify

| File Path | Role | Data Flow |
|-----------|------|-----------|
| `internal/media/engine.go` | Deep Module | Exposes the consolidated `Engine` interface and implements `DefaultEngine` wrapping downloader and S3 uploader. |
| `internal/inbound/processor.go` | Client | Call site for inbound media uploads. |
| `internal/outbound/processor.go` | Client | Call site for outbound remote media download and upload. |
| `cmd/pergo/main.go` | Injector | Passes `mediaEngine` to processors instead of raw `s3Client`. |

## Expected Pattern: Deep Media Engine Implementation

```go
package media

import (
	"context"
	"github.com/google/uuid"
)

type Engine interface {
	ProcessOutbound(ctx context.Context, workspaceID uuid.UUID, mediaURL string) (string, error)
	ProcessInbound(ctx context.Context, workspaceID uuid.UUID, mediaType string, data []byte) (string, error)
}
```

### ProcessOutbound Pattern

```go
func (e *DefaultEngine) ProcessOutbound(ctx context.Context, workspaceID uuid.UUID, mediaURL string) (string, error) {
	// 1. Download media with size limit (maxMediaSize = 25MB)
	res, err := e.Download(ctx, mediaURL, nil, maxMediaSize)
	if err != nil {
		return "", err // Map size limits and download failures to typed errors
	}
	
	// 2. Upload to S3
	key := workspaceID.String() + "/" + res.Hash + "." + res.Extension
	if err := e.Upload(ctx, key, res.Bytes, res.ContentType); err != nil {
		return "", err
	}
	
	// 3. Return internal proxy URL
	return "/media/" + workspaceID.String() + "/" + res.Hash + "." + res.Extension, nil
}
```

### ProcessInbound Pattern

```go
func (e *DefaultEngine) ProcessInbound(ctx context.Context, workspaceID uuid.UUID, mediaType string, data []byte) (string, error) {
	if int64(len(data)) > maxMediaSize {
		return "", ErrMediaSizeExceeded
	}
	
	hashKey := hashBytes(data)
	ext := getExtFromMediaType(mediaType)
	s3Key := workspaceID.String() + "/" + hashKey + "." + ext
	mimeType := getMimeFromMediaType(mediaType)
	
	if err := e.Upload(ctx, s3Key, data, mimeType); err != nil {
		return "", err
	}
	
	return "/media/" + workspaceID.String() + "/" + hashKey + "." + ext, nil
}
```
